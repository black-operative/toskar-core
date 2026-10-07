package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/automations"
)

const runSelect = `
	SELECT id, automation_id, occurrence_at, status, claimed_at, lease_until,
		started_at, finished_at, COALESCE(result, ''), COALESCE(error, ''),
		notification_sent, COALESCE(model_id, ''), COALESCE(node_id, ''),
		attempt, retry_at, created_at, COALESCE(notify_detail, ''), COALESCE(notify_values, ''), COALESCE(conversation_id, ''), COALESCE(saved_file, '')
	FROM automation_runs`

// Claim takes the occurrence for this daemon.
// A live lease is left alone. An expired claim or run can be taken over after a crash.
// A succeeded or failed occurrence is not claimed again.
func (r *AutomationRepo) Claim(ctx context.Context, automationID string, occurrence, now time.Time, lease time.Duration) (automations.Run, bool, error) {
	now = clock(now)
	occurrence = clock(occurrence)
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	until := now.Add(lease)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return automations.Run{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	id := uuid.NewString()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO automation_runs (
			id, automation_id, occurrence_at, status, claimed_at, lease_until, attempt, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(automation_id, occurrence_at) DO NOTHING`,
		id, automationID, formatTime(occurrence), automations.RunClaimed,
		formatTime(now), formatTime(until), 1, formatTime(now))
	if err != nil {
		return automations.Run{}, false, err
	}
	inserted, _ := res.RowsAffected()
	if inserted == 0 {
		res, err = tx.ExecContext(ctx, `
			UPDATE automation_runs
			SET status = ?, claimed_at = ?, lease_until = ?, attempt = attempt + 1,
				started_at = NULL, finished_at = NULL, retry_at = NULL
			WHERE automation_id = ? AND occurrence_at = ?
				AND status = ?
				AND retry_at IS NOT NULL AND retry_at <= ?`,
			automations.RunClaimed, formatTime(now), formatTime(until),
			automationID, formatTime(occurrence),
			automations.RunRetrying, formatTime(now))
		if err != nil {
			return automations.Run{}, false, err
		}
		updated, _ := res.RowsAffected()
		if updated == 0 {
			res, err = tx.ExecContext(ctx, `
				UPDATE automation_runs
				SET status = ?, claimed_at = ?, lease_until = ?, attempt = attempt + 1,
					started_at = NULL, finished_at = NULL, result = NULL, error = NULL,
					model_id = NULL, node_id = NULL, notification_sent = 0, retry_at = NULL
				WHERE automation_id = ? AND occurrence_at = ?
					AND status IN (?, ?)
					AND attempt < ?
					AND (lease_until IS NULL OR lease_until <= ?)`,
				automations.RunClaimed, formatTime(now), formatTime(until),
				automationID, formatTime(occurrence),
				automations.RunClaimed, automations.RunRunning, automations.MaxAttempts, formatTime(now))
			if err != nil {
				return automations.Run{}, false, err
			}
			updated, _ = res.RowsAffected()
		}
		if updated == 0 {
			return automations.Run{}, false, nil
		}
	}
	run, err := runFor(ctx, tx, automationID, occurrence)
	if err != nil {
		return automations.Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return automations.Run{}, false, err
	}
	return run, true, nil
}

// MarkRunning records that the claimed occurrence has started work and extends its lease.
func (r *AutomationRepo) MarkRunning(ctx context.Context, runID string, now time.Time, lease time.Duration) error {
	now = clock(now)
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE automation_runs
		SET status = ?, started_at = COALESCE(started_at, ?), lease_until = ?
		WHERE id = ? AND status IN (?, ?)`,
		automations.RunRunning, formatTime(now), formatTime(now.Add(lease)),
		runID, automations.RunClaimed, automations.RunRunning)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %q is not claimed", runID)
	}
	return nil
}

