package profiles

import (
	"fmt"
	"slices"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Profile is an alias for the public contract.
type Profile = contracts.AIProfile

// Validate checks basic profile invariants.
func Validate(p Profile) error {
	if p.Name == "" {
		return ErrInvalidProfile("name is required")
	}
	if p.OrchestratorID == "" {
		return ErrInvalidProfile("orchestrator_id is required")
	}
	for _, r := range p.Roles {
		if r.Role == "" {
			return ErrInvalidProfile("role name is required")
		}
		if r.Required && r.ModelID == "" {
			return ErrInvalidProfile("required role " + r.Role + " missing model_id")
		}
	}
	if err := ValidateNodePolicy(p.NodePolicy); err != nil {
		return err
	}
	return ValidateOrchestration(p.Orchestration)
}

// ValidateNodePolicy checks a profile's placement rules.
func ValidateNodePolicy(n contracts.NodePolicy) error {
	switch n.Mode {
	case "", "automatic", "prefer_local", "manual":
	default:
		return ErrInvalidProfile("node_policy.mode must be automatic, prefer_local, or manual")
	}
	if n.Remote != "" && n.Remote != "off" {
		return ErrInvalidProfile("node_policy.remote must be off or empty")
	}
	for _, id := range n.PreferredNodes {
		if slices.Contains(n.DeniedNodes, id) {
			return ErrInvalidProfile("a computer cannot be both preferred and denied")
		}
	}
	return nil
}

// ValidateOrchestration checks a profile's advanced controls (§40).
func ValidateOrchestration(o contracts.OrchestrationPolicy) error {
	choice := func(field, v string, allowed ...string) error {
		if v != "" && !slices.Contains(allowed, v) {
			return ErrInvalidProfile(fmt.Sprintf("orchestration.%s must be one of %v", field, allowed))
		}
		return nil
	}
	between := func(field string, v, lo, hi int) error {
		if v != 0 && (v < lo || v > hi) {
			return ErrInvalidProfile(fmt.Sprintf("orchestration.%s must be from %d to %d", field, lo, hi))
		}
		return nil
	}
	for _, err := range []error{
		choice("effort", o.Effort, "fast", "balanced", "thorough"),
		choice("strategy", o.Strategy, StrategySingle, StrategyPlanned, StrategyTeam),
		choice("planning", o.Planning, "on", "off", "always"),
		choice("parallel", o.Parallel, "on", "off"),
		choice("verification", o.Verification, "off", "check", "correct", "thorough"),
		choice("memory", o.Memory, "off"),
		choice("fallback", o.Fallback, "off"),
		between("max_workers", o.MaxWorkers, 2, 8),
		between("max_tool_calls", o.MaxToolCalls, 1, 50),
		between("retries", o.Retries, 1, 3),
		between("timeout_seconds", o.TimeoutSeconds, 10, 3600),
	} {
		if err != nil {
			return err
		}
	}
	if len(o.FallbackModels) > 8 {
		return ErrInvalidProfile("orchestration.fallback_models may list up to 8 models")
	}
	for _, id := range o.FallbackModels {
		if id == "" {
			return ErrInvalidProfile("orchestration.fallback_models cannot contain an empty model id")
		}
	}
	if o.ContextShare != 0 && (o.ContextShare < 0.1 || o.ContextShare > 0.9) {
		return ErrInvalidProfile("orchestration.context_share must be from 0.1 to 0.9")
	}
	return nil
}

// ErrInvalidProfile indicates profile validation failure.
type ErrInvalidProfile string

func (e ErrInvalidProfile) Error() string { return string(e) }
