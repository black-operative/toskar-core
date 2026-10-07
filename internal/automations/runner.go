package automations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/locale"
	modelhealth "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const (
	defaultInterval = 30 * time.Second
	defaultLease    = 15 * time.Minute
	// defaultWorkers due automations run at once. More would only queue for
	// the same models; one slow run no longer holds up the rest (#204).
	defaultWorkers = 2
	// defaultRunTimeout stops a run that never finishes (#204).
	defaultRunTimeout = 20 * time.Minute
)

// ErrRunning means the automation already has a run in progress.
var ErrRunning = contracts.NewError("AUTOMATION_RUNNING", nil, errors.New("this automation is already running"))

// timedOut is a run stopped by its time limit. It isn't retried: a run that
// took the whole limit would most likely take it again.
func timedOut(limit time.Duration) error {
	minutes := int(limit.Minutes())
	return contracts.Errorf("AUTOMATION_TIMEOUT", map[string]any{"minutes": minutes}, "the run took longer than %d minutes and was stopped", minutes)
}

// Store is the persistence the daemon loop needs. *repositories.AutomationRepo satisfies it.
type Store interface {
	Get(ctx context.Context, id string) (Automation, error)
	RefreshNextRuns(ctx context.Context, now time.Time) error
	Due(ctx context.Context, now time.Time) ([]Automation, error)
	Claim(ctx context.Context, automationID string, occurrence, now time.Time, lease time.Duration) (Run, bool, error)
	MarkRunning(ctx context.Context, runID string, now time.Time, lease time.Duration) error
	RenewLease(ctx context.Context, runID string, now time.Time, lease time.Duration) error
	CompleteRun(ctx context.Context, runID string, result Execution, finished time.Time) error
	SetSavedFile(ctx context.Context, runID, path string) error
	FailRun(ctx context.Context, runID string, message string, result Execution, finished time.Time) error
	ScheduleRetry(ctx context.Context, runID, message string, result Execution, finished, retryAt time.Time) error
	AbandonExpired(ctx context.Context, now time.Time) ([]Run, error)
	PreviousResult(ctx context.Context, automationID string, before time.Time) (prev Previous, ok bool, err error)
	SetNotificationSent(ctx context.Context, runID string, sent bool) error
	SetDecision(ctx context.Context, runID, detail string, values map[string]any) error
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
	// Pause turns an automation off. Runs that keep failing are paused
	// instead of failing on every schedule (spec §60, #204).
	Pause func(ctx context.Context, id string) error
	// Workers is how many automations run at once; RunTimeout stops a run
	// that takes longer. Zero uses the defaults.
	Workers    int
	RunTimeout time.Duration
	// Post adds a result to the chat an automation was made from, when it
	// notifies, so the person can reply to it there (#204).
	Post func(ctx context.Context, automation Automation, run Run, text string) error

	mu       sync.Mutex
	inflight map[string]bool
	sem      chan struct{}
	base     context.Context
	wg       sync.WaitGroup
}

const (
	// oomPauseAfter out-of-memory failures in a row pause an automation;
	// pauseAfter failures of any other kind do.
	oomPauseAfter = 2
	pauseAfter    = 3
)

// Start ticks until ctx is cancelled. The first tick runs immediately. A
// tick starts the due runs and returns, so a slow run doesn't delay the
// next tick; runs still going when ctx ends are stopped and waited for.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	r.base = ctx
	r.mu.Unlock()
	defer r.wg.Wait()
	if err := r.tick(ctx, false); err != nil && r.Logger != nil {
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
			if err := r.tick(ctx, false); err != nil && r.Logger != nil {
				r.Logger.Warn("automation tick failed", "error", err)
			}
		}
	}
}

// Tick refreshes schedules, runs due automations, and fails leases left
// behind by a crash. It returns when the runs it started have finished.
func (r *Runner) Tick(ctx context.Context) error {
	return r.tick(ctx, true)
}

