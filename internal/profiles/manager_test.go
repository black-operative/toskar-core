package profiles_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestListDoesNotDeadlockWithSingleConn(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	mgr := profiles.NewManager(db.SQL)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := mgr.Create(ctx, profiles.Profile{
			Name:           "Profile",
			Purpose:        "general",
			OrchestratorID: "simple",
			Roles: []contracts.ModelRole{
				{Role: "assistant", ModelID: "m1", Required: true},
			},
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := mgr.List(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("list: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("List deadlocked (nested query with MaxOpenConns=1)")
	}
}

func TestListEmptyRolesIsNonNil(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	mgr := profiles.NewManager(db.SQL)
	ctx := context.Background()
	if _, err := mgr.Create(ctx, profiles.Profile{
		Name:           "Bare",
		Purpose:        "custom",
		OrchestratorID: "simple",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	items, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(items))
	}
	if items[0].Roles == nil {
		t.Fatal("roles must be empty slice, not nil (JSON null breaks the UI)")
	}
}

func TestEnsurePresetsMigratesProgrammingPreferLocal(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	mgr := profiles.NewManager(db.SQL)
	ctx := context.Background()
	if _, err := mgr.Create(ctx, profiles.Profile{
		ID:             profiles.PresetProgramming,
		Name:           "Programming",
		Purpose:        "coding",
		OrchestratorID: "team",
		NodePolicy:     contracts.NodePolicy{Mode: "prefer_local"},
		Roles: []contracts.ModelRole{
			{Role: "coordinator", Required: false},
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := mgr.EnsurePresets(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got, err := mgr.Get(ctx, profiles.PresetProgramming)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NodePolicy.Mode != "automatic" {
		t.Fatalf("expected automatic, got %q", got.NodePolicy.Mode)
	}
}

func TestResetToDefaults(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	mgr := profiles.NewManager(db.SQL)
	ctx := context.Background()
	if err := mgr.EnsurePresets(ctx); err != nil {
		t.Fatalf("presets: %v", err)
	}
	created, err := mgr.Create(ctx, profiles.Profile{
		Name:           "User Profile",
		Purpose:        "general",
		OrchestratorID: "simple",
		Roles: []contracts.ModelRole{
			{Role: "assistant", ModelID: "fake-model", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	preset, err := mgr.Get(ctx, profiles.PresetGeneral)
	if err != nil {
		t.Fatalf("get preset: %v", err)
	}
	preset.Roles = []contracts.ModelRole{{Role: "assistant", ModelID: "mutated", Required: true}}
	if err := mgr.Update(ctx, preset); err != nil {
		t.Fatalf("mutate preset: %v", err)
	}

	if err := mgr.ResetToDefaults(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}

	items, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range items {
		if p.ID == created.ID {
			t.Fatal("user profile should be removed")
		}
	}
	restored, err := mgr.Get(ctx, profiles.PresetGeneral)
	if err != nil {
		t.Fatalf("get restored: %v", err)
	}
	if len(restored.Roles) != 1 || restored.Roles[0].ModelID != "" {
		t.Fatalf("preset roles not restored: %+v", restored.Roles)
	}
}
