package app

import (
	"time"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// minLoad is the shortest model start counted as load time; a model that is
// already running answers sooner.
const minLoad = 150 * time.Millisecond

// traceGeneration passes a model's stream through, noting when its first
// token arrived and what the runtime reported at the end (§35).
func traceGeneration(c *runlog.Collector, in <-chan pluginapi.ChatChunk, modelID, role, node string, started time.Time) <-chan pluginapi.ChatChunk {
	if c == nil {
		return in
	}
	out := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(out)
		var first time.Duration
		var metrics *pluginapi.GenerationMetrics
		for chunk := range in {
			if first == 0 && chunk.Content != "" {
				first = time.Since(started)
			}
			if chunk.Metrics != nil {
				metrics = chunk.Metrics
			}
			out <- chunk
		}
		var m *runlog.GenerationMetrics
		if metrics != nil {
			m = &runlog.GenerationMetrics{
				TTFTMs: metrics.TTFTMs, PromptTokens: metrics.PromptTokens, CompletionTokens: metrics.CompletionTokens,
				CachedTokens: metrics.CachedTokens, TokPerSec: metrics.EvalTokPerSec,
			}
		}
		c.ModelCall(modelID, role, node, first, m)
	}()
	return out
}

// traceEvent adds an orchestrator event to the run trace.
func traceEvent(c *runlog.Collector, eventType string, payload map[string]any) {
	if c == nil {
		return
	}
	switch eventType {
	case simple.EventPlanCreated:
		steps, _ := payload["steps"].([]string)
		parallel, _ := payload["parallel"].(bool)
		c.Plan(len(steps), parallel)
		switch {
		case len(steps) == 1:
			c.Note("drafted", nil)
		case parallel:
			c.Note("partsParallel", map[string]any{"count": len(steps)})
		default:
			c.Note("partsInOrder", map[string]any{"count": len(steps)})
		}
	case simple.EventEffort:
		if e, _ := payload["effort"].(string); e != "" {
			c.Effort(huginn.ParseEffort(e).Describe(c.Language()))
		}
	case simple.EventVerified, simple.EventCodeChecked, simple.EventLinksChecked:
		issues, _ := payload["issues"].(int)
		fixed, _ := payload["fixed"].(int)
		c.Verified(issues, fixed)
	case simple.EventConsistency:
		found, _ := payload["found"].(int)
		fixed, _ := payload["fixed"].(int)
		c.Verified(found, fixed)
	case simple.EventLookup:
		c.Note("lookedUp", nil)
	}
}
