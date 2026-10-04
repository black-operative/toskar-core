package retention

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

func count(t *testing.T, db *store.DB, q string) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRetention(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	recent := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO tasks (id, prompt, status, created_at) VALUES ('old', 'old prompt', 'pending', ?), ('new', 'new prompt', 'pending', ?)`, old, recent)
	mustExec(`INSERT INTO task_steps (id, task_id, step_index, status) VALUES ('s1', 'old', 0, 'done')`)
	mustExec(`INSERT INTO automations (id, name, prompt, schedule_json, time_zone, created_at, updated_at) VALUES ('a1', 'n', 'p', '{}', 'UTC', ?, ?)`, old, old)
	mustExec(`INSERT INTO automation_runs (id, automation_id, occurrence_at, status, created_at) VALUES
		('r1', 'a1', '2026-08-01T08:00:00Z', 'succeeded', ?),
		('r2', 'a1', '2026-08-02T08:00:00Z', 'failed', ?),
		('r3', 'a1', '2026-08-03T08:00:00Z', 'succeeded', ?)`, old, old, old)
	mustExec(`INSERT INTO egress (id, at, kind, destination) VALUES ('e1', ?, 'web_search', 'duckduckgo.com'), ('e2', ?, 'web_page', 'example.com')`, old, recent)

	c, err := Before(ctx, db.SQL, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if c.Tasks != 1 || c.AutomationRuns != 2 || c.Egress != 1 {
		t.Fatalf("counts = %+v", c)
	}
	if count(t, db, `SELECT COUNT(*) FROM task_steps`) != 0 {
		t.Error("steps of a removed task remain")
	}
	if count(t, db, `SELECT COUNT(*) FROM automation_runs WHERE id = 'r3'`) != 1 {
		t.Error("the latest successful run was removed")
	}

	// Delete everything: the recent egress record goes, work that may still
	// be running and the latest result stay.
	c, err = All(ctx, db.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if c.Egress != 1 || c.Tasks != 0 || count(t, db, `SELECT COUNT(*) FROM automation_runs`) != 1 {
		t.Fatalf("all = %+v", c)
	}
}
