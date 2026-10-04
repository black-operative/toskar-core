package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
)

// Every tool, whatever it comes from, has a descriptor with a permission
// level worked out from what it can affect (Gungnir §7, §10).
func TestDescriptors(t *testing.T) {
	want := map[string]int{
		"filesystem.read": LevelLocal, "files.create": LevelLocal, "internet.search": LevelReadExternal,
		"filesystem.write": LevelChange, "git.push": LevelChange, "terminal": LevelHighImpact,
	}
	for id, level := range want {
		def, ok := Lookup(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		d := Describe(def)
		if d.Level != level || d.LevelName == "" {
			t.Errorf("%s level %d %q, want %d", id, d.Level, d.LevelName, level)
		}
		if d.Version != 1 || d.Execution != "local" || d.Provider != "builtin" || !d.Supports.Cancel || d.TimeoutSeconds <= 0 {
			t.Errorf("%s descriptor %+v", id, d)
		}
	}
	created := Describe(mustLookup(t, "files.create"))
	if len(created.Outputs) != 1 || created.Outputs[0] != OutputFile {
		t.Fatalf("files.create outputs %v", created.Outputs)
	}
	search := Describe(mustLookup(t, "internet.search"))
	props, _ := search.InputSchema["properties"].(map[string]any)
	if q, _ := props["query"].(map[string]any); q["type"] != "string" || !search.Requirements.Network {
		t.Fatalf("internet.search schema %v network %v", search.InputSchema, search.Requirements.Network)
	}

	SetConnected("connector:demo", []Definition{
		{ID: "demo.read", Source: "connector:demo", Risk: RiskRead, Schema: `{}`},
		{ID: "demo.post", Source: "connector:demo", Risk: RiskWrite, Schema: `{}`},
	})
	defer SetConnected("connector:demo", nil)
	if d := Describe(mustLookup(t, "demo.read")); d.Level != LevelReadExternal || !d.Requirements.Credentials || d.Provider != "connector:demo" {
		t.Fatalf("connector read %+v", d)
	}
	if d := Describe(mustLookup(t, "demo.post")); d.Level != LevelChange {
		t.Fatalf("connector write level %d", d.Level)
	}
}

func mustLookup(t *testing.T, id string) Definition {
	t.Helper()
	def, ok := Lookup(id)
	if !ok {
		t.Fatalf("%s missing", id)
	}
	return def
}

// Every call is audited with what became of it and how it was allowed
// (§13).
func TestToolCallsAreAudited(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	log := NewAuditLog(db.SQL)
	r := &Registry{
		tools:    map[string]Tool{"internet.search": stubTool{id: "internet.search"}, "terminal": stubTool{id: "terminal"}},
		policy:   NewPolicyEngine(),
		bus:      events.NewBus(8),
		pending:  map[string]*PendingCall{},
		disabled: map[string]struct{}{},
	}
	r.SetAudit(log)
	ctx := egress.WithRun(context.Background(), egress.Run{Source: egress.SourceChat})
	meta := map[string]any{"conversation_id": "c1"}

	if _, err := r.Execute(ctx, "web.search", map[string]any{"query": "weather in Oslo"}, PolicyAllow, "", meta); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ctx, "terminal", map[string]any{"command": "rm -rf /tmp/x"}, PolicyDeny, "", meta); err == nil {
		t.Fatal("a denied tool ran")
	}
	if _, err := r.Execute(ctx, "internet.search", map[string]any{"query": ""}, PolicyAllow, "", meta); err == nil {
		t.Fatal("a malformed call ran")
	}
	r.disabled["terminal"] = struct{}{}
	_, _ = r.Execute(ctx, "terminal", map[string]any{"command": "ls"}, PolicyAllow, "", meta)

	runs, err := log.List(context.Background(), AuditFilter{ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ToolRun{}
	for _, run := range runs {
		got[run.Status] = run
	}
	ok := got[RunCompleted]
	if ok.ToolID != "internet.search" || ok.Approval != ApprovedByProfile || ok.Summary != "weather in Oslo" || ok.Source != egress.SourceChat {
		t.Fatalf("completed %+v", ok)
	}
	if d := got[RunDenied]; d.ToolID != "terminal" || d.Error == "" {
		t.Fatalf("denied %+v", d)
	}
	if got[RunRefused].ToolID != "internet.search" || got[RunDisabled].ToolID != "terminal" {
		t.Fatalf("runs %+v", runs)
	}
	if only, _ := log.List(context.Background(), AuditFilter{ToolID: "web.search"}); len(only) != 2 {
		t.Fatalf("filter by tool (alias) = %d runs", len(only))
	}
}
