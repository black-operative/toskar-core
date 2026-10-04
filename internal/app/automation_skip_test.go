package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestUnapprovedToolsAreSkippedAndReported(t *testing.T) {
	a, _ := memoryApp(t)
	a.Notifications = gjallarhorn.NewHub(a.DB.SQL, a.Bus)
	a.Tools = tools.NewRegistry(t.TempDir(), a.Bus)
	ctx := context.Background()
	profile := profiles.Profile{Tools: []contracts.ToolPolicy{
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "filesystem.write", Policy: "allow"},
	}}
	env := &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profile, trace: &turnTrace{}}, granted: []string{"filesystem.read"}}

	_, err := env.ExecuteTool(ctx, "terminal", map[string]any{"command": "rm -rf /tmp/x"})
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("an unapproved tool must be skipped, got %v", err)
	}
	_, _ = env.ExecuteTool(ctx, "terminal", map[string]any{"command": "ls"})
	if got := env.skippedTools(); len(got) != 1 || got[0] != "terminal" {
		t.Fatalf("skipped = %v", got)
	}

	automationExecutor{app: a}.reportSkipped(ctx, automations.Automation{ID: "a1", Name: "Nightly cleanup"}, env.skippedTools())
	list, _, _ := a.Notifications.List(ctx, false, 0)
	if len(list) != 1 || list[0].Category != gjallarhorn.CategoryApproval || list[0].Title != "Nightly cleanup skipped Terminal" || list[0].Link != "/automations?id=a1" {
		t.Fatalf("notification = %+v", list)
	}
	if !errors.Is(func() error { _, e := tools.UnattendedPolicy(profile, nil, "terminal"); return e }(), tools.ErrNeedsApproval) {
		t.Fatal("policy")
	}
}
