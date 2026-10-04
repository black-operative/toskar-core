package automations_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestRunnerRecordsThresholdNotification(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Morning price",
		Prompt:  "Check the price and end with a JSON object {\"price\": number}",
		Notification: automations.Notification{
			Mode: automations.NotifyOnCondition,
			Condition: &automations.Condition{
				Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500,
			},
		},
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}

	exec := &scriptedExec{texts: []string{
		"The laptop is $420.\n{\"price\": 420}",
		"The laptop is $640.\n{\"price\": 640}",
	}}
	notes := &recordingNotifier{}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{
		Store:  repo,
		Exec:   exec,
		Notify: notes,
		Now:    func() time.Time { return clock },
		Lease:  time.Hour,
	}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if notes.count() != 1 || notes.last().Title != "Morning price" || notes.last().Body != "The laptop is $420." {
		t.Fatalf("notices = %+v", notes.notices())
	}
	first, err := repo.RunFor(ctx, created.ID, time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !first.NotificationSent || first.Result == "" {
		t.Fatalf("first run = %+v", first)
	}

	clock = clock.Add(24 * time.Hour)
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if notes.count() != 1 {
		t.Fatalf("notices after a price above the threshold = %d", notes.count())
	}
	second, err := repo.RunFor(ctx, created.ID, time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if second.NotificationSent || second.Status != automations.RunSucceeded {
		t.Fatalf("second run = %+v", second)
	}
	if first, err = repo.RunFor(ctx, created.ID, time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)); err != nil || !first.NotificationSent {
		t.Fatalf("first flag changed: %+v err=%v", first, err)
	}
}

func TestRunnerChangeUsesTheFirstResultAsBaseline(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	if _, err := repo.Create(ctx, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Page watch",
		Prompt:       "Summarize the page",
		Notification: automations.Notification{Mode: automations.NotifyOnChange},
		Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt); err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{texts: []string{"Same page.", "Same page.", "New release."}}
	notes := &recordingNotifier{}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: notes, Now: func() time.Time { return clock }, Lease: time.Hour}
	for i := 0; i < 3; i++ {
		if err := runner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(24 * time.Hour)
	}
	if notes.count() != 1 || notes.last().Body != "New release." {
		t.Fatalf("notices = %+v", notes.notices())
	}
}

type recordingNotifier struct {
	mu    sync.Mutex
	items []automations.Notice
	err   error
}

func (n *recordingNotifier) Notify(_ context.Context, notice automations.Notice) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.items = append(n.items, notice)
	return nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.items)
}

func (n *recordingNotifier) last() automations.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.items[len(n.items)-1]
}

func (n *recordingNotifier) notices() []automations.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]automations.Notice, len(n.items))
	copy(out, n.items)
	return out
}
