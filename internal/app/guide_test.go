package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/guide"
)

func TestGuideReachesTheTurn(t *testing.T) {
	e := &chatExecEnv{guide: guide.Block(guide.About("How do I schedule an automation?"))}
	got := e.TurnInstructions(context.Background(), "How do I schedule an automation?")
	if !strings.Contains(got, "From Toskar's user guide") || !strings.Contains(got, "[Schedule an automation]") {
		t.Fatalf("instructions = %q", got)
	}
	if strings.Contains((&chatExecEnv{}).TurnInstructions(context.Background(), "Write a poem"), "user guide") {
		t.Fatal("the guide was added to an unrelated turn")
	}
}
