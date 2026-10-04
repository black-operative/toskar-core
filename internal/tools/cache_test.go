package tools

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/runlog"
)

type countingTool struct{ calls int }

func (c *countingTool) ID() string          { return "internet.search" }
func (c *countingTool) DisplayName() string { return "search" }
func (c *countingTool) Description() string { return "search" }
func (c *countingTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	c.calls++
	return map[string]any{"results": []any{"r"}}, nil
}

type mapCache map[string]map[string]any

func (m mapCache) Lookup(_ string, args map[string]any) (map[string]any, bool) {
	q, _ := args["query"].(string)
	v, ok := m[q]
	return v, ok
}
func (m mapCache) Store(_ string, args map[string]any, result map[string]any) {
	q, _ := args["query"].(string)
	m[q] = result
}

// A repeat is answered from the cache: the tool does not run, the observer
// that records what leaves the computer is not called, and the run trace
// counts the hit (§36).
func TestRegistryServesRepeatsFromCache(t *testing.T) {
	r := NewRegistry(t.TempDir(), events.NewBus(16))
	tool := &countingTool{}
	r.Register(tool)
	observed := 0
	r.SetObserver(func(context.Context, string, map[string]any) { observed++ })
	r.SetCache(mapCache{})
	run := runlog.New("r", "", "", "chat")
	ctx := runlog.With(context.Background(), run)
	for i := 0; i < 3; i++ {
		if _, err := r.Execute(ctx, "internet.search", map[string]any{"query": "tides"}, PolicyAllow, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if tool.calls != 1 || observed != 1 {
		t.Fatalf("calls=%d observed=%d", tool.calls, observed)
	}
	if got := run.Finish(runlog.StatusCompleted, nil).CacheHits["internet.search"]; got != 2 {
		t.Fatalf("cache hits = %d", got)
	}
	if acts := r.Recent(); acts[len(acts)-1].Status != "cached" {
		t.Fatalf("activity = %+v", acts[len(acts)-1])
	}
}
