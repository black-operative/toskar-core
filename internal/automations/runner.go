package automations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/locale"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
)

const (
	defaultInterval = 30 * time.Second
	defaultLease    = 15 * time.Minute
)

// Store is the persistence the daemon loop needs. *repositories.AutomationRepo satisfies it.
type Store interface {
	Get(ctx context.Context, id string) (Automation, error)
	RefreshNextRuns(ctx context.Context, now time.Time) error
	Due(ctx context.Context, now time.Time) ([]Automation, error)
	Claim(ctx context.Context, automationID string, occurrence, now time.Time, lease time.Duration) (Run, bool, error)
	MarkRunning(ctx context.Context, runID string, now time.Time, lease time.Duration) error
	RenewLease(ctx context.Context, runID string, now time.Time, lease time.Duration) error
	CompleteRun(ctx context.Context, runID string, result Execution, finished time.Time) error
	FailRun(ctx context.Context, runID string, message string, result Execution, finished time.Time) error
	ScheduleRetry(ctx context.Context, runID, message string, result Execution, finished, retryAt time.Time) error
	AbandonExpired(ctx context.Context, now time.Time) ([]Run, error)
	PreviousResult(ctx context.Context, automationID string, before time.Time) (text string, notified bool, ok bool, err error)
	SetNotificationSent(ctx context.Context, runID string, sent bool) error
	RunFor(ctx context.Context, automationID string, occurrence time.Time) (Run, error)
}

// Executor runs one scheduled prompt. The daemon supplies profile resolution,
// Norn placement, and model startup. The runner only owns the claim and the result.
type Executor interface {
	Execute(ctx context.Context, automation Automation) (Execution, error)
}

// Runner claims due automations and executes each occurrence once.
type Runner struct {
	Store    Store
	Exec     Executor
	Notify   Notifier
	Bus      *events.Bus
	Logger   *slog.Logger
	Interval time.Duration
	Lease    time.Duration
	Now      func() time.Time
	// Pause turns an automation off. Runs that keep running out of memory
	// are paused instead of failing on every schedule (spec §60).
	Pause func(ctx context.Context, id string) error
}

// oomPauseAfter is how many out-of-memory failures in a row pause an automation.
const oomPauseAfter = 2

// Start ticks until ctx is cancelled. The first tick runs immediately.
func (r *Runner) Start(ctx context.Context) {
	if err := r.Tick(ctx); err != nil && r.Logger != nil {
		r.Logger.Warn("automation tick failed", "error", err)
	}
	interval := r.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil && r.Logger != nil {
				r.Logger.Warn("automation tick failed", "error", err)
			}
		}
	}
}

