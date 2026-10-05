package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// This computer's figures come first, named, with empty history before the
// sampler has read anything.
func TestLiveAllStartsWithThisComputer(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	all, err := a.liveAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.Config.Get()
	if len(all) == 0 || all[0].NodeID != cfg.NodeID || all[0].NodeName != cfg.NodeName {
		t.Fatalf("live: %+v", all)
	}
	if all[0].Recent == nil || all[0].Day == nil {
		t.Error("history should be empty lists, not null")
	}
}