// Wait returns when every run started so far has finished, such as one
// Run now started.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) tick(ctx context.Context, wait bool) error {
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
	var (
		errs   []error
		errsMu sync.Mutex
		mine   sync.WaitGroup
	)
	for _, automation := range due {
		// One still running from an earlier tick, or from Run now, is
		// left to finish.
		if automation.NextRunAt == nil || !r.begin(automation.ID) {
			continue
		}
		mine.Add(1)
		r.wg.Add(1)
		go func(automation Automation) {
			defer r.wg.Done()
			defer mine.Done()
			defer r.end(automation.ID)
			if !r.acquire(ctx) {
				return
			}
			defer r.release()
			if err := r.runOne(ctx, automation); err != nil {
				if wait {
					errsMu.Lock()
					errs = append(errs, err)
					errsMu.Unlock()
				} else if r.Logger != nil && ctx.Err() == nil {
					r.Logger.Warn("automation run failed", "automation", automation.Name, "error", err)
				}
			}
		}(automation)
	}
	if wait {
		mine.Wait()
	}
	abandoned, err := r.Store.AbandonExpired(ctx, r.now())
	if err != nil {
		errs = append(errs, err)
	} else if err := r.reportAbandoned(ctx, abandoned); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RunNow starts one occurrence immediately and returns its run as soon as
// it is claimed; the run goes on without the request, so closing the page
// can't stop it, and its progress arrives as automation events (#204). A
// paused automation stays paused afterward. When the scheduled occurrence
// is already due, that occurrence is the one that runs.
func (r *Runner) RunNow(ctx context.Context, id string) (Run, error) {
	if r.Store == nil || r.Exec == nil {
		return Run{}, errors.New("automation runner is not configured")
	}
	automation, err := r.Store.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if !r.begin(id) {
		return Run{}, ErrRunning
	}
	now := r.now().UTC().Truncate(time.Second)
	occurrence := now
	if automation.NextRunAt != nil && !automation.NextRunAt.After(now) {
		occurrence = automation.NextRunAt.UTC().Truncate(time.Second)
	}
	run, ok, err := r.claim(ctx, automation, occurrence)
	if err == nil && !ok {
		err = fmt.Errorf("automation %q already has a run at %s", automation.ID, occurrence.UTC().Format(time.RFC3339))
	}
	if err != nil {
		r.end(id)
		return Run{}, err
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.end(id)
		if err := r.finish(r.background(), automation, run); err != nil && r.Logger != nil {
			r.Logger.Warn("automation run failed", "automation", automation.Name, "error", err)
		}
	}()
	return run, nil
}

func (r *Runner) runOne(ctx context.Context, automation Automation) error {
	if automation.NextRunAt == nil {
		return nil
	}
	run, ok, err := r.claim(ctx, automation, *automation.NextRunAt)
	if err != nil || !ok {
		return err
	}
	return r.finish(ctx, automation, run)
}

// claim takes the occurrence and marks its run running. ok is false when
// another run already has it.
func (r *Runner) claim(ctx context.Context, automation Automation, occurrence time.Time) (Run, bool, error) {
	run, ok, err := r.Store.Claim(ctx, automation.ID, occurrence, r.now(), r.lease())
	if err != nil || !ok {
		return Run{}, ok, err
	}
	started := r.now()
	if err := r.Store.MarkRunning(ctx, run.ID, started, r.lease()); err != nil {
		return Run{}, false, err
	}
	run.Status, run.StartedAt = RunRunning, &started
	return run, true, nil
}

