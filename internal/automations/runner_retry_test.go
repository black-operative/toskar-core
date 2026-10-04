package automations_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestRunnerRetriesTransientFailureAfterBackoff(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	clock := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := clock.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Morning check",
		Prompt:       "Check the page",
		Notification: automations.Notification{Mode: automations.NotifyNone},
		Schedule:     automations.Schedule{Kind: automations.KindOnce, TimeZone: "UTC", At: &at},
	}, clock.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{
		errs:    []error{errExec("connection refused"), nil},
		text:    "page is up",
		modelID: "model-a",
		nodeID:  "node-a",
	}
	notes := &recordingNotifier{}
	runner := &automations.Runner{
		Store: repo, Exec: exec, Notify: notes,
		Now: func() time.Time { return clock }, Lease: time.Hour,
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 || notes.count() != 0 {
		t.Fatalf("exec=%d notices=%d", exec.count(), notes.count())
	}
	run, err := repo.RunFor(ctx, created.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunRetrying || run.Attempt != 1 || run.RetryAt == nil || !run.RetryAt.Equal(clock.Add(time.Minute)) {
		t.Fatalf("waiting run = %+v", run)
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != 0 || loaded.LastError != "connection refused" {
		t.Fatalf("automation while waiting = %+v", loaded)
	}

	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 {
		t.Fatalf("retried before the backoff, executions = %d", exec.count())
	}

	clock = clock.Add(time.Minute)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 2 {
		t.Fatalf("executions = %d", exec.count())
	}
	run, err = repo.RunFor(ctx, created.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunSucceeded || run.Result != "page is up" || run.NotificationSent {
		t.Fatalf("recovered run = %+v", run)
	}
	loaded, err = repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != 0 || loaded.LastError != "" {
		t.Fatalf("automation after recovery = failures %d error %q", loaded.ConsecutiveFailures, loaded.LastError)
	}
}

func TestRunnerStopsTransientRetriesAtTheBound(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	clock := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := clock.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Morning check",
		Prompt:       "Check the page",
		Notification: automations.Notification{Mode: automations.NotifyNone},
		Schedule:     automations.Schedule{Kind: automations.KindOnce, TimeZone: "UTC", At: &at},
	}, clock.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{errs: []error{
		errExec("connection refused"),
		errExec("connection refused"),
		errExec("connection refused"),
		errExec("connection refused"),
	}}
	notes := &recordingNotifier{}
	runner := &automations.Runner{
		Store: repo, Exec: exec, Notify: notes,
		Now: func() time.Time { return clock }, Lease: time.Hour,
	}
	steps := []time.Duration{0, time.Minute, 5 * time.Minute, 15 * time.Minute}
	for i, step := range steps {
		clock = clock.Add(step)
		if err := runner.Tick(ctx); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if exec.count() != automations.MaxAttempts {
		t.Fatalf("executions = %d", exec.count())
	}
	if notes.count() != 1 || notes.last().Title != "Morning check" {
		t.Fatalf("notices = %+v", notes.notices())
	}
	run, err := repo.RunFor(ctx, created.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunFailed || run.Attempt != automations.MaxAttempts || !run.NotificationSent {
		t.Fatalf("run = %+v", run)
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != 1 || loaded.LastError != "connection refused" || loaded.NextRunAt != nil {
		t.Fatalf("automation = failures %d error %q next %v", loaded.ConsecutiveFailures, loaded.LastError, loaded.NextRunAt)
	}
}

func TestPermanentFailureIsNotRetried(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	clock := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := clock.Add(-time.Minute)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:  "model-a",
		Name:     "Load model",
		Prompt:   "Summarize",
		Schedule: automations.Schedule{Kind: automations.KindOnce, TimeZone: "UTC", At: &at},
	}, clock.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{err: errExec("model ran out of memory")}
	notes := &recordingNotifier{}
	runner := &automations.Runner{
		Store: repo, Exec: exec, Notify: notes,
		Now: func() time.Time { return clock }, Lease: time.Hour,
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Hour)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 || notes.count() != 0 {
		t.Fatalf("exec=%d notices=%d", exec.count(), notes.count())
	}
	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != 1 || loaded.LastError != "model ran out of memory" {
		t.Fatalf("automation = %+v", loaded)
	}
}

func TestRepeatedPermanentFailuresNotify(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	clock := createdAt.Add(90 * time.Minute)
	if _, err := repo.Create(ctx, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Stock check",
		Prompt:       "Check stock",
		Notification: automations.Notification{Mode: automations.NotifyNone},
		Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt); err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{err: errExec(`tool "internet.search" denied by policy`)}
	notes := &recordingNotifier{}
	runner := &automations.Runner{
		Store: repo, Exec: exec, Notify: notes,
		Now: func() time.Time { return clock }, Lease: time.Hour,
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 || notes.count() != 0 {
		t.Fatalf("first failure exec=%d notices=%d", exec.count(), notes.count())
	}
	clock = clock.Add(24 * time.Hour)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 2 || notes.count() != 1 {
		t.Fatalf("second failure exec=%d notices=%d", exec.count(), notes.count())
	}
	if notes.last().Body == "" || notes.last().Title != "Stock check" {
		t.Fatalf("notice = %+v", notes.last())
	}
}
