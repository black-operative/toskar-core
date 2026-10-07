package app

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/tools/internet"
)

func TestTurnTraceRecordsSourcesAndSteps(t *testing.T) {
	tr := &turnTrace{}
	if tr.meta() != nil || tr.sawUntrusted() {
		t.Fatal("an empty turn has no meta and no untrusted content")
	}
	tr.knowledge([]mimir.Hit{
		{SourceName: "inventory.csv", Title: "inventory.csv row 1", Body: "sku: MP-1; price: 189.99"},
		{SourceName: "inventory.csv", Title: "inventory.csv row 2", Body: "sku: MP-2"},
	})
	tr.tool("internet.search", map[string]any{"query": "tire pressure"}, map[string]any{"results": []internet.Result{
		{Title: "PSI guide", URL: "https://example.com/psi", Snippet: "Use the door sticker."},
		{Title: "", URL: "https://www.tires.example/faq"},
	}})
	tr.tool("internet.open", map[string]any{"url": "https://example.com/psi"}, map[string]any{
		"url": "https://example.com/psi", "title": "Tire pressure, explained", "content": strings.Repeat("word ", 200)})

	meta := tr.meta()
	if meta == nil || !tr.sawUntrusted() {
		t.Fatal("expected meta and untrusted content")
	}
	var steps []string
	for _, s := range meta.Steps {
		steps = append(steps, s.Text)
	}
	want := "Found 2 passages in inventory.csv|Searched the web for “tire pressure”|Read “Tire pressure, explained”"
	if strings.Join(steps, "|") != want {
		t.Fatalf("steps = %v", steps)
	}
	// Two knowledge passages, two search hits; the opened page renames its hit.
	if len(meta.Sources) != 4 {
		t.Fatalf("sources = %+v", meta.Sources)
	}
	if meta.Sources[2].Title != "Tire pressure, explained" || meta.Sources[3].Title != "tires.example" {
		t.Fatalf("web sources = %+v", meta.Sources[2:])
	}
}

func TestTurnTraceCapsSources(t *testing.T) {
	tr := &turnTrace{}
	var hits []mimir.Hit
	for i := 0; i < 20; i++ {
		hits = append(hits, mimir.Hit{SourceName: "kb", Title: strings.Repeat("x", i+1)})
	}
	tr.knowledge(hits)
	if got := len(tr.meta().Sources); got != maxTurnSources {
		t.Fatalf("sources = %d", got)
	}
}

func TestEffectivePolicyAsksAfterUntrustedContent(t *testing.T) {
	cases := []struct {
		policy, tool string
		untrusted    bool
		want         string
	}{
		{tools.PolicyAllow, "terminal", false, tools.PolicyAllow},
		{tools.PolicyAllow, "terminal", true, tools.PolicyAsk},
		{tools.PolicyAllow, "filesystem.write", true, tools.PolicyAsk},
		{tools.PolicyAllow, "git.push", true, tools.PolicyAsk},
		{tools.PolicyAllow, "internet.open", true, tools.PolicyAllow},
		{tools.PolicyAllow, "unknown.tool", true, tools.PolicyAsk},
		{tools.PolicyDeny, "terminal", true, tools.PolicyDeny},
	}
	for _, c := range cases {
		if got := effectivePolicy(c.policy, c.tool, c.untrusted); got != c.want {
			t.Errorf("%s %s untrusted=%v: got %s, want %s", c.policy, c.tool, c.untrusted, got, c.want)
		}
	}
}

// Map results are other people's writing: the turn treats them as data and
// shows what was looked up.
func TestPlacesAreUntrustedSteps(t *testing.T) {
	tr := &turnTrace{}
	tr.tool("places.search", map[string]any{"query": "coffee", "near": "Juneau"}, map[string]any{"places": []any{}})
	tr.tool("maps.route", map[string]any{"from": "the airport", "to": "downtown"}, map[string]any{})
	if !tr.sawUntrusted() {
		t.Fatal("map results were trusted")
	}
	meta := tr.meta()
	if meta == nil || len(meta.Steps) != 2 || meta.Steps[0].Text != "Looked up coffee near Juneau" || meta.Steps[1].Text != "Found the way from the airport to downtown" {
		t.Fatalf("steps %+v", meta)
	}
}

// A made image, a clip, or files code saved are attached to the answer, so
// the chat shows them; before, only files.create's were.
func TestTurnTraceAttachesMadeFiles(t *testing.T) {
	tr := &turnTrace{}
	tr.tool("image.generate", map[string]any{"prompt": "a dog"}, map[string]any{"id": "art-1", "name": "a-dog.png", "kind": "image", "seed": 7})
	tr.tool("video.generate", nil, map[string]any{"id": "art-2", "name": "waves.mp4", "kind": "video"})
	tr.tool("code.execute", nil, map[string]any{"files": []map[string]any{{"id": "art-3", "name": "chart.png", "kind": "image", "size_bytes": int64(2048)}}})
	tr.tool("internet.search", map[string]any{"query": "x"}, map[string]any{"id": "not-a-file", "name": "x"})
	meta := tr.meta()
	if meta == nil || len(meta.Files) != 3 {
		t.Fatalf("files = %+v", meta)
	}
	if f := meta.Files[0]; f.ID != "art-1" || f.Kind != "image" || f.Producer != "assistant" {
		t.Fatalf("image = %+v", f)
	}
	if meta.Files[2].Size != 2048 {
		t.Fatalf("chart = %+v", meta.Files[2])
	}
}
