package app

import (
	"testing"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// An answer keeps its context reading, so the gauge shows it when the chat
// is opened again, even when it has nothing else to keep.
func TestWithContextUsage(t *testing.T) {
	usage := map[string]any{"prompt_tokens": 1200, "limit": 8192, "instructions": 400, "tools": 300, "conversation": 500, "memory_bytes": int64(1 << 30)}
	meta := withContextUsage(nil, usage)
	if meta == nil || meta.Context == nil || meta.Context.PromptTokens != 1200 || meta.Context.Limit != 8192 || meta.Context.MemoryBytes != 1<<30 || meta.Contract != contracts.ContractVersion {
		t.Fatalf("meta = %+v", meta)
	}
	kept := &contracts.MessageMeta{Notice: "smaller model", Contract: contracts.ContractVersion}
	if got := withContextUsage(kept, usage); got != kept || got.Notice != "smaller model" || got.Context == nil {
		t.Fatalf("existing meta = %+v", got)
	}
	// No reading, or an empty one, adds nothing.
	if withContextUsage(nil, nil) != nil || withContextUsage(nil, map[string]any{"limit": 8192}) != nil {
		t.Fatal("an empty reading made metadata")
	}
}
