package automations_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// blockingExec holds each run until released or its context ends, and
// records how many ran at once.
type blockingExec struct {
	release chan struct{}
	running atomic.Int32
	most    atomic.Int32
	calls   atomic.Int32
	started chan struct{}
}

func newBlockingExec() *blockingExec {
	return &blockingExec{release: make(chan struct{}), started: make(chan struct{}, 16)}
}

func (e *blockingExec) Execute(ctx context.Context, _ automations.Automation) (automations.Execution, error) {
	e.calls.Add(1)
	now := e.running.Add(1)
	defer e.running.Add(-1)
	for {
		most := e.most.Load()
		if now <= most || e.most.CompareAndSwap(most, now) {
			break
		}
	}
	e.started <- struct{}{}
	select {
	case <-e.release:
		return automations.Execution{Text: "done"}, nil
	case <-ctx.Done():
		return automations.Execution{}, ctx.Err()
	}
}

func createDaily(t *testing.T, repo *repositories.AutomationRepo, name string, at time.Time) automations.Automation {
	t.Helper()
	created, err := repo.Create(context.Background(), automations.CreateInput{
		ModelID:      "model",
		Name:         name,
		Prompt:       "Check something",
		Notification: automations.Notification{Mode: automations.NotifyNone},
		Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func waitStarted(t *testing.T, exec *blockingExec, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-exec.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d runs started", i, n)
		}
	}
}

// A run that takes longer than its limit is stopped, fails with a code,
// and isn't retried (#204).
func TestRunStopsAtItsTimeLimit(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created := createDaily(t, repo, "Slow report", createdAt)
	exec := newBlockingExec()
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return clock }, Lease: time.Hour, RunTimeout: 50 * time.Millisecond}
	if err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := repo.RunFor(context.Background(), created.ID, time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunFailed || !strings.Contains(run.Error, "took longer than") {
		t.Fatalf("run = %+v", run)
	}
	if exec.calls.Load() != 1 {
		t.Fatalf("runs = %d; a run that timed out must not be retried", exec.calls.Load())
	}
}

// Due automations run side by side, up to the workers, so a slow one
// doesn't hold up the rest (#204).
func TestDueAutomationsRunSideBySide(t *testing.T) {
	for _, workers := range []int{1, 2} {
		db := openAutomationDB(t)
		repo := repositories.NewAutomationRepo(db.SQL)
		createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
		for _, name := range []string{"A", "B", "C"} {
			createDaily(t, repo, name, createdAt)
		}
		exec := newBlockingExec()
		clock := createdAt.Add(90 * time.Minute)
		runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return clock }, Lease: time.Hour, Workers: workers}
		done := make(chan error, 1)
		go func() { done <- runner.Tick(context.Background()) }()
		waitStarted(t, exec, workers)
		time.Sleep(50 * time.Millisecond)
		if got := exec.running.Load(); int(got) != workers {
			t.Fatalf("workers=%d: %d running at once", workers, got)
		}
		close(exec.release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if exec.calls.Load() != 3 || int(exec.most.Load()) != workers {
			t.Fatalf("workers=%d: calls=%d most=%d", workers, exec.calls.Load(), exec.most.Load())
		}
	}
}

// Run now returns when the run is claimed and goes on after the request
// ends; a second Run now, or the schedule, doesn't start another (#204).
func TestRunNowGoesOnWithoutTheRequest(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created := createDaily(t, repo, "Price watch", createdAt)
	exec := newBlockingExec()
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Now: func() time.Time { return clock }, Lease: time.Hour}

	request, closeTab := context.WithCancel(context.Background())
	started, err := runner.RunNow(request, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != automations.RunRunning {
		t.Fatalf("started = %+v", started)
	}
	closeTab()
	waitStarted(t, exec, 1)

	if _, err := runner.RunNow(context.Background(), created.ID); !errors.Is(err, automations.ErrRunning) {
		t.Fatalf("second run now: %v", err)
	}
	// The due occurrence is the one Run now took; the schedule leaves it alone.
	var ticked sync.WaitGroup
	ticked.Add(1)
	go func() {
		defer ticked.Done()
		_ = runner.Tick(context.Background())
	}()
	ticked.Wait()
	if exec.calls.Load() != 1 {
		t.Fatalf("runs = %d", exec.calls.Load())
	}

	close(exec.release)
	runner.Wait()
	run, err := repo.RunFor(context.Background(), created.ID, started.OccurrenceAt)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunSucceeded || run.Result != "done" {
		t.Fatalf("run = %+v; closing the page must not stop it", run)
	}
}

// Three failures in a row of any kind pause an automation, with a notice
// saying why (#204).
func TestRunnerPausesAfterRepeatedFailures(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created := createDaily(t, repo, "Broken report", createdAt)
	exec := &scriptedExec{err: contracts.NewError("NO_MODEL_INSTALLED", nil, errExec("no installed model"))}
	notes := &recordingNotifier{}
	var paused []string
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{
		Store: repo, Exec: exec, Notify: notes, Now: func() time.Time { return clock }, Lease: time.Hour,
		Pause: func(ctx context.Context, id string) error {
			paused = append(paused, id)
			enabled := false
			_, err := repo.Update(ctx, id, automations.Patch{Enabled: &enabled}, clock)
			return err
		},
	}
	for day := 0; day < 3; day++ {
		if err := runner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(24 * time.Hour)
	}
	if exec.count() != 3 || len(paused) != 1 || paused[0] != created.ID {
		t.Fatalf("runs=%d paused=%v", exec.count(), paused)
	}
	if !strings.Contains(notes.last().Body, "Paused after failing 3 times in a row") {
		t.Fatalf("notices = %+v", notes.notices())
	}
}

// A failure's code decides whether it's retried, whatever its text says
// (#204).
func TestClassifyFailureByCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want automations.FailureClass
	}{
		{contracts.NewError("CONNECTION_LOST", nil, errExec("la connexion a été perdue")), automations.FailureTransient},
		{contracts.NewError("COMPUTER_OFFLINE", nil, errExec("Studio")), automations.FailureTransient},
		{contracts.NewError("OUT_OF_MEMORY", nil, errExec("connection reset while loading")), automations.FailurePermanent},
		{contracts.NewError("AUTOMATION_TIMEOUT", nil, errExec("timed out")), automations.FailurePermanent},
		// No code this knows: the text decides, as before.
		{errExec("dial tcp: i/o timeout"), automations.FailureTransient},
		{contracts.NewError("SOMETHING_NEW", nil, errExec("strange")), automations.FailurePermanent},
	} {
		if got := automations.ClassifyFailure(tc.err); got != tc.want {
			t.Errorf("%v = %s, want %s", tc.err, got, tc.want)
		}
	}
}
