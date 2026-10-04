package profiles

import (
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// UpgradeGeneralTools moves an untouched built-in General Assistant onto the
// current defaults. A profile whose policies differ from every stock snapshot
// keeps those choices, and only gains tools that were not listed yet.
func UpgradeGeneralTools(existing []contracts.ToolPolicy) []contracts.ToolPolicy {
	if untouchedGeneral(existing) {
		return generalToolPolicies()
	}
	return tools.MergeMissingTools(existing, generalToolPolicies())
}

func untouchedGeneral(existing []contracts.ToolPolicy) bool {
	if len(existing) == 0 {
		return true
	}
	for _, snapshot := range generalStockSnapshots() {
		if matchesStock(existing, snapshot) {
			return true
		}
	}
	return false
}

func samePolicies(a, b []contracts.ToolPolicy) bool {
	if len(a) != len(b) {
		return false
	}
	want := map[string]string{}
	for _, tool := range b {
		want[tool.ToolID] = tool.Policy
	}
	for _, tool := range a {
		if want[tool.ToolID] != tool.Policy {
			return false
		}
	}
	return true
}

// upgradeIfUntouched replaces a built-in profile that still matches an older stock
// snapshot. Any policy the user changed is left in place, and missing tools are added.
func upgradeIfUntouched(existing, desired []contracts.ToolPolicy, snapshots ...[]contracts.ToolPolicy) []contracts.ToolPolicy {
	if len(existing) == 0 || untouchedAgainst(existing, snapshots) {
		return desired
	}
	return tools.MergeMissingTools(existing, desired)
}

func untouchedAgainst(existing []contracts.ToolPolicy, snapshots [][]contracts.ToolPolicy) bool {
	for _, snapshot := range snapshots {
		if matchesStock(existing, snapshot) {
			return true
		}
	}
	return false
}

func matchesStock(existing, snapshot []contracts.ToolPolicy) bool {
	want := map[string]string{}
	for _, tool := range snapshot {
		want[tool.ToolID] = tool.Policy
	}
	for _, tool := range existing {
		policy, ok := want[tool.ToolID]
		if !ok || policy != tool.Policy {
			return false
		}
	}
	return true
}

func generalStockSnapshots() [][]contracts.ToolPolicy {
	shipped := []contracts.ToolPolicy{
		{ToolID: "filesystem.read", Policy: "allow-for-session"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "allow"},
		{ToolID: "git.add", Policy: "ask"},
		{ToolID: "git.commit", Policy: "ask"},
	}
	previous := []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "deny"},
		{ToolID: "git.status", Policy: "deny"},
		{ToolID: "git.diff", Policy: "deny"},
		{ToolID: "git.add", Policy: "deny"},
		{ToolID: "git.commit", Policy: "deny"},
		{ToolID: "git.push", Policy: "deny"},
	}
	withApproval := []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "allow"},
		{ToolID: "git.log", Policy: "allow"},
		{ToolID: "git.show", Policy: "allow"},
		{ToolID: "git.add", Policy: "ask"},
		{ToolID: "git.commit", Policy: "ask"},
		{ToolID: "git.push", Policy: "ask"},
	}
	return [][]contracts.ToolPolicy{
		shipped,
		previous,
		tools.MergeMissingTools(shipped, previous),
		withApproval,
		generalToolPolicies(),
	}
}

func programmingStockSnapshots() [][]contracts.ToolPolicy {
	return generalStockSnapshots()
}

func researchStockSnapshots() [][]contracts.ToolPolicy {
	return [][]contracts.ToolPolicy{{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "deny"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "deny"},
		{ToolID: "git.add", Policy: "deny"},
		{ToolID: "git.commit", Policy: "deny"},
		{ToolID: "git.push", Policy: "deny"},
	}, researchToolPolicies()}
}
