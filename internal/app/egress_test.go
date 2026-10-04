package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/tools"
)

func TestToolCallsAreRecordedAsEgress(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := &App{Egress: egress.New(db.SQL)}
	tools.SetConnected("github", []tools.Definition{{ID: "github.comment", Name: "Comment on GitHub", Source: "connector:github"}})
	t.Cleanup(func() { tools.SetConnected("github", nil) })
	ctx := egress.WithRun(context.Background(), egress.Run{Source: egress.SourceAPI, ConversationID: "c"})

	a.recordToolEgress(ctx, "internet.search", map[string]any{"query": "tide times"})
	a.recordToolEgress(ctx, "internet.open", map[string]any{"url": "https://tides.example.com/juneau"})
	a.recordToolEgress(ctx, "github.comment", map[string]any{"repo": "o/r", "number": float64(7), "body": "a long private comment"})
	a.recordToolEgress(ctx, "filesystem.read", map[string]any{"path": "notes.txt"})

	got, _ := a.Egress.List(context.Background(), egress.Filter{})
	if len(got) != 3 {
		t.Fatalf("records = %+v", got)
	}
	want := map[string]string{
		egress.WebSearch: "DuckDuckGo|tide times",
		egress.WebPage:   "tides.example.com|https://tides.example.com/juneau",
		egress.Connector: "github|Comment on GitHub (repo: o/r, number: 7)",
	}
	for _, r := range got {
		if w := want[r.Kind]; w != r.Destination+"|"+r.Detail || r.Source != egress.SourceAPI {
			t.Errorf("%s = %q|%q (%s), want %q", r.Kind, r.Destination, r.Detail, r.Source, w)
		}
	}
}

func TestThisComputerOnlyKeepsTheTurnHere(t *testing.T) {
	cfg, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *config.Config) { c.NodeID = "here" }); err != nil {
		t.Fatal(err)
	}
	env := &chatExecEnv{app: &App{Config: cfg}, trace: &turnTrace{}, roleNodes: map[string]string{"assistant": "studio"}}
	if got := env.keepLocalIfNeeded("assistant", "studio"); got != "studio" {
		t.Fatalf("moved without local-only data: %s", got)
	}
	env.markLocalOnly()
	if got := env.keepLocalIfNeeded("assistant", "studio"); got != "here" || env.roleNodes["assistant"] != "here" {
		t.Fatalf("local-only turn placed on %s", got)
	}
	env.keepLocalIfNeeded("assistant", "studio")
	if steps := env.trace.meta().Steps; len(steps) != 1 || steps[0].Kind != "share" {
		t.Fatalf("steps = %+v", steps)
	}
	if got := env.keepLocalIfNeeded("assistant", "here"); got != "here" {
		t.Fatal(got)
	}
}
