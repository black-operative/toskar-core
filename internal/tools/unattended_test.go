package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestUnattendedPolicy(t *testing.T) {
	profile := contracts.AIProfile{
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: PolicyAllow},
			{ToolID: "filesystem.read", Policy: PolicyAllow},
			{ToolID: "filesystem.write", Policy: PolicyAllow},
			{ToolID: "terminal", Policy: PolicyAsk},
			{ToolID: "git.push", Policy: PolicyAllowForSession},
			{ToolID: "git.commit", Policy: PolicyDeny},
		},
	}

	needsApproval := func(granted []string, id string) bool {
		_, err := UnattendedPolicy(profile, granted, id)
		return errors.Is(err, ErrNeedsApproval)
	}

	// No approvals: read-only tools the profile allows, as before.
	policy, err := UnattendedPolicy(profile, nil, "internet.search")
	if err != nil || policy != PolicyAllow {
		t.Fatalf("search policy=%q err=%v", policy, err)
	}
	if !needsApproval(nil, "filesystem.write") || !needsApproval(nil, "terminal") || !needsApproval(nil, "git.push") {
		t.Fatal("without approval, a write tool or an Ask is skipped and reported")
	}

	// Approved when the automation was created: it may run unattended, even
	// a write tool or one the profile asks about (§59).
	for _, id := range []string{"filesystem.write", "terminal", "git.push", "filesystem.read"} {
		policy, err := UnattendedPolicy(profile, []string{id}, id)
		if err != nil || policy != PolicyAllow {
			t.Fatalf("approved %s policy=%q err=%v", id, policy, err)
		}
	}
	// With approvals, only the approved tools run.
	if !needsApproval([]string{"filesystem.write"}, "internet.search") {
		t.Fatal("a tool outside the approvals is skipped and reported")
	}
	// Deny is never lifted, and is not a question to report.
	if _, err := UnattendedPolicy(profile, []string{"git.commit"}, "git.commit"); err == nil || errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("a denied tool stays denied: %v", err)
	}
	if _, err := UnattendedPolicy(profile, nil, "not.a.tool"); err == nil {
		t.Fatal("expected an unknown tool to be rejected")
	}
}

func TestForUnattendedOffersOnlyWhatMayRun(t *testing.T) {
	profile := contracts.AIProfile{
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: PolicyAllow},
			{ToolID: "filesystem.write", Policy: PolicyAllow},
			{ToolID: "terminal", Policy: PolicyAsk},
		},
	}
	narrowed := ForUnattended(profile, nil, map[string]struct{}{"internet.open": {}})
	prompt := PromptFor(narrowed)
	if !strings.Contains(prompt, "internet.search") {
		t.Fatalf("prompt = %s", prompt)
	}
	if strings.Contains(prompt, "filesystem.write") || strings.Contains(prompt, "terminal") {
		t.Fatalf("prompt advertised a tool the run cannot use: %s", prompt)
	}
	if profile.Tools[1].Policy != PolicyAllow {
		t.Fatal("narrowing changed the stored profile")
	}
	// Approved at creation, the terminal is offered even though the profile asks.
	if prompt := PromptFor(ForUnattended(profile, []string{"terminal"}, nil)); !strings.Contains(prompt, "terminal") || strings.Contains(prompt, "internet.search") {
		t.Fatalf("approved prompt = %s", prompt)
	}
}
