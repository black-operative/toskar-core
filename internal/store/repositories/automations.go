package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/automations"
)

// AutomationRepo persists scheduled prompts.
type AutomationRepo struct {
	db *sql.DB
}

func NewAutomationRepo(db *sql.DB) *AutomationRepo {
	return &AutomationRepo{db: db}
}

const automationSelect = `
	SELECT id, name, enabled, schedule_json, time_zone, prompt,
		COALESCE(profile_id, ''), COALESCE(model_id, ''), tools_json, notification_json,
		created_at, updated_at, next_run_at, last_run_at,
		consecutive_failures, COALESCE(last_error, ''), COALESCE(response_language, '')
	FROM automations`

// Create stores an automation and computes its first next run.
// now is the creation time, so tests can pin the clock.
func (r *AutomationRepo) Create(ctx context.Context, in automations.CreateInput, now time.Time) (automations.Automation, error) {
	now = clock(now)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	a := automations.Automation{
		ID:       uuid.NewString(),
		Name:     in.Name,
		Enabled:  enabled,
		Schedule: in.Schedule,
		// Only the task: the server adds a condition's instruction at run
		// time, and drops one an older client wrote into the prompt (#204).
		Prompt:           automations.TaskPrompt(in.Prompt),
		ProfileID:        in.ProfileID,
		ModelID:          in.ModelID,
		Tools:            in.Tools,
		Notification:     in.Notification,
		ResponseLanguage: in.ResponseLanguage,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := prepareAutomation(&a); err != nil {
		return automations.Automation{}, err
	}
	if err := a.SetNextRun(now); err != nil {
		return automations.Automation{}, err
	}
	if err := r.insert(ctx, a); err != nil {
		return automations.Automation{}, err
	}
	return a, nil
}

// Get loads one automation by id.
func (r *AutomationRepo) Get(ctx context.Context, id string) (automations.Automation, error) {
	row := r.db.QueryRowContext(ctx, automationSelect+` WHERE id = ?`, id)
	a, err := scanAutomation(row)
	if err == sql.ErrNoRows {
		return automations.Automation{}, fmt.Errorf("automation %q not found", id)
	}
	return a, err
}

// List returns every automation, soonest next run first.
func (r *AutomationRepo) List(ctx context.Context) ([]automations.Automation, error) {
	rows, err := r.db.QueryContext(ctx, automationSelect+`
		ORDER BY (next_run_at IS NULL), next_run_at, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanAutomations(rows)
	if err != nil {
		return nil, err
	}
	if err := r.attachLatest(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

// Due returns enabled automations whose next run is at or before now.
func (r *AutomationRepo) Due(ctx context.Context, now time.Time) ([]automations.Automation, error) {
	rows, err := r.db.QueryContext(ctx, automationSelect+`
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY next_run_at, name`, formatTime(clock(now)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAutomations(rows)
}

// Update applies a partial change and recomputes the next run.
func (r *AutomationRepo) Update(ctx context.Context, id string, patch automations.Patch, now time.Time) (automations.Automation, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return automations.Automation{}, err
	}
	if patch.Name != nil {
		existing.Name = *patch.Name
	}
	if patch.Enabled != nil {
		existing.Enabled = *patch.Enabled
	}
	if patch.Schedule != nil {
		existing.Schedule = *patch.Schedule
	}
	if patch.Prompt != nil {
		existing.Prompt = automations.TaskPrompt(*patch.Prompt)
	}
	if patch.ProfileID != nil {
		existing.ProfileID = *patch.ProfileID
	}
	if patch.ModelID != nil {
		existing.ModelID = *patch.ModelID
	}
	if patch.Tools != nil {
		existing.Tools = *patch.Tools
	}
	if patch.ResponseLanguage != nil {
		existing.ResponseLanguage = *patch.ResponseLanguage
	}
	if patch.Notification != nil {
		existing.Notification = *patch.Notification
	}
	existing.UpdatedAt = clock(now)
	if err := prepareAutomation(&existing); err != nil {
		return automations.Automation{}, err
	}
	if err := existing.SetNextRun(existing.UpdatedAt); err != nil {
		return automations.Automation{}, err
	}
	if err := r.updateRow(ctx, existing); err != nil {
		return automations.Automation{}, err
	}
	return existing, nil
}

// SetLastRun records when an occurrence finished and advances the next run.
func (r *AutomationRepo) SetLastRun(ctx context.Context, id string, finishedAt, now time.Time) (automations.Automation, error) {
	if finishedAt.IsZero() {
		return automations.Automation{}, fmt.Errorf("finished time is required")
	}
	existing, err := r.Get(ctx, id)
	if err != nil {
		return automations.Automation{}, err
	}
	finished := clock(finishedAt)
	existing.LastRunAt = &finished
	existing.UpdatedAt = clock(now)
	if err := existing.SetNextRun(existing.UpdatedAt); err != nil {
		return automations.Automation{}, err
	}
	if err := r.updateRow(ctx, existing); err != nil {
		return automations.Automation{}, err
	}
	return existing, nil
}

// RefreshNextRuns recomputes every next run from the missed-occurrence rule.
// Call this when the daemon starts, before reading Due.
func (r *AutomationRepo) RefreshNextRuns(ctx context.Context, now time.Time) error {
	all, err := r.List(ctx)
	if err != nil {
		return err
	}
	now = clock(now)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, a := range all {
		previous := formatTimePtr(a.NextRunAt)
		if err := a.SetNextRun(now); err != nil {
			return err
		}
		if previous == formatTimePtr(a.NextRunAt) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE automations SET next_run_at = ? WHERE id = ?`, formatTimePtr(a.NextRunAt), a.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes an automation and its run rows.
func (r *AutomationRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM automations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("automation %q not found", id)
	}
	return nil
}

func (r *AutomationRepo) insert(ctx context.Context, a automations.Automation) error {
	sched, tools, note, err := marshalAutomation(a)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO automations (
			id, name, enabled, schedule_json, time_zone, prompt, profile_id, model_id,
			tools_json, notification_json, created_at, updated_at, next_run_at, last_run_at,
			consecutive_failures, last_error, response_language
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, boolInt(a.Enabled), sched, a.Schedule.TimeZone, a.Prompt, nullIfEmpty(a.ProfileID), nullIfEmpty(a.ModelID),
		tools, note, formatTime(a.CreatedAt), formatTime(a.UpdatedAt), formatTimePtr(a.NextRunAt), formatTimePtr(a.LastRunAt),
		a.ConsecutiveFailures, nullIfEmpty(a.LastError), nullIfEmpty(a.ResponseLanguage))
	return err
}

func (r *AutomationRepo) updateRow(ctx context.Context, a automations.Automation) error {
	return updateAutomation(ctx, r.db, a)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func updateAutomation(ctx context.Context, db execer, a automations.Automation) error {
	sched, tools, note, err := marshalAutomation(a)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		UPDATE automations SET
			name = ?, enabled = ?, schedule_json = ?, time_zone = ?, prompt = ?, profile_id = ?, model_id = ?,
			tools_json = ?, notification_json = ?, updated_at = ?, next_run_at = ?, last_run_at = ?,
			consecutive_failures = ?, last_error = ?, response_language = ?
		WHERE id = ?`,
		a.Name, boolInt(a.Enabled), sched, a.Schedule.TimeZone, a.Prompt, nullIfEmpty(a.ProfileID), nullIfEmpty(a.ModelID),
		tools, note, formatTime(a.UpdatedAt), formatTimePtr(a.NextRunAt), formatTimePtr(a.LastRunAt),
		a.ConsecutiveFailures, nullIfEmpty(a.LastError), nullIfEmpty(a.ResponseLanguage), a.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("automation %q not found", a.ID)
	}
	return nil
}

func prepareAutomation(a *automations.Automation) error {
	a.Name = strings.TrimSpace(a.Name)
	a.Prompt = strings.TrimSpace(a.Prompt)
	a.ProfileID = strings.TrimSpace(a.ProfileID)
	a.ModelID = strings.TrimSpace(a.ModelID)
	a.ResponseLanguage = strings.TrimSpace(a.ResponseLanguage)
	if a.ResponseLanguage == automations.ResponseAccount {
		a.ResponseLanguage = ""
	}
	if !automations.ValidResponseLanguage(a.ResponseLanguage) {
		return fmt.Errorf("response_language must be account, app, auto, or a language tag such as de")
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	cleaned := make([]string, len(a.Tools))
	for i, id := range a.Tools {
		cleaned[i] = strings.TrimSpace(id)
	}
	a.Tools = cleaned
	a.Notification.Normalize()
	return automations.ValidateDraft(a.Name, a.Prompt, a.ModelID, a.Tools, a.Notification, a.Schedule)
}

func marshalAutomation(a automations.Automation) (schedule, tools, notification string, err error) {
	sb, err := json.Marshal(a.Schedule)
	if err != nil {
		return "", "", "", err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	tb, err := json.Marshal(a.Tools)
	if err != nil {
		return "", "", "", err
	}
	nb, err := json.Marshal(a.Notification)
	if err != nil {
		return "", "", "", err
	}
	return string(sb), string(tb), string(nb), nil
}

type automationScanner interface {
	Scan(dest ...any) error
}

func scanAutomation(s automationScanner) (automations.Automation, error) {
	var a automations.Automation
	var enabled int
	var sched, zone, tools, note, created, updated string
	var next, last sql.NullString
	if err := s.Scan(
		&a.ID, &a.Name, &enabled, &sched, &zone, &a.Prompt, &a.ProfileID, &a.ModelID, &tools, &note,
		&created, &updated, &next, &last, &a.ConsecutiveFailures, &a.LastError, &a.ResponseLanguage,
	); err != nil {
		return automations.Automation{}, err
	}
	if err := json.Unmarshal([]byte(sched), &a.Schedule); err != nil {
		return automations.Automation{}, fmt.Errorf("schedule: %w", err)
	}
	if a.Schedule.TimeZone == "" {
		a.Schedule.TimeZone = zone
	}
	if err := json.Unmarshal([]byte(tools), &a.Tools); err != nil {
		return automations.Automation{}, fmt.Errorf("tools: %w", err)
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if err := json.Unmarshal([]byte(note), &a.Notification); err != nil {
		return automations.Automation{}, fmt.Errorf("notification: %w", err)
	}
	a.Enabled = enabled != 0
	a.CreatedAt = parseTime(created)
	a.UpdatedAt = parseTime(updated)
	a.NextRunAt = parseTimePtr(next)
	a.LastRunAt = parseTimePtr(last)
	return a, nil
}

func scanAutomations(rows *sql.Rows) ([]automations.Automation, error) {
	var out []automations.Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []automations.Automation{}
	}
	return out, rows.Err()
}

func clock(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC().Truncate(time.Second)
	}
	return t.UTC().Truncate(time.Second)
}

func formatTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	if t.IsZero() {
		return nil
	}
	return &t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
