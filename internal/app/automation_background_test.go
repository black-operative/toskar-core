package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestSavedScheduleKeepsTheDaemonRunning(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	settings := repositories.NewSettingsRepo(db.SQL)
	repo := repositories.NewAutomationRepo(db.SQL)
	application := &App{Settings: settings, Automations: repo}

	if err := application.enableBackgroundWhenScheduled(ctx); err != nil {
		t.Fatal(err)
	}
	on, err := settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Fatal("background mode turned on with no schedules")
	}

	if _, err := repo.Create(ctx, automations.CreateInput{
		ModelID:  "model-a",
		Name:     "Morning price",
		Prompt:   "Check the price",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := application.enableBackgroundWhenScheduled(ctx); err != nil {
		t.Fatal(err)
	}
	on, err = settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("background mode stayed off after a schedule was saved")
	}
}
