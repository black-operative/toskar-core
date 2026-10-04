package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/models/lifecycle"
)

func TestSweeperUnloadsIdle(t *testing.T) {
	stopped := []string{}
	reported := []string{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := &lifecycle.Sweeper{
		List: func(ctx context.Context) ([]lifecycle.Instance, error) {
			return []lifecycle.Instance{{
				InstanceID: "inst-1",
				ModelID:    "m1",
				LastUsed:   now.Add(-20 * time.Minute),
			}}, nil
		},
		Stop: func(ctx context.Context, instanceID string) error {
			stopped = append(stopped, instanceID)
			return nil
		},
		Settings: func(ctx context.Context) (string, int, error) {
			return "automatic", 15, nil
		},
		OnUnload: func(inst lifecycle.Instance) { reported = append(reported, inst.ModelID) },
	}
	s.TickOnce(context.Background(), now)
	if len(stopped) != 1 || stopped[0] != "inst-1" {
		t.Fatalf("expected unload, got %v", stopped)
	}
	if len(reported) != 1 || reported[0] != "m1" {
		t.Fatalf("the unload must be reported, got %v", reported)
	}
}

func TestSweeperSkipsManual(t *testing.T) {
	stopped := 0
	s := &lifecycle.Sweeper{
		List: func(ctx context.Context) ([]lifecycle.Instance, error) {
			return []lifecycle.Instance{{InstanceID: "x", ModelID: "m", LastUsed: time.Now().Add(-time.Hour)}}, nil
		},
		Stop: func(ctx context.Context, instanceID string) error {
			stopped++
			return nil
		},
		Settings: func(ctx context.Context) (string, int, error) {
			return "manual", 15, nil
		},
	}
	s.TickOnce(context.Background(), time.Now())
	if stopped != 0 {
		t.Fatal("should not unload in manual mode")
	}
}
