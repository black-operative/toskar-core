package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A model that cannot call tools keeps its tools allowed, so Toskar's own
// look-ups still run, and is marked so it isn't asked to call any.
func TestWithModelToolsKeepsLookUps(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: tools.PolicyAllow}}}

	gemma := a.withModelTools(profile, "gemma-2-9b-q4")
	if !gemma.ModelCallsNoTools {
		t.Error("gemma-2-9b-q4 cannot call tools")
	}
	if tools.PolicyForProfile(gemma, "internet.search") != tools.PolicyAllow {
		t.Error("web search was taken away, so Toskar can't look anything up for it")
	}
	if qwen := a.withModelTools(gemma, "qwen2.5-7b-q4"); qwen.ModelCallsNoTools {
		t.Error("qwen2.5-7b-q4 calls tools; switching to it should clear the mark")
	}
}
