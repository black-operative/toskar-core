package profiles

import (
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// PresetIDs for built-in profile templates.
const (
	PresetGeneral     = "general-assistant"
	PresetProgramming = "programming"
	PresetResearch    = "research"
	PresetCustom      = "custom"
)

// IsPreset reports whether id is a built-in profile template.
func IsPreset(id string) bool {
	switch id {
	case PresetGeneral, PresetProgramming, PresetResearch, PresetCustom:
		return true
	default:
		return false
	}
}

// BuiltInPresets returns default profile templates.
func BuiltInPresets() []Profile {
	return []Profile{
		{
			ID:             PresetGeneral,
			Name:           "General Assistant",
			Purpose:        "general",
			OrchestratorID: "simple",
			NodePolicy:     contracts.NodePolicy{Mode: "automatic"},
			Roles: []contracts.ModelRole{
				{Role: "assistant", Required: false},
			},
			Tools: defaultToolPolicies(),
		},
		{
			ID:             PresetProgramming,
			Name:           "Programming",
			Purpose:        "coding",
			OrchestratorID: "simple",
			NodePolicy:     contracts.NodePolicy{Mode: "automatic"},
			Roles: []contracts.ModelRole{
				{Role: RolePlanner, Required: false},
				{Role: RoleWorker, Required: false},
				{Role: RoleReviewer, Required: false},
			},
			Orchestration: contracts.OrchestrationPolicy{Strategy: StrategyTeam},
			Tools:         codingToolPolicies(),
		},
		{
			ID:             PresetResearch,
			Name:           "Research",
			Purpose:        "research",
			OrchestratorID: "simple",
			NodePolicy:     contracts.NodePolicy{Mode: "automatic"},
			Roles: []contracts.ModelRole{
				{Role: RolePrimary, Required: false},
			},
			Tools: researchToolPolicies(),
		},
		{
			ID:             PresetCustom,
			Name:           "Custom",
			Purpose:        "custom",
			OrchestratorID: "simple",
			NodePolicy:     contracts.NodePolicy{Mode: "manual"},
			Roles: []contracts.ModelRole{
				{Role: "assistant", Required: false},
			},
			Tools: offlineToolPolicies(),
		},
	}
}

func defaultToolPolicies() []contracts.ToolPolicy {
	return generalToolPolicies()
}

func generalToolPolicies() []contracts.ToolPolicy {
	return []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "browser.open", Policy: "allow"},
		{ToolID: "browser.extract", Policy: "allow"},
		{ToolID: "browser.screenshot", Policy: "allow"},
		{ToolID: "browser.close", Policy: "allow"},
		{ToolID: "browser.click", Policy: "ask"},
		{ToolID: "browser.type", Policy: "ask"},
		{ToolID: "browser.download", Policy: "ask"},
		{ToolID: "places.search", Policy: "allow"},
		{ToolID: "places.details", Policy: "allow"},
		{ToolID: "maps.route", Policy: "allow"},
		{ToolID: "maps.distance", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "allow"},
		{ToolID: "files.create", Policy: "allow"},
		{ToolID: "spreadsheet.analyze", Policy: "allow"},
		{ToolID: "code.execute", Policy: "ask"},
		{ToolID: "speech.transcribe", Policy: "allow"},
		{ToolID: "speech.synthesize", Policy: "allow"},
		{ToolID: "image.generate", Policy: "allow"},
		{ToolID: "video.generate", Policy: "allow"},
		{ToolID: "image.edit", Policy: "allow"},
		{ToolID: "terminal", Policy: "allow"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "allow"},
		{ToolID: "git.log", Policy: "allow"},
		{ToolID: "git.show", Policy: "allow"},
		{ToolID: "git.add", Policy: "allow"},
		{ToolID: "git.commit", Policy: "allow"},
		{ToolID: "git.push", Policy: "allow"},
	}
}

func codingToolPolicies() []contracts.ToolPolicy {
	return []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "browser.open", Policy: "allow"},
		{ToolID: "browser.extract", Policy: "allow"},
		{ToolID: "browser.screenshot", Policy: "allow"},
		{ToolID: "browser.close", Policy: "allow"},
		{ToolID: "browser.click", Policy: "ask"},
		{ToolID: "browser.type", Policy: "ask"},
		{ToolID: "browser.download", Policy: "ask"},
		{ToolID: "places.search", Policy: "allow"},
		{ToolID: "places.details", Policy: "allow"},
		{ToolID: "maps.route", Policy: "allow"},
		{ToolID: "maps.distance", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "allow"},
		{ToolID: "files.create", Policy: "allow"},
		{ToolID: "spreadsheet.analyze", Policy: "allow"},
		{ToolID: "code.execute", Policy: "ask"},
		{ToolID: "speech.transcribe", Policy: "allow"},
		{ToolID: "speech.synthesize", Policy: "allow"},
		{ToolID: "image.generate", Policy: "allow"},
		{ToolID: "video.generate", Policy: "allow"},
		{ToolID: "image.edit", Policy: "allow"},
		{ToolID: "terminal", Policy: "allow"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "allow"},
		{ToolID: "git.log", Policy: "allow"},
		{ToolID: "git.show", Policy: "allow"},
		{ToolID: "git.add", Policy: "allow"},
		{ToolID: "git.commit", Policy: "allow"},
		{ToolID: "git.push", Policy: "allow"},
	}
}

func researchToolPolicies() []contracts.ToolPolicy {
	return []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "browser.open", Policy: "allow"},
		{ToolID: "browser.extract", Policy: "allow"},
		{ToolID: "browser.screenshot", Policy: "allow"},
		{ToolID: "browser.close", Policy: "allow"},
		{ToolID: "browser.click", Policy: "ask"},
		{ToolID: "browser.type", Policy: "ask"},
		{ToolID: "browser.download", Policy: "ask"},
		{ToolID: "places.search", Policy: "allow"},
		{ToolID: "places.details", Policy: "allow"},
		{ToolID: "maps.route", Policy: "allow"},
		{ToolID: "maps.distance", Policy: "allow"},
		{ToolID: "internet.open", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "allow"},
		{ToolID: "files.create", Policy: "allow"},
		{ToolID: "spreadsheet.analyze", Policy: "allow"},
		{ToolID: "code.execute", Policy: "ask"},
		{ToolID: "speech.transcribe", Policy: "allow"},
		{ToolID: "speech.synthesize", Policy: "allow"},
		{ToolID: "image.generate", Policy: "allow"},
		{ToolID: "video.generate", Policy: "allow"},
		{ToolID: "image.edit", Policy: "allow"},
		{ToolID: "terminal", Policy: "deny"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "deny"},
		{ToolID: "git.add", Policy: "deny"},
		{ToolID: "git.commit", Policy: "deny"},
		{ToolID: "git.push", Policy: "deny"},
	}
}

func offlineToolPolicies() []contracts.ToolPolicy {
	policies := generalToolPolicies()
	for i := range policies {
		// Files stay on this computer, so they work offline.
		if strings.HasPrefix(policies[i].ToolID, "filesystem.") || policies[i].ToolID == "files.create" || policies[i].ToolID == "spreadsheet.analyze" ||
			strings.HasPrefix(policies[i].ToolID, "speech.") || strings.HasPrefix(policies[i].ToolID, "image.") || strings.HasPrefix(policies[i].ToolID, "video.") {
			policies[i].Policy = "allow"
			continue
		}
		policies[i].Policy = "deny"
	}
	return policies
}
