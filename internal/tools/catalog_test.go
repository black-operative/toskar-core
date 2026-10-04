package tools

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestPromptHidesDisabledInternet(t *testing.T) {
	on := PromptFor(testProfile("allow"))
	if !strings.Contains(on, "internet.search") || !strings.Contains(on, "You can search and open web pages") {
		t.Fatalf("expected internet in prompt, got %s", on)
	}
	off := PromptFor(testProfile("deny"))
	if strings.Contains(off, "internet.search") || strings.Contains(off, "You can search and open web pages") {
		t.Fatalf("internet leaked into prompt: %s", off)
	}
}

func TestEnabledMatchesPolicy(t *testing.T) {
	for _, def := range Enabled(testProfile("deny"), nil) {
		if def.ID == "internet.search" {
			t.Fatal("search exposed while denied")
		}
	}
	found := false
	for _, def := range Enabled(testProfile("allow"), nil) {
		if def.ID == "internet.search" {
			found = true
		}
	}
	if !found {
		t.Fatal("search missing while allowed")
	}
	found = false
	for _, def := range Enabled(testProfile("allow"), map[string]struct{}{"internet.search": {}}) {
		if def.ID == "internet.search" {
			found = true
		}
	}
	if found {
		t.Fatal("globally disabled tool was exposed")
	}
}

func TestMergeMissingKeepsExistingPolicy(t *testing.T) {
	got := MergeMissingTools(
		[]contracts.ToolPolicy{{ToolID: "terminal", Policy: "ask"}},
		[]contracts.ToolPolicy{{ToolID: "terminal", Policy: "deny"}, {ToolID: "internet.search", Policy: "allow"}},
	)
	if len(got) != 2 || got[0].Policy != "ask" || got[1].ToolID != "internet.search" {
		t.Fatalf("merged %#v", got)
	}
}

func testProfile(internet string) contracts.AIProfile {
	return contracts.AIProfile{
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: internet},
			{ToolID: "internet.open", Policy: internet},
			{ToolID: "filesystem.read", Policy: "allow"},
		},
	}
}
