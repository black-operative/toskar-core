package runtimes

import "github.com/yeixio/toskar-core/pkg/pluginapi"

// Runtime re-exports the plugin runtime interface.
type Runtime = pluginapi.Runtime

// Generator streams chat completions from a running model.
type Generator = pluginapi.Generator

// Re-export common types for convenience within internal/runtimes.
type (
	RuntimeDetection    = pluginapi.RuntimeDetection
	InstallOptions      = pluginapi.InstallOptions
	RuntimeCapabilities = pluginapi.RuntimeCapabilities
	ModelStartConfig    = pluginapi.ModelStartConfig
	RunningModel        = pluginapi.RunningModel
	ChatRequest         = pluginapi.ChatRequest
	ChatMessage         = pluginapi.ChatMessage
	ChatChunk           = pluginapi.ChatChunk
)
