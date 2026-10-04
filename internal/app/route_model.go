package app

import (
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// routeToolCapableModel prefers an installed tool-calling model for live questions
// when Automatic placement is on and the current model cannot call tools.
func routeToolCapableModel(execution, message, currentID string, installed []contracts.Model) (string, string) {
	if execution != "automatic" || message == "" {
		return currentID, ""
	}
	if !toolsMessageNeedsWeb(message) {
		return currentID, ""
	}
	if toolCallRank(modelSupport(currentID, installed)) >= toolCallRank("compatible") {
		return currentID, ""
	}
	var best *contracts.Model
	for i := range installed {
		model := &installed[i]
		if !model.Installed || model.ID == currentID || huginn.Supporting(*model) {
			continue
		}
		rank := toolCallRank(toolCallSupport(model.Capabilities))
		if rank < toolCallRank("compatible") {
			continue
		}
		if best == nil || toolCallRank(toolCallSupport(best.Capabilities)) < rank || (toolCallRank(toolCallSupport(best.Capabilities)) == rank && smallerMemory(*model, *best)) {
			best = model
		}
	}
	if best == nil {
		return currentID, ""
	}
	return best.ID, "request needs current web data"
}

func modelSupport(id string, installed []contracts.Model) string {
	if id == "" {
		return "unsupported"
	}
	for _, model := range installed {
		if model.ID == id {
			return toolCallSupport(model.Capabilities)
		}
	}
	// Unknown models are left alone so a local import is not treated as incapable.
	return "compatible"
}

func toolCallSupport(caps contracts.ModelCapabilities) string {
	switch caps.ToolCallSupport {
	case "native", "compatible", "limited", "unsupported":
		return caps.ToolCallSupport
	}
	if caps.ToolCalling {
		return "compatible"
	}
	return "unsupported"
}

func toolCallRank(level string) int {
	switch level {
	case "native":
		return 3
	case "compatible":
		return 2
	case "limited":
		return 1
	default:
		return 0
	}
}

func smallerMemory(a, b contracts.Model) bool {
	if a.MemoryNeeded == 0 {
		return false
	}
	if b.MemoryNeeded == 0 {
		return true
	}
	return a.MemoryNeeded < b.MemoryNeeded
}

func toolsMessageNeedsWeb(message string) bool {
	return tools.MessageNeedsLiveWeb(message)
}