// RenewLease extends a live claim so a long run is not stolen.
func (r *AutomationRepo) RenewLease(ctx context.Context, runID string, now time.Time, lease time.Duration) error {
	now = clock(now)
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE automation_runs
		SET lease_until = ?
		WHERE id = ? AND status IN (?, ?)`,
		formatTime(now.Add(lease)), runID, automations.RunClaimed, automations.RunRunning)
	return err
}

// CompleteRun stores a successful result and advances the automation's next run.
func (r *AutomationRepo) CompleteRun(ctx context.Context, runID string, result automations.Execution, finished time.Time) error {
	finished = clock(finished)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var automationID string
	var occurrenceText string
	err = tx.QueryRowContext(ctx, `
		SELECT automation_id, occurrence_at FROM automation_runs
		WHERE id = ? AND status IN (?, ?)`,
		runID, automations.RunClaimed, automations.RunRunning).Scan(&automationID, &occurrenceText)
	if err == sql.ErrNoRows {
		return fmt.Errorf("run %q is not active", runID)
	}
	if err != nil {
		return err
	}
	occurrence := parseTime(occurrenceText)
	bound := finished
	if occurrence.After(bound) {
		bound = occurrence
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE automation_runs
		SET status = ?, result = ?, error = NULL, model_id = ?, node_id = ?,
			finished_at = ?, lease_until = NULL, source_hash = ?
		WHERE id = ?`,
		automations.RunSucceeded, result.Text, nullIfEmpty(result.ModelID), nullIfEmpty(result.NodeID),
		formatTime(finished), nullIfEmpty(result.SourceHash), runID); err != nil {
		return err
	}

	automation, err := getAutomation(ctx, tx, automationID)
	if err != nil {
		return err
	}
	automation.LastRunAt = &bound
	automation.UpdatedAt = finished
	automation.ConsecutiveFailures = 0
	automation.LastError = ""
	if err := automation.SetNextRun(finished); err != nil {
		return err
	}
	if err := updateAutomation(ctx, tx, automation); err != nil {
		return err
	}
	return tx.Commit()
}

