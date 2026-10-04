package automations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// Running out of memory is not retried, and two in a row pause the
// automation with an explanation instead of failing every day (§60).
func TestRunnerPausesAfterRepeatedOutOfMemory(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID:      "big-model",
		Name:         "Nightly report",
		Prompt:       "Summarize the day",
		Notification: automations.Notification{Mode: automations.NotifyAlways},
		Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{err: errExec("llama-server: failed to allocate buffer: out of memory")}
	notes := &recordingNotifier{}
	var paused []string
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{
		Store:  repo,
		Exec:   exec,
		Notify: notes,
		Now:    func() time.Time { return clock },
		Lease:  time.Hour,
		Pause: func(ctx context.Context, id string) error {
			paused = append(paused, id)
			enabled := false
			_, err := repo.Update(ctx, id, automations.Patch{Enabled: &enabled}, clock)
			return err
		},
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 1 || len(paused) != 0 {
		t.Fatalf("after one failure: runs=%d paused=%v", exec.count(), paused)
	}

	clock = clock.Add(24 * time.Hour)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 2 {
		t.Fatalf("runs = %d; out of memory must not be retried", exec.count())
	}
	if len(paused) != 1 || paused[0] != created.ID {
		t.Fatalf("paused = %v", paused)
	}
	if notes.count() != 1 || !strings.Contains(notes.last().Body, "Paused after running out of memory 2 times") {
		t.Fatalf("notices = %+v", notes.notices())
	}

	clock = clock.Add(24 * time.Hour)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.count() != 2 {
		t.Fatalf("a paused automation ran again (runs = %d)", exec.count())
	}
}
