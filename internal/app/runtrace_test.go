package app

import (
	"errors"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestRunTraceFromStreamAndEvents(t *testing.T) {
	c := runlog.New("r", "", "", "chat")
	in := make(chan pluginapi.ChatChunk, 3)
	in <- pluginapi.ChatChunk{Content: "Hi"}
	in <- pluginapi.ChatChunk{Content: "!", Done: true, Metrics: &pluginapi.GenerationMetrics{PromptTokens: 40, CompletionTokens: 2, CachedTokens: 30, EvalTokPerSec: 55}}
	close(in)
	var text string
	for chunk := range traceGeneration(c, in, "m", "assistant", "This Mac", time.Now().Add(-time.Second), nil) {
		text += chunk.Content
	}
	traceEvent(c, simple.EventPlanCreated, map[string]any{"steps": []string{"a", "b"}, "parallel": false})
	traceEvent(c, simple.EventLookup, map[string]any{"query": "x"})
	traceEvent(c, simple.EventEffort, map[string]any{"effort": "thorough"})
	traceEvent(c, simple.EventVerified, map[string]any{"issues": 1, "fixed": 1})
	// The stream is consumed; give the wrapper's goroutine a moment to record.
	time.Sleep(20 * time.Millisecond)
	r := c.Finish(runlog.StatusCompleted, nil)
	if text != "Hi!" || len(r.Models) != 1 || r.Models[0].CachedTokens != 30 || r.Models[0].FirstTokenMs < 1000 || r.Models[0].TokPerSec != 55 {
		t.Fatalf("text=%q models=%+v", text, r.Models)
	}
	if r.Workers != 2 || r.Effort != "Thorough" || r.VerificationPasses != 1 || len(r.Strategy) != 2 ||
		r.Strategy[0] != "Worked through 2 parts one after another" || r.Strategy[1] != "Looked up the web first" {
		t.Fatalf("run = %+v", r)
	}
	if traceGeneration(nil, in, "m", "", "", time.Now(), nil) != (<-chan pluginapi.ChatChunk)(in) {
		t.Fatal("no collector should pass the stream through")
	}
}

// A run's notes are written in the App language (multilingual spec §16).
func TestRunTraceInAppLanguage(t *testing.T) {
	c := runlog.New("r", "", "", "chat")
	c.SetLanguage("de")
	traceEvent(c, simple.EventPlanCreated, map[string]any{"steps": []string{"a", "b", "c"}, "parallel": true})
	traceEvent(c, simple.EventEffort, map[string]any{"effort": "thorough"})
	c.Note("otherModel", map[string]any{"model": "Qwen 14B"})
	r := c.Finish(runlog.StatusCompleted, nil)
	if r.Effort != "Gründlich" || len(r.Strategy) != 2 || r.Strategy[0] != "3 Teile parallel bearbeitet" ||
		r.Strategy[1] != "Mit einem anderen Modell geantwortet, nachdem Qwen 14B fehlgeschlagen ist" {
		t.Fatalf("run = %+v", r)
	}
}

// A failed run keeps its error's stable code, so clients show it in the App
// language (multilingual spec §10).
func TestRunErrorHasItsCode(t *testing.T) {
	r := runlog.New("r", "", "", "chat").Finish(runlog.StatusFailed, codedChatError("llama-server: failed to allocate buffer"))
	if r.Error != "llama-server: failed to allocate buffer" || r.ErrorCode != "OUT_OF_MEMORY" {
		t.Fatalf("run = %+v", r)
	}
	r = runlog.New("r", "", "", "chat").Finish(runlog.StatusFailed, codedError(contracts.NewError("MODEL_NOT_INSTALLED", map[string]any{"model_id": "qwen"}, errors.New("model qwen is not installed"))))
	if r.ErrorCode != "MODEL_NOT_INSTALLED" || r.ErrorDetails["model_id"] != "qwen" {
		t.Fatalf("run = %+v", r)
	}
	if r := runlog.New("r", "", "", "chat").Finish(runlog.StatusFailed, codedError(errors.New("something odd"))); r.Error != "something odd" || r.ErrorCode != "" {
		t.Fatalf("run = %+v", r)
	}
	if r := runlog.New("r", "", "", "chat").Finish(runlog.StatusCompleted, nil); r.Error != "" || r.ErrorCode != "" {
		t.Fatalf("run = %+v", r)
	}
}
