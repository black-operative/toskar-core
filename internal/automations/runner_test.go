package automations_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestRunnerExecutesDueOccurrenceOnce(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 8, 30, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:   "model-a",
		Name:      "Morning check",
		Prompt:    "Check the price",
		ProfileID: "general-assistant",
		Schedule: automations.Schedule{
			Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8,
		},
	}, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	exec := &scriptedExec{text: "price is $420", modelID: "model-a", nodeID: "node-a"}
	bus := events.NewBus(4)
	_, eventsCh := bus.Subscribe()
	runner := &automations.Runner{
		Store: repo,
		Exec:  exec,
		Bus:   bus,
		Now:   func() time.Time { return now },
		Lease: time.Hour,
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}
	run, err := repo.RunFor(ctx, created.ID, time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunSucceeded || run.Result != "price is $420" || run.ModelID != "model-a" {
		t.Fatalf("run = %+v", run)
	}
	if run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("run missing times: %+v", run)
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	if loaded.NextRunAt == nil || !loaded.NextRunAt.Equal(wantNext) {
		t.Fatalf("next = %v, want %s", loaded.NextRunAt, wantNext.Format(time.RFC3339))
	}

	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("second tick executions = %d", exec.count())
	}
	assertEvent(t, eventsCh, events.AutomationStarted)
	assertEvent(t, eventsCh, events.AutomationCompleted)
}

func TestRunnerRecordsFailureWithoutRetry(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Once",
		Prompt:  "Research this",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: &at,
		},
	}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{err: errExec("model failed to start")}
	notes := &recordingNotifier{}
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: notes, Now: func() time.Time { return now }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}
	run, err := repo.RunFor(ctx, created.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunFailed || run.Error != "model failed to start" || run.NotificationSent {
		t.Fatalf("run = %+v", run)
	}
	if notes.count() != 0 {
		t.Fatalf("failed run sent %d notifications", notes.count())
	}
}

func TestRunnerLeavesALiveLeaseAlone(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Held",
		Prompt:  "Wait",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: &at,
		},
	}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.Claim(ctx, created.ID, at, now, time.Hour); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	exec := &scriptedExec{text: "should not run"}
	runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return now }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 0 {
		t.Fatalf("live lease was executed %d times", exec.count())
	}
}

func TestRunnerReclaimsExpiredLease(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Crashed",
		Prompt:  "Try again",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: &at,
		},
	}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	run, ok, err := repo.Claim(ctx, created.ID, at, now.Add(-time.Hour), time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := repo.MarkRunning(ctx, run.ID, now.Add(-time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{text: "recovered"}
	runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return now }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}
	loaded, err := repo.RunFor(ctx, created.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != automations.RunSucceeded || loaded.Result != "recovered" {
		t.Fatalf("run = %+v", loaded)
	}
}

func TestClaimAllowsOneWinner(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Race",
		Prompt:  "Once",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: &at,
		},
	}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Claim(ctx, created.ID, at, now, time.Hour)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	var n int
	for ok := range wins {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("claim winners = %d", n)
	}
}

func TestAbandonExpiredLeavesTheReclaimedRun(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Daily",
		Prompt:  "Check",
		Schedule: automations.Schedule{
			Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8,
		},
	}, time.Date(2026, 9, 22, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	if _, err := db.SQL.Exec(`
		INSERT INTO automation_runs (id, automation_id, occurrence_at, status, claimed_at, lease_until)
		VALUES ('old-run', ?, ?, 'running', ?, ?)`,
		created.ID, old.Format(time.RFC3339), old.Format(time.RFC3339), old.Add(time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{text: "today"}
	runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return now }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}
	abandoned, err := repo.RunFor(ctx, created.ID, old)
	if err != nil {
		t.Fatal(err)
	}
	if abandoned.Status != automations.RunFailed {
		t.Fatalf("abandoned status = %s", abandoned.Status)
	}
	current, err := repo.RunFor(ctx, created.ID, time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != automations.RunSucceeded {
		t.Fatalf("current status = %s", current.Status)
	}
}

func TestRunNowWhilePausedKeepsTheSchedule(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 8, 30, 0, 0, time.UTC)
	off := false
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:   "model-a",
		Name:      "Morning check",
		Prompt:    "Check the price",
		Enabled:   &off,
		ProfileID: "general-assistant",
		Schedule: automations.Schedule{
			Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8,
		},
		Notification: automations.Notification{Mode: automations.NotifyNone},
	}, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{text: "price is $420"}
	runner := &automations.Runner{
		Store: repo,
		Exec:  exec,
		Now:   func() time.Time { return now },
		Lease: time.Hour,
	}
	run, err := runner.RunNow(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunSucceeded || run.Result != "price is $420" {
		t.Fatalf("run = %+v", run)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled {
		t.Fatal("paused automation was enabled by run now")
	}
	wantNext := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	if loaded.NextRunAt == nil || !loaded.NextRunAt.Equal(wantNext) {
		t.Fatalf("next = %v, want %s", loaded.NextRunAt, wantNext.Format(time.RFC3339))
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("paused tick executions = %d", exec.count())
	}
}

type scriptedExec struct {
	mu      sync.Mutex
	calls   int
	text    string
	texts   []string
	errs    []error
	modelID string
	nodeID  string
	err     error
}

func (e *scriptedExec) Execute(context.Context, automations.Automation) (automations.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if len(e.errs) > 0 {
		err := e.errs[0]
		e.errs = e.errs[1:]
		if err != nil {
			return automations.Execution{}, err
		}
	} else if e.err != nil {
		return automations.Execution{}, e.err
	}
	text := e.text
	if len(e.texts) > 0 {
		text = e.texts[0]
		e.texts = e.texts[1:]
	}
	return automations.Execution{Text: text, ModelID: e.modelID, NodeID: e.nodeID}, nil
}

func (e *scriptedExec) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type errExec string

func (e errExec) Error() string { return string(e) }

func openAutomationDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertEvent(t *testing.T, ch <-chan events.Event, want string) {
	t.Helper()
	select {
	case evt := <-ch:
		if evt.Type != want {
			t.Fatalf("event = %s, want %s", evt.Type, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", want)
	}
}
