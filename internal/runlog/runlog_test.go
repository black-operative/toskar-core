package runlog

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestCollectAndStore(t *testing.T) {
	c := New("run-1", "conv-1", "general", "chat")
	ctx := With(context.Background(), c)
	if From(ctx) != c || From(context.Background()) != nil {
		t.Fatal("context round trip")
	}
	c.Strategy("Looked up the web first")
	c.Strategy("Looked up the web first")
	c.Effort("Balanced")
	c.Loaded("llama", 2*time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.ModelCall("llama", "assistant", "This Mac", 2100*time.Millisecond,
				&GenerationMetrics{TTFTMs: 90, PromptTokens: 800, CompletionTokens: 60, CachedTokens: 500, TokPerSec: 42})
			c.ToolCall("internet.search", 300*time.Millisecond, false)
		}()
	}
	wg.Wait()
	c.ToolCall("internet.open", time.Second, true)
	c.Plan(3, true)
	c.Verified(2, 1)
	c.Retried()
	c.Context(800, 8192)
	r := c.Finish(StatusCompleted, nil)

	if len(r.Strategy) != 1 || r.Effort != "Balanced" || r.Workers != 3 || !r.Parallel || r.Retries != 1 ||
		r.VerificationPasses != 1 || r.VerificationFixed != 1 || r.ContextTokens != 800 || r.CompletedAt == nil {
		t.Fatalf("run = %+v", r)
	}
	if len(r.Models) != 1 {
		t.Fatalf("models = %+v", r.Models)
	}
	m := r.Models[0]
	if m.Calls != 3 || m.LoadMs != 2000 || m.FirstTokenMs != 2100 || m.PromptTokens != 2400 || m.CachedTokens != 1500 || m.Node != "This Mac" {
		t.Fatalf("model = %+v", m)
	}
	if len(r.Tools) != 2 || r.Tools[0].ToolID != "internet.open" || r.Tools[0].Failures != 1 || r.Tools[1].Calls != 3 || r.Tools[1].TotalMs != 900 {
		t.Fatalf("tools = %+v", r.Tools)
	}
	if len(r.Nodes) != 1 || r.Nodes[0] != "This Mac" {
		t.Fatalf("nodes = %+v", r.Nodes)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewStore(db.SQL)
	if err := s.Save(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "run-1")
	if err != nil || got.Models[0].CachedTokens != 1500 || got.Status != StatusCompleted {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := s.Get(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("missing = %v", err)
	}
	list, _ := s.List(context.Background(), "conv-1", 0)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	var nilC *Collector
	nilC.ToolCall("x", time.Second, false) // safe without a run
}
