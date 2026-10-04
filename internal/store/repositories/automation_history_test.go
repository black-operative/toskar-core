package repositories_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestAutomationHistory(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var migrated int
	if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = 8`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("migration 8 applied = %d", migrated)
	}

	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Morning price",
		Prompt:  "Check the price",
		Schedule: automations.Schedule{
			Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8,
		},
		ProfileID: "general-assistant",
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}

	empty, err := repo.History(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Name != "Morning price" || len(empty.History) != 0 {
		t.Fatalf("empty history = %+v", empty)
	}

	other, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Other",
		Prompt:  "Do not mix me in",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: timePtr(createdAt.Add(time.Hour)),
		},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.Claim(ctx, other.ID, createdAt.Add(time.Hour), createdAt, time.Hour); err != nil || !ok {
		t.Fatalf("other claim ok=%v err=%v", ok, err)
	}

	dayOne := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	dayTwo := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	dayThree := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)

	finish := func(occurrence, finished time.Time, result automations.Execution, fail string) automations.Run {
		t.Helper()
		run, ok, err := repo.Claim(ctx, created.ID, occurrence, occurrence, time.Hour)
		if err != nil || !ok {
			t.Fatalf("claim %s ok=%v err=%v", occurrence, ok, err)
		}
		if err := repo.MarkRunning(ctx, run.ID, occurrence.Add(5*time.Second), time.Hour); err != nil {
			t.Fatal(err)
		}
		if fail != "" {
			if err := repo.FailRun(ctx, run.ID, fail, result, finished); err != nil {
				t.Fatal(err)
			}
		} else if err := repo.CompleteRun(ctx, run.ID, result, finished); err != nil {
			t.Fatal(err)
		}
		return run
	}

	finish(dayOne, dayOne.Add(20*time.Second), automations.Execution{Text: "price is $510", ModelID: "model-a", NodeID: "node-a"}, "")
	finish(dayThree, dayThree.Add(40*time.Second), automations.Execution{Text: "could not load", ModelID: "model-b", NodeID: "node-b"}, "model failed to start")
	finish(dayTwo, dayTwo.Add(30*time.Second), automations.Execution{Text: "price is $480", ModelID: "model-a", NodeID: "node-a"}, "")

	detail, err := repo.History(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.History) != 3 {
		t.Fatalf("history len = %d", len(detail.History))
	}
	wantOrder := []time.Time{dayThree, dayTwo, dayOne}
	for i, want := range wantOrder {
		if !detail.History[i].OccurrenceAt.Equal(want) {
			t.Fatalf("history[%d] occurrence = %s, want %s", i, detail.History[i].OccurrenceAt.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}

	latest := detail.History[0]
	if latest.Status != automations.RunFailed || latest.Error != "model failed to start" || latest.Result != "could not load" {
		t.Fatalf("latest = %+v", latest)
	}
	if latest.ModelID != "model-b" || latest.NodeID != "node-b" || latest.NotificationSent {
		t.Fatalf("latest placement = %+v", latest)
	}
	if latest.StartedAt == nil || !latest.StartedAt.Equal(dayThree.Add(5*time.Second)) {
		t.Fatalf("latest start = %v", latest.StartedAt)
	}
	if latest.FinishedAt == nil || !latest.FinishedAt.Equal(dayThree.Add(40*time.Second)) {
		t.Fatalf("latest finish = %v", latest.FinishedAt)
	}

	middle := detail.History[1]
	if middle.Status != automations.RunSucceeded || middle.Result != "price is $480" || middle.Error != "" {
		t.Fatalf("middle = %+v", middle)
	}
	if detail.LastRunAt == nil || !detail.LastRunAt.Equal(dayTwo.Add(30*time.Second)) {
		t.Fatalf("last run on automation = %v", detail.LastRunAt)
	}

	listed, err := repo.ListRuns(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].AutomationID != other.ID || listed[0].Status != automations.RunClaimed {
		t.Fatalf("other history = %+v", listed)
	}

	if _, err := repo.History(ctx, "missing"); err == nil {
		t.Fatal("expected missing automation to fail")
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var morning automations.Automation
	for _, item := range all {
		if item.ID == created.ID {
			morning = item
		}
	}
	if morning.LastStatus != automations.RunFailed || morning.LastResult != "could not load" {
		t.Fatalf("list latest = %s %q", morning.LastStatus, morning.LastResult)
	}
}