// Tick refreshes schedules, runs due automations, and fails leases left behind by a crash.
func (r *Runner) Tick(ctx context.Context) error {
	if r.Store == nil {
		return errors.New("automation store is required")
	}
	if r.Exec == nil {
		return errors.New("automation executor is required")
	}
	now := r.now()
	if err := r.Store.RefreshNextRuns(ctx, now); err != nil {
		return err
	}
	due, err := r.Store.Due(ctx, now)
	if err != nil {
		return err
	}
	var errs []error
	for _, automation := range due {
		if automation.NextRunAt == nil {
			continue
		}
		if err := r.runOne(ctx, automation); err != nil {
			errs = append(errs, err)
		}
	}
	abandoned, err := r.Store.AbandonExpired(ctx, r.now())
	if err != nil {
		errs = append(errs, err)
	} else if err := r.reportAbandoned(ctx, abandoned); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RunNow executes one occurrence immediately. A paused automation stays paused afterward.
// When the scheduled occurrence is already due, that occurrence is the one that runs.
func (r *Runner) RunNow(ctx context.Context, id string) (Run, error) {
	if r.Store == nil || r.Exec == nil {
		return Run{}, errors.New("automation runner is not configured")
	}
	automation, err := r.Store.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	now := r.now().UTC().Truncate(time.Second)
	occurrence := now
	if automation.NextRunAt != nil && !automation.NextRunAt.After(now) {
		occurrence = automation.NextRunAt.UTC().Truncate(time.Second)
	}
	if err := r.execute(ctx, automation, occurrence, true); err != nil {
		return Run{}, err
	}
	return r.Store.RunFor(ctx, id, occurrence)
}

func (r *Runner) runOne(ctx context.Context, automation Automation) error {
	if automation.NextRunAt == nil {
		return nil
	}
	return r.execute(ctx, automation, *automation.NextRunAt, false)
}

func (r *Runner) execute(ctx context.Context, automation Automation, occurrence time.Time, manual bool) error {
	now := r.now()
	run, ok, err := r.Store.Claim(ctx, automation.ID, occurrence, now, r.lease())
	if err != nil {
		return err
	}
	if !ok {
		if manual {
			return fmt.Errorf("automation %q already has a run at %s", automation.ID, occurrence.UTC().Format(time.RFC3339))
		}
		return nil
	}
	if err := r.Store.MarkRunning(ctx, run.ID, r.now(), r.lease()); err != nil {
		return err
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go r.keepLease(execCtx, run.ID)

	r.publish(events.AutomationStarted, automation, run, Execution{}, nil, false)
	result, execErr := r.Exec.Execute(execCtx, automation)
	finished := r.now()
	if execErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ClassifyFailure(execErr) == FailureTransient && run.Attempt < MaxAttempts {
			retryAt := finished.Add(RetryDelay(run.Attempt))
			if err := r.Store.ScheduleRetry(ctx, run.ID, execErr.Error(), result, finished, retryAt); err != nil {
				return err
			}
			return nil
		}
		if err := r.Store.FailRun(ctx, run.ID, execErr.Error(), result, finished); err != nil {
			return err
		}
		updated, err := r.Store.Get(ctx, automation.ID)
		if err != nil {
			return err
		}
		message := locale.Literal(execErr.Error())
		if modelhealth.OutOfMemory(execErr.Error()) {
			message = locale.Key("notifications:notices.automationOutOfMemory", nil)
			if updated.ConsecutiveFailures >= oomPauseAfter && r.Pause != nil && updated.Enabled {
				if err := r.Pause(ctx, automation.ID); err != nil {
					if r.Logger != nil {
						r.Logger.Warn("pause automation after memory failures", "automation", automation.Name, "error", err)
					}
				} else {
					updated.Enabled = false
					message = locale.Key("notifications:notices.automationPausedMemory", map[string]any{"count": updated.ConsecutiveFailures})
				}
			}
		}
		sent, notifyErr := r.notifyRepeated(ctx, updated, run, message)
		if err := r.Store.SetNotificationSent(ctx, run.ID, sent); err != nil {
			return err
		}
		if notifyErr != nil && !errors.Is(notifyErr, ErrNotifyDisabled) && r.Logger != nil {
			r.Logger.Warn("automation failure notification failed", "automation", automation.Name, "error", notifyErr)
		}
		r.publish(events.AutomationFailed, updated, run, result, execErr, false)
		return nil
	}
	if err := r.Store.CompleteRun(ctx, run.ID, result, finished); err != nil {
		return err
	}
	sent, notifyErr := r.deliver(ctx, automation, run, result)
	if err := r.Store.SetNotificationSent(ctx, run.ID, sent); err != nil {
		return err
	}
	if notifyErr != nil && !errors.Is(notifyErr, ErrNotifyDisabled) && r.Logger != nil {
		r.Logger.Warn("automation notification failed", "automation", automation.Name, "error", notifyErr)
	}
	r.publish(events.AutomationCompleted, automation, run, result, nil, sent)
	return nil
}

func (r *Runner) reportAbandoned(ctx context.Context, runs []Run) error {
	var errs []error
	for _, run := range runs {
		automation, err := r.Store.Get(ctx, run.AutomationID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sent, notifyErr := r.notifyRepeated(ctx, automation, run, locale.Literal(run.Error))
		if err := r.Store.SetNotificationSent(ctx, run.ID, sent); err != nil {
			errs = append(errs, err)
			continue
		}
		if notifyErr != nil && !errors.Is(notifyErr, ErrNotifyDisabled) {
			errs = append(errs, notifyErr)
		}
		r.publish(events.AutomationFailed, automation, run, Execution{Text: run.Result, ModelID: run.ModelID, NodeID: run.NodeID}, errors.New(run.Error), false)
	}
	return errors.Join(errs...)
}

// notifyRepeated tells the user an automation keeps failing, and why.
func (r *Runner) notifyRepeated(ctx context.Context, automation Automation, run Run, reason locale.Text) (bool, error) {
	if automation.ConsecutiveFailures < 2 && run.Attempt < MaxAttempts {
		return false, nil
	}
	if r.Notify == nil {
		return false, errors.New("notifier is not configured")
	}
	what := locale.Key("notifications:notices.automationFailedAttempts", map[string]any{"count": run.Attempt})
	if run.Attempt < MaxAttempts {
		what = locale.Key("notifications:notices.automationFailedInARow", map[string]any{"count": automation.ConsecutiveFailures})
	}
	notice := Notice{AutomationID: automation.ID, Failure: true,
		Message: &locale.Message{Body: []locale.Text{what, reason}}}
	if err := r.Notify.Notify(ctx, noticeTitle(automation.Name, notice)); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) deliver(ctx context.Context, automation Automation, run Run, result Execution) (bool, error) {
	text, notified, ok, err := r.Store.PreviousResult(ctx, automation.ID, run.OccurrenceAt)
	if err != nil {
		return false, err
	}
	var previous *string
	if ok {
		previous = &text
	}
	decision := Decide(automation.Notification, result.Text, previous, notified)
	if !decision.Notify {
		return false, nil
	}
	if r.Notify == nil {
		return false, errors.New("notifier is not configured")
	}
	notice := decision.Notice
	notice.AutomationID = automation.ID
	if err := r.Notify.Notify(ctx, noticeTitle(automation.Name, notice)); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) keepLease(ctx context.Context, runID string) {
	interval := r.lease() / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Store.RenewLease(ctx, runID, r.now(), r.lease()); err != nil && r.Logger != nil {
				r.Logger.Warn("automation lease renewal failed", "run_id", runID, "error", err)
			}
		}
	}
}

func (r *Runner) publish(eventType string, automation Automation, run Run, result Execution, execErr error, notified bool) {
	if r.Bus == nil {
		return
	}
	payload := map[string]any{
		"automation_id": automation.ID,
		"run_id":        run.ID,
		"name":          automation.Name,
		"occurrence_at": run.OccurrenceAt.UTC().Format(time.RFC3339),
	}
	if eventType == events.AutomationCompleted {
		payload["notification_sent"] = notified
	}
	if result.ModelID != "" {
		payload["model_id"] = result.ModelID
	}
	if result.NodeID != "" {
		payload["node_id"] = result.NodeID
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	if len(result.Skipped) > 0 {
		payload["skipped"] = result.Skipped
	}
	r.Bus.Publish(events.New(eventType, payload))
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) lease() time.Duration {
	if r.Lease <= 0 {
		return defaultLease
	}
	return r.Lease
}
