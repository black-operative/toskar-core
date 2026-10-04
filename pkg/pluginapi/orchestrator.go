package pluginapi

import (
	"context"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// OrchestratorCapabilities describes orchestrator features.
type OrchestratorCapabilities struct {
	SupportsTools bool     `json:"supports_tools"`
	SupportsTeam  bool     `json:"supports_team"`
	Roles         []string `json:"roles"`
}

// ExecutionEnvironment provides runtime services to an orchestrator.
type ExecutionEnvironment interface {
	Generate(ctx context.Context, role string, messages []ChatMessage) (<-chan ChatChunk, error)
	ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error)
	Emit(eventType string, payload map[string]any)
	NodeForRole(role string) (nodeID string, err error)
}

// OrchestrationEvent is a structured progress event from an orchestrator.
type OrchestrationEvent struct {
	Type    string             `json:"type"`
	Role    string             `json:"role,omitempty"`
	NodeID  string             `json:"node_id,omitempty"`
	ModelID string             `json:"model_id,omitempty"`
	Content string             `json:"content,omitempty"`
	Payload map[string]any     `json:"payload,omitempty"`
	Metrics *GenerationMetrics `json:"metrics,omitempty"`
	Done    bool               `json:"done,omitempty"`
	Error   string             `json:"error,omitempty"`
}

// Orchestrator runs multi-step AI workflows.
type Orchestrator interface {
	ID() string
	DisplayName() string
	Capabilities() OrchestratorCapabilities

	ValidateProfile(ctx context.Context, profile contracts.AIProfile) error

	Run(
		ctx context.Context,
		task contracts.Task,
		profile contracts.AIProfile,
		env ExecutionEnvironment,
	) (<-chan OrchestrationEvent, error)
}
