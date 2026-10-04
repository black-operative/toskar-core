package huginn

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestBudgetWithProfileControls(t *testing.T) {
	base := BudgetFor(EffortBalanced, Research)
	if got := base.With(contracts.OrchestrationPolicy{}); got != base {
		t.Fatalf("empty controls changed the budget: %+v", got)
	}
	got := base.With(contracts.OrchestrationPolicy{Planning: "off", Verification: "thorough", MaxToolCalls: 4, MaxWorkers: 3, Parallel: "off"})
	if got.Plan || got.Corrections != 2 || !got.Verify || got.MaxToolCalls != 4 || got.MaxWorkers != 3 || !got.Sequential {
		t.Fatalf("controls = %+v", got)
	}
	if off := base.With(contracts.OrchestrationPolicy{Verification: "off"}); off.Verify || off.Corrections != 0 {
		t.Fatalf("verification off = %+v", off)
	}
	if check := base.With(contracts.OrchestrationPolicy{Verification: "check"}); !check.Verify || check.Corrections != 0 {
		t.Fatalf("check only = %+v", check)
	}
	if on := BudgetFor(EffortFast, Chat).With(contracts.OrchestrationPolicy{Planning: "on"}); !on.Plan {
		t.Fatal("planning on did not plan at fast effort")
	}
}
