package repositories_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestAutomationPersistence(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var migrated int
	if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = 7`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("migration 7 applied = %d", migrated)
	}

	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	friday := int(time.Friday)

	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    " Morning price ",
		Prompt:  " Check the price ",
		Schedule: automations.Schedule{
			Kind: automations.KindDaily, TimeZone: "America/Los_Angeles", Hour: 8,
		},
		ProfileID: "general-assistant",
		Tools:     []string{"web.search"},
	}, createdAt)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "Morning price" || created.Prompt != "Check the price" {
		t.Fatalf("trimmed name/prompt = %q / %q", created.Name, created.Prompt)
	}
	if created.Notification.Mode != automations.NotifyAlways {
		t.Fatalf("notification = %q", created.Notification.Mode)
	}
	wantNext := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	if created.NextRunAt == nil || !created.NextRunAt.Equal(wantNext) {
		t.Fatalf("next = %v, want %s", created.NextRunAt, wantNext.Format(time.RFC3339))
	}

	loaded, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Schedule.Hour != 8 || loaded.Schedule.TimeZone != "America/Los_Angeles" || loaded.ProfileID != "general-assistant" {
		t.Fatalf("round trip = %+v", loaded.Schedule)
	}
	if len(loaded.Tools) != 1 || loaded.Tools[0] != "web.search" {
		t.Fatalf("tools = %#v", loaded.Tools)
	}

	later := time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC)
	if err := repo.RefreshNextRuns(ctx, later); err != nil {
		t.Fatal(err)
	}
	loaded, err = repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	missed := time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)
	if loaded.NextRunAt == nil || !loaded.NextRunAt.Equal(missed) {
		t.Fatalf("refreshed next = %v, want %s", loaded.NextRunAt, missed.Format(time.RFC3339))
	}

	due, err := repo.Due(ctx, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != created.ID {
		t.Fatalf("due = %+v", due)
	}

	paused := false
	loaded, err = repo.Update(ctx, created.ID, automations.Patch{Enabled: &paused}, later)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled {
		t.Fatal("expected paused")
	}
	due, err = repo.Due(ctx, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("paused automation was due: %+v", due)
	}
	listed, err := repo.List(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v len=%d", err, len(listed))
	}

	finished, err := repo.SetLastRun(ctx, created.ID, missed, later)
	if err != nil {
		t.Fatal(err)
	}
	nextMorning := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	if finished.NextRunAt == nil || !finished.NextRunAt.Equal(nextMorning) {
		t.Fatalf("after last run next = %v, want %s", finished.NextRunAt, nextMorning.Format(time.RFC3339))
	}

	weekly, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Friday release",
		Prompt:  "Summarize the release",
		Schedule: automations.Schedule{
			Kind: automations.KindWeekly, TimeZone: "UTC", Hour: 9, Weekday: &friday,
		},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`
		INSERT INTO automation_runs (id, automation_id, occurrence_at)
		VALUES ('run-1', ?, ?)`, weekly.ID, "2026-09-25T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`
		INSERT INTO automation_runs (id, automation_id, occurrence_at)
		VALUES ('run-2', ?, ?)`, weekly.ID, "2026-09-25T09:00:00Z"); err == nil {
		t.Fatal("expected duplicate occurrence to fail")
	}
	if err := repo.Delete(ctx, weekly.ID); err != nil {
		t.Fatal(err)
	}
	var runs int
	if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM automation_runs WHERE automation_id = ?`, weekly.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("runs after delete = %d", runs)
	}
	if _, err := repo.Get(ctx, weekly.ID); err == nil {
		t.Fatal("expected get after delete to fail")
	}
	if err := repo.Delete(ctx, weekly.ID); err == nil {
		t.Fatal("expected delete of missing automation to fail")
	}
}

func TestOneTimeCompletionClearsNextRun(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Tomorrow morning",
		Prompt:  "Run the research prompt",
		Schedule: automations.Schedule{
			Kind: automations.KindOnce, TimeZone: "UTC", At: &at,
		},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if created.NextRunAt == nil || !created.NextRunAt.Equal(at) {
		t.Fatalf("next = %v", created.NextRunAt)
	}

	finished, err := repo.SetLastRun(ctx, created.ID, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if finished.NextRunAt != nil {
		t.Fatalf("completed one-time still has next run %s", finished.NextRunAt.Format(time.RFC3339))
	}
	due, err := repo.Due(ctx, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("completed one-time was due: %+v", due)
	}
}

func TestAutomationCreateRejectsEmptyPrompt(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewAutomationRepo(db.SQL)
	_, err = repo.Create(context.Background(), automations.CreateInput{
		ModelID:  "model-a",
		Name:     "Nameless work",
		Schedule: automations.Schedule{Kind: automations.KindOnce, TimeZone: "UTC", At: timePtr(time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC))},
	}, time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected missing prompt to fail")
	}
}

func TestAutomationCreateRejectsMissingModel(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewAutomationRepo(db.SQL)
	_, err = repo.Create(context.Background(), automations.CreateInput{
		Name:     "Morning price",
		Prompt:   "Check the price",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("expected missing model to fail, got %v", err)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// An automation keeps its response language (multilingual spec §22).
func TestAutomationResponseLanguage(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := repositories.NewAutomationRepo(db.SQL)
	in := automations.CreateInput{
		ModelID: "model-a", Name: "Preis", Prompt: "Prüfe den Preis",
		Schedule:         automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		ResponseLanguage: "de",
	}
	created, err := repo.Create(ctx, in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, created.ID)
	if got.ResponseLanguage != "de" {
		t.Fatalf("response language = %q", got.ResponseLanguage)
	}
	auto := "auto"
	if got, err = repo.Update(ctx, created.ID, automations.Patch{ResponseLanguage: &auto}, time.Now()); err != nil || got.ResponseLanguage != "auto" {
		t.Fatalf("patched = %q, %v", got.ResponseLanguage, err)
	}
	// "account" is the default, stored as none.
	account := "account"
	if got, err = repo.Update(ctx, created.ID, automations.Patch{ResponseLanguage: &account}, time.Now()); err != nil || got.ResponseLanguage != "" {
		t.Fatalf("account = %q, %v", got.ResponseLanguage, err)
	}
	in.ResponseLanguage = "German!"
	if _, err := repo.Create(ctx, in, time.Now()); err == nil {
		t.Fatal("a response language that isn't a tag was accepted")
	}
}
