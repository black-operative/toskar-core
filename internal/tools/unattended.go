package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrNeedsApproval means a scheduled run reached a tool nobody approved for
// it. The run skips the action and reports it; it never widens its own
// permissions because no one answered (spec §59).
var ErrNeedsApproval = contracts.NewError("NEEDS_APPROVAL", nil, errors.New("needs your approval for this automation"))

// UnattendedPolicy is the policy a scheduled run may use for one tool.
//
// The profile stays the ceiling: Deny is never lifted. A tool approved when
// the automation was created (listed in granted) may run unattended even
// when the profile says Ask, because the person answered that Ask then; that
// includes tools that change things. An automation with no approvals keeps
// to read-only tools the profile allows. Anything else returns
// ErrNeedsApproval.
func UnattendedPolicy(profile contracts.AIProfile, granted []string, toolID string) (string, error) {
	toolID = strings.TrimSpace(toolID)
	if toolID == "" {
		return "", fmt.Errorf("tool id is required")
	}
	def, ok := Lookup(toolID)
	if !ok {
		return "", fmt.Errorf("tool %q is not allowed for unattended execution", toolID)
	}
	policy := strings.ToLower(strings.TrimSpace(PolicyForProfile(profile, toolID)))
	if policy == PolicyDeny || policy == "" {
		return "", fmt.Errorf("tool %q is not allowed by the profile", toolID)
	}
	if len(granted) == 0 {
		if policy == PolicyAllow && def.Risk == RiskRead {
			return PolicyAllow, nil
		}
		return "", fmt.Errorf("tool %q: %w", toolID, ErrNeedsApproval)
	}
	if !grantIncludes(granted, toolID) {
		return "", fmt.Errorf("tool %q: %w", toolID, ErrNeedsApproval)
	}
	return PolicyAllow, nil
}

// ForUnattended returns a profile whose tool list matches UnattendedPolicy.
// The orchestrator advertises only those tools. The stored profile is left unchanged.
func ForUnattended(profile contracts.AIProfile, granted []string, disabled map[string]struct{}) contracts.AIProfile {
	out := profile
	out.Tools = make([]contracts.ToolPolicy, len(profile.Tools))
	for i, tool := range profile.Tools {
		policy := PolicyDeny
		if _, off := disabled[tool.ToolID]; !off {
			if allowed, err := UnattendedPolicy(profile, granted, tool.ToolID); err == nil {
				policy = allowed
			}
		}
		out.Tools[i] = contracts.ToolPolicy{ToolID: tool.ToolID, Policy: policy}
	}
	return out
}

func grantIncludes(granted []string, toolID string) bool {
	for _, id := range granted {
		if strings.TrimSpace(id) == toolID {
			return true
		}
	}
	return false
}