// finish runs a claimed occurrence within its time limit and records how it
// ended.
func (r *Runner) finish(ctx context.Context, automation Automation, run Run) error {
	leaseCtx, stopLease := context.WithCancel(ctx)
	defer stopLease()
	go r.keepLease(leaseCtx, run.ID)

	r.publish(events.AutomationStarted, automation, run, Execution{}, nil, false)
	// The executor compares a change-mode result with the last one while
	// its model is still loaded (#204).
	prev, err := r.previous(ctx, automation, run.OccurrenceAt)
	if err != nil {
		return err
	}
	runCtx := ctx
	if prev != nil {
		runCtx = WithPrevious(ctx, *prev)
	}
	limit := r.runTimeout()
	execCtx, cancel := context.WithTimeout(runCtx, limit)
	result, execErr := r.Exec.Execute(execCtx, automation)
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		execErr = timedOut(limit)
	}
	cancel()
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
		code, params := contracts.ErrorCode(execErr)
		if key := "errors:" + code; code != "" && locale.T(locale.Source, key, nil) != key {
			// The reason in the App language, for a code the catalog has.
			values := map[string]any{"detail": execErr.Error()}
			for k, v := range params {
				values[k] = v
			}
			message = locale.Key(key, values)
		}
		oom := code == "OUT_OF_MEMORY" || modelhealth.OutOfMemory(execErr.Error())
		limit := pauseAfter
		if oom {
			message = locale.Key("notifications:notices.automationOutOfMemory", nil)
			limit = oomPauseAfter
		}
		// One that keeps failing, for any reason, is paused instead of
		// failing on every schedule (#204).
		if updated.ConsecutiveFailures >= limit && r.Pause != nil && updated.Enabled {
			if err := r.Pause(ctx, automation.ID); err != nil {
				if r.Logger != nil {
					r.Logger.Warn("pause automation after failures", "automation", automation.Name, "error", err)
				}
			} else {
				updated.Enabled = false
				key := "notifications:notices.automationPausedFailures"
				if oom {
					key = "notifications:notices.automationPausedMemory"
				}
				message = locale.Key(key, map[string]any{"count": updated.ConsecutiveFailures})
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
	// Also saved as a file, for an automation with a save folder (#204).
	if automation.SaveFolder != "" {
		if text := ResultProse(result.Text); text != "" {
			path, err := SaveResult(automation, finished, text)
			if err == nil {
				err = r.Store.SetSavedFile(ctx, run.ID, path)
			}
			if err != nil && r.Logger != nil {
				r.Logger.Warn("save automation result", "automation", automation.Name, "error", err)
			}
		}
	}
	sent, notifyErr := r.deliver(ctx, automation, run, result, prev)
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

func (r *Runner) deliver(ctx context.Context, automation Automation, run Run, result Execution, prev *Previous) (bool, error) {
	decision := DecideRun(automation.Notification, result, prev)
	// The apps explain this decision rather than make their own (#204).
	if err := r.Store.SetDecision(ctx, run.ID, decision.Detail, decision.Values); err != nil && r.Logger != nil {
		r.Logger.Warn("record automation decision", "run_id", run.ID, "error", err)
	}
	if !decision.Notify {
		return false, nil
	}
	if r.Notify == nil {
		return false, errors.New("notifier is not configured")
	}
	notice := decision.Notice
	notice.AutomationID = automation.ID
	// The notice opens the chat when the result is there; a chat deleted
	// since leaves it opening the automation.
	if r.Post != nil && automation.ConversationID != "" {
		if text := ResultProse(result.Text); text != "" {
			if err := r.Post(ctx, automation, run, text); err != nil {
				if r.Logger != nil {
					r.Logger.Warn("post automation result to its chat", "automation_id", automation.ID, "error", err)
				}
			} else {
				notice.ConversationID = automation.ConversationID
			}
		}
	}
	if err := r.Notify.Notify(ctx, noticeTitle(automation.Name, notice)); err != nil {
		return false, err
	}
	return true, nil
}

// previous is the last successful run before this occurrence, or nil.
func (r *Runner) previous(ctx context.Context, automation Automation, occurrence time.Time) (*Previous, error) {
	prev, ok, err := r.Store.PreviousResult(ctx, automation.ID, occurrence)
	if err != nil || !ok {
		return nil, err
	}
	return &prev, nil
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
	if automation.ConversationID != "" {
		payload["conversation_id"] = automation.ConversationID
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

// begin marks an automation as running; false when it already is.
func (r *Runner) begin(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		r.inflight = map[string]bool{}
	}
	if r.inflight[id] {
		return false
	}
	r.inflight[id] = true
	return true
}

func (r *Runner) end(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, id)
}

// acquire waits for a worker; false when ctx ends first.
func (r *Runner) acquire(ctx context.Context) bool {
	r.mu.Lock()
	if r.sem == nil {
		n := r.Workers
		if n <= 0 {
			n = defaultWorkers
		}
		r.sem = make(chan struct{}, n)
	}
	sem := r.sem
	r.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *Runner) release() {
	r.mu.Lock()
	sem := r.sem
	r.mu.Unlock()
	<-sem
}

// background is the context a run started by Run now goes on in: the
// runner's own, which ends with the daemon, never the request's.
func (r *Runner) background() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.base != nil {
		return r.base
	}
	return context.Background()
}

func (r *Runner) runTimeout() time.Duration {
	if r.RunTimeout <= 0 {
		return defaultRunTimeout
	}
	return r.RunTimeout
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
