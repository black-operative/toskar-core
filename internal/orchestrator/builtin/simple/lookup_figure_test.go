package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A question asking for a figure reads the best page even at Fast effort
// (no pages), because snippets rarely carry the price; others don't.
func TestLookUpReadsAPageForAFigure(t *testing.T) {
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
	}}
	for _, c := range []struct {
		prompt string
		read   bool
	}{
		{"How much does it cost?", true},
		{"What's the price of the Zize Forte?", true},
		{"What are their hours?", true},
		{"Can you recommend a good bike?", false},
	} {
		env := &searchPageEnv{}
		found, ok := lookUp(context.Background(), env, profile, "query", c.prompt, 0)
		if !ok {
			t.Fatalf("%q: no lookup", c.prompt)
		}
		if read := env.openedURL != ""; read != c.read {
			t.Errorf("%q: read a page = %v, want %v", c.prompt, read, c.read)
		}
		if c.read && !strings.Contains(found, "Page read:") {
			t.Errorf("%q: the page isn't in the material:\n%s", c.prompt, found)
		}
	}
}