// ScheduleRetry keeps this occurrence and waits until retryAt before another attempt.
func (r *AutomationRepo) ScheduleRetry(ctx context.Context, runID, message string, result automations.Execution, finished, retryAt time.Time) error {
	finished = clock(finished)
	retryAt = clock(retryAt)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var automationID string
	err = tx.QueryRowContext(ctx, `
		SELECT automation_id FROM automation_runs
		WHERE id = ? AND status IN (?, ?)`,
		runID, automations.RunClaimed, automations.RunRunning).Scan(&automationID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("run %q is not active", runID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE automation_runs
		SET status = ?, result = ?, error = ?, model_id = ?, node_id = ?,
			finished_at = ?, lease_until = NULL, retry_at = ?
		WHERE id = ?`,
		automations.RunRetrying, result.Text, message, nullIfEmpty(result.ModelID), nullIfEmpty(result.NodeID),
		formatTime(finished), formatTime(retryAt), runID); err != nil {
		return err
	}
	automation, err := getAutomation(ctx, tx, automationID)
	if err != nil {
		return err
	}
	automation.LastError = message
	automation.UpdatedAt = finished
	if err := updateAutomation(ctx, tx, automation); err != nil {
		return err
	}
	return tx.Commit()
}

// FailRun records a terminal failure, shows it on the automation, and moves the schedule past this occurrence.
func (r *AutomationRepo) FailRun(ctx context.Context, runID, message string, result automations.Execution, finished time.Time) error {
	finished = clock(finished)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var automationID, occurrenceText string
	err = tx.QueryRowContext(ctx, `
		SELECT automation_id, occurrence_at FROM automation_runs
		WHERE id = ? AND status IN (?, ?)`,
		runID, automations.RunClaimed, automations.RunRunning).Scan(&automationID, &occurrenceText)
	if err == sql.ErrNoRows {
		return fmt.Errorf("run %q is not active", runID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE automation_runs
		SET status = ?, result = ?, error = ?, model_id = ?, node_id = ?,
			finished_at = ?, lease_until = NULL, retry_at = NULL
		WHERE id = ?`,
		automations.RunFailed, result.Text, message, nullIfEmpty(result.ModelID), nullIfEmpty(result.NodeID),
		formatTime(finished), runID); err != nil {
		return err
	}
	if err := noteTerminalFailure(ctx, tx, automationID, parseTime(occurrenceText), message, finished); err != nil {
		return err
	}
	return tx.Commit()
}

// AbandonExpired marks leftover expired leases as failed.
// When the expired occurrence is still the automation's next run, the failure is recorded on the automation.
// Those runs are returned so the daemon can notify if the failure has become repeated.
func (r *AutomationRepo) AbandonExpired(ctx context.Context, now time.Time) ([]automations.Run, error) {
	now = clock(now)
	rows, err := r.db.QueryContext(ctx, runSelect+`
		WHERE status IN (?, ?)
			AND lease_until IS NOT NULL
			AND lease_until <= ?`,
		automations.RunClaimed, automations.RunRunning, formatTime(now))
	if err != nil {
		return nil, err
	}
	expired, err := scanRuns(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(expired) == 0 {
		return []automations.Run{}, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var terminal []automations.Run
	for _, run := range expired {
		message := run.Error
		if message == "" {
			message = "lease expired before the run finished"
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE automation_runs
			SET status = ?, error = ?, finished_at = ?, lease_until = NULL, retry_at = NULL
			WHERE id = ? AND status IN (?, ?)`,
			automations.RunFailed, message, formatTime(now), run.ID,
			automations.RunClaimed, automations.RunRunning)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue
		}
		automation, err := getAutomation(ctx, tx, run.AutomationID)
		if err != nil {
			return nil, err
		}
		if automation.NextRunAt == nil || !automation.NextRunAt.Equal(run.OccurrenceAt) {
			continue
		}
		if err := noteTerminalFailure(ctx, tx, run.AutomationID, run.OccurrenceAt, message, now); err != nil {
			return nil, err
		}
		run.Status = automations.RunFailed
		run.Error = message
		terminal = append(terminal, run)
	}
	if terminal == nil {
		terminal = []automations.Run{}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return terminal, nil
}

func noteTerminalFailure(ctx context.Context, tx *sql.Tx, automationID string, occurrence time.Time, message string, finished time.Time) error {
	automation, err := getAutomation(ctx, tx, automationID)
	if err != nil {
		return err
	}
	automation.LastError = message
	automation.ConsecutiveFailures++
	bound := clock(finished)
	occurrence = clock(occurrence)
	if occurrence.After(bound) {
		bound = occurrence
	}
	if automation.LastRunAt == nil || bound.After(*automation.LastRunAt) {
		automation.LastRunAt = &bound
	}
	automation.UpdatedAt = clock(finished)
	if err := automation.SetNextRun(automation.UpdatedAt); err != nil {
		return err
	}
	return updateAutomation(ctx, tx, automation)
}

// attachLatest fills the newest occurrence summary on each listed automation.
func (r *AutomationRepo) attachLatest(ctx context.Context, items []automations.Automation) error {
	if len(items) == 0 {
		return nil
	}
	latest, err := r.latestRuns(ctx)
	if err != nil {
		return err
	}
	for i := range items {
		run, ok := latest[items[i].ID]
		if !ok {
			continue
		}
		items[i].LastStatus = run.Status
		text := strings.TrimSpace(run.Result)
		if text == "" {
			text = strings.TrimSpace(run.Error)
		}
		items[i].LastResult = clipResult(text)
	}
	return nil
}

func (r *AutomationRepo) latestRuns(ctx context.Context) (map[string]automations.Run, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.automation_id, r.occurrence_at, r.status, r.claimed_at, r.lease_until,
			r.started_at, r.finished_at, COALESCE(r.result, ''), COALESCE(r.error, ''),
			r.notification_sent, COALESCE(r.model_id, ''), COALESCE(r.node_id, ''),
			r.attempt, r.retry_at, r.created_at, COALESCE(r.notify_detail, ''), COALESCE(r.notify_values, ''), COALESCE(r.conversation_id, ''), COALESCE(r.saved_file, '')
		FROM automation_runs r
		INNER JOIN (
			SELECT automation_id, MAX(occurrence_at) AS occurrence_at
			FROM automation_runs
			GROUP BY automation_id
		) latest ON latest.automation_id = r.automation_id AND latest.occurrence_at = r.occurrence_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]automations.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out[run.AutomationID] = run
	}
	return out, rows.Err()
}

func clipResult(text string) string {
	const limit = 240
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

// ListRuns returns every occurrence for an automation, newest scheduled time first.
func (r *AutomationRepo) ListRuns(ctx context.Context, automationID string) ([]automations.Run, error) {
	rows, err := r.db.QueryContext(ctx, runSelect+`
		WHERE automation_id = ?
		ORDER BY occurrence_at DESC, id DESC`, automationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

// History loads an automation with its newest runs, a page of them
// (#204); HistoryMore says older ones exist, which RunsPage reads.
func (r *AutomationRepo) History(ctx context.Context, automationID string) (automations.Detail, error) {
	automation, err := r.Get(ctx, automationID)
	if err != nil {
		return automations.Detail{}, err
	}
	page, err := r.RunsPage(ctx, automationID, "", automations.HistoryPage)
	if err != nil {
		return automations.Detail{}, err
	}
	return automations.Detail{Automation: automation, History: page.Runs, HistoryMore: page.More}, nil
}

// RunsPage is up to limit runs, newest occurrence first, older than the run
// before when it is set.
func (r *AutomationRepo) RunsPage(ctx context.Context, automationID, before string, limit int) (automations.RunsPage, error) {
	if limit <= 0 || limit > automations.MaxHistoryPage {
		limit = automations.HistoryPage
	}
	where, args := `
		WHERE automation_id = ?`, []any{automationID}
	if before != "" {
		var at string
		err := r.db.QueryRowContext(ctx, `SELECT occurrence_at FROM automation_runs WHERE id = ? AND automation_id = ?`, before, automationID).Scan(&at)
		if err == sql.ErrNoRows {
			return automations.RunsPage{}, fmt.Errorf("run %q not found", before)
		}
		if err != nil {
			return automations.RunsPage{}, err
		}
		where += ` AND (occurrence_at < ? OR (occurrence_at = ? AND id < ?))`
		args = append(args, at, at, before)
	}
	// One more than the page says whether there are older ones.
	rows, err := r.db.QueryContext(ctx, runSelect+where+`
		ORDER BY occurrence_at DESC, id DESC
		LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return automations.RunsPage{}, err
	}
	defer rows.Close()
	runs, err := scanRuns(rows)
	if err != nil {
		return automations.RunsPage{}, err
	}
	page := automations.RunsPage{Runs: runs}
	if len(runs) > limit {
		page.Runs, page.More = runs[:limit], true
	}
	if page.Runs == nil {
		page.Runs = []automations.Run{}
	}
	return page, nil
}

// PreviousResult returns the latest successful run scheduled before an
// occurrence: its result, whether it notified, and what its tools read.
func (r *AutomationRepo) PreviousResult(ctx context.Context, automationID string, before time.Time) (automations.Previous, bool, error) {
	var prev automations.Previous
	var notified int
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(result, ''), notification_sent, COALESCE(source_hash, '') FROM automation_runs
		WHERE automation_id = ? AND status = ? AND occurrence_at < ?
		ORDER BY occurrence_at DESC
		LIMIT 1`, automationID, automations.RunSucceeded, formatTime(clock(before))).Scan(&prev.Text, &notified, &prev.SourceHash)
	if err == sql.ErrNoRows {
		return automations.Previous{}, false, nil
	}
	if err != nil {
		return automations.Previous{}, false, err
	}
	prev.Notified = notified != 0
	return prev, true, nil
}

// SetDecision records why a run did or didn't notify (#204).
func (r *AutomationRepo) SetDecision(ctx context.Context, runID, detail string, values map[string]any) error {
	var raw any
	if len(values) > 0 {
		b, err := json.Marshal(values)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := r.db.ExecContext(ctx, `UPDATE automation_runs SET notify_detail = ?, notify_values = ? WHERE id = ?`, nullIfEmpty(detail), raw, runID)
	return err
}

// GetRun loads one run of an automation.
func (r *AutomationRepo) GetRun(ctx context.Context, automationID, runID string) (automations.Run, error) {
	run, err := scanRun(r.db.QueryRowContext(ctx, runSelect+` WHERE id = ? AND automation_id = ?`, runID, automationID))
	if err == sql.ErrNoRows {
		return automations.Run{}, fmt.Errorf("run %q not found", runID)
	}
	return run, err
}

// SetRunConversation records the chat a run's result was posted to.
func (r *AutomationRepo) SetRunConversation(ctx context.Context, runID, conversationID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE automation_runs SET conversation_id = ? WHERE id = ?`, nullIfEmpty(conversationID), runID)
	return err
}

// SetSavedFile records where a run's result was saved.
func (r *AutomationRepo) SetSavedFile(ctx context.Context, runID, path string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE automation_runs SET saved_file = ? WHERE id = ?`, nullIfEmpty(path), runID)
	return err
}

// SetNotificationSent records whether the user was notified for this occurrence.
func (r *AutomationRepo) SetNotificationSent(ctx context.Context, runID string, sent bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE automation_runs SET notification_sent = ? WHERE id = ?`, boolInt(sent), runID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("run %q not found", runID)
	}
	return nil
}

// RunFor loads the row for one occurrence.
func (r *AutomationRepo) RunFor(ctx context.Context, automationID string, occurrence time.Time) (automations.Run, error) {
	return runFor(ctx, r.db, automationID, clock(occurrence))
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func runFor(ctx context.Context, q queryRower, automationID string, occurrence time.Time) (automations.Run, error) {
	row := q.QueryRowContext(ctx, runSelect+` WHERE automation_id = ? AND occurrence_at = ?`, automationID, formatTime(occurrence))
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return automations.Run{}, fmt.Errorf("run for automation %q at %s not found", automationID, formatTime(occurrence))
	}
	return run, err
}

func getAutomation(ctx context.Context, q queryRower, id string) (automations.Automation, error) {
	row := q.QueryRowContext(ctx, automationSelect+` WHERE id = ?`, id)
	automation, err := scanAutomation(row)
	if err == sql.ErrNoRows {
		return automations.Automation{}, fmt.Errorf("automation %q not found", id)
	}
	return automation, err
}

func scanRuns(rows *sql.Rows) ([]automations.Run, error) {
	var out []automations.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	if out == nil {
		out = []automations.Run{}
	}
	return out, rows.Err()
}

func scanRun(s automationScanner) (automations.Run, error) {
	var run automations.Run
	var status, occurrence, created string
	var claimed, lease, started, finished, retryAt sql.NullString
	var notified int
	var values string
	if err := s.Scan(
		&run.ID, &run.AutomationID, &occurrence, &status, &claimed, &lease,
		&started, &finished, &run.Result, &run.Error, &notified, &run.ModelID, &run.NodeID,
		&run.Attempt, &retryAt, &created, &run.NotifyDetail, &values, &run.ConversationID, &run.SavedFile,
	); err != nil {
		return automations.Run{}, err
	}
	if values != "" {
		_ = json.Unmarshal([]byte(values), &run.NotifyValues)
	}
	run.Status = automations.RunStatus(status)
	run.OccurrenceAt = parseTime(occurrence)
	run.ClaimedAt = parseTimePtr(claimed)
	run.LeaseUntil = parseTimePtr(lease)
	run.StartedAt = parseTimePtr(started)
	run.FinishedAt = parseTimePtr(finished)
	run.RetryAt = parseTimePtr(retryAt)
	run.CreatedAt = parseTime(created)
	run.NotificationSent = notified != 0
	if run.Attempt == 0 {
		run.Attempt = 1
	}
	return run, nil
}
