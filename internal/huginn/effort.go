package huginn

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Effort is how much work a request should get (spec §15). Auto lets Huginn
// decide from the kind of request.
type Effort string

const (
	EffortAuto     Effort = "auto"
	EffortFast     Effort = "fast"
	EffortBalanced Effort = "balanced"
	EffortThorough Effort = "thorough"
)

// ParseEffort reads an effort from a request, defaulting to Auto.
func ParseEffort(s string) Effort {
	switch Effort(strings.ToLower(strings.TrimSpace(s))) {
	case EffortFast:
		return EffortFast
	case EffortBalanced:
		return EffortBalanced
	case EffortThorough:
		return EffortThorough
	default:
		return EffortAuto
	}
}

// Describe is the effort's label in the App language app, as the effort
// control shows it.
func (e Effort) Describe(app string) string {
	return locale.T(app, "chat:effort."+string(ParseEffort(string(e)))+".label", nil)
}

// Budget is what a turn may spend. Efforts map to budgets, never to call
// counts the user sees.
type Budget struct {
	// Effort is the level the budget is for, after Auto is resolved.
	Effort Effort
	// Plan lets a request with several parts be worked through in parts.
	Plan bool
	// Pages is how many web pages a look-up reads after searching; 0 uses
	// the search results alone.
	Pages int
	// Corrections is how many times an answer whose figures do not check
	// out is sent back to the model.
	Corrections int
	// MaxToolCalls caps the tool calls in one turn.
	MaxToolCalls int
	// Verify checks an answer's figures before it is shown.
	Verify bool
	// MaxWorkers caps a plan's parts; 0 leaves the plan as made.
	MaxWorkers int
	// Sequential works through a plan's parts one at a time, even ones
	// that could be looked up side by side.
	Sequential bool
}

// With applies a profile's orchestration controls (§40) on top of the
// budget its effort gives. Empty controls change nothing.
func (b Budget) With(o contracts.OrchestrationPolicy) Budget {
	switch o.Planning {
	case "on":
		b.Plan = true
	case "off":
		b.Plan = false
	}
	switch o.Verification {
	case "off":
		b.Verify, b.Corrections = false, 0
	case "check":
		b.Corrections = 0
	case "correct":
		b.Corrections = 1
	case "thorough":
		b.Corrections = 2
	}
	if o.MaxToolCalls > 0 {
		b.MaxToolCalls = o.MaxToolCalls
	}
	b.MaxWorkers = o.MaxWorkers
	b.Sequential = o.Parallel == "off"
	return b
}

var budgets = map[Effort]Budget{
	EffortFast:     {Effort: EffortFast, Plan: false, Pages: 0, Corrections: 0, MaxToolCalls: 3, Verify: true},
	EffortBalanced: {Effort: EffortBalanced, Plan: true, Pages: 1, Corrections: 1, MaxToolCalls: 10, Verify: true},
	EffortThorough: {Effort: EffortThorough, Plan: true, Pages: 2, Corrections: 2, MaxToolCalls: 16, Verify: true},
}

// BudgetFor resolves an effort for a kind of request. Auto keeps a quick
// question fast, gives a request that needs a detailed answer the thorough
// budget, and balances everything else.
func BudgetFor(e Effort, k Kind) Budget {
	if e == EffortAuto || e == "" {
		switch k {
		case Chat:
			e = EffortFast
		case Research:
			e = EffortThorough
		default:
			e = EffortBalanced
		}
	}
	if b, ok := budgets[e]; ok {
		return b
	}
	return budgets[EffortBalanced]
}

type effortKey struct{}

// WithEffort carries the effort the user chose for a message.
func WithEffort(ctx context.Context, e Effort) context.Context {
	return context.WithValue(ctx, effortKey{}, e)
}

// EffortFrom returns the effort chosen for a message, or Auto.
func EffortFrom(ctx context.Context) Effort {
	if e, ok := ctx.Value(effortKey{}).(Effort); ok {
		return e
	}
	return EffortAuto
}
