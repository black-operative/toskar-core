package events

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Event is a structured Heimdall event.
type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	TaskID    string         `json:"task_id,omitempty"`
	NodeID    string         `json:"node_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	// Contract is the client contract the event is written in (§68).
	Contract string `json:"contract"`
}

// Common event type constants.
const (
	TaskCreated   = "task.created"
	TaskStarted   = "task.started"
	TaskCompleted = "task.completed"
	TaskFailed    = "task.failed"

	AutomationStarted   = "automation.started"
	AutomationCompleted = "automation.completed"
	AutomationFailed    = "automation.failed"

	AgentStarted   = "agent.started"
	AgentMessage   = "agent.message"
	AgentCompleted = "agent.completed"

	ModelDownloadStarted   = "model.download.started"
	ModelDownloadProgress  = "model.download.progress"
	ModelDownloadCompleted = "model.download.completed"
	ModelDownloadFailed    = "model.download.failed"
	ModelLoadStarted       = "model.load.started"
	ModelLoadCompleted     = "model.load.completed"
	// ModelUnloaded reports a model the idle sweeper stopped.
	ModelUnloaded         = "model.unloaded"
	ModelHealthDegraded   = "model.health.degraded"
	ModelHealthFailed     = "model.health.failed"
	ModelCleanupStarted   = "model.cleanup.started"
	ModelCleanupCompleted = "model.cleanup.completed"
	ModelCleanupFailed    = "model.cleanup.failed"

	ToolRequested = "tool.requested"
	ToolStarted   = "tool.started"
	ToolCompleted = "tool.completed"
	ToolFailed    = "tool.failed"
	ToolParsed    = "tool.parsed"

	NodeDiscovered = "node.discovered"
	NodePaired     = "node.paired"
	NodeOnline     = "node.online"
	NodeOffline    = "node.offline"
	// One-line join (#40): tokens made and revoked, computers that joined
	// or were refused, and computers that left.
	JoinTokenCreated = "join_token.created"
	JoinTokenRevoked = "join_token.revoked"
	NodeJoined       = "node.join.accepted"
	NodeJoinRefused  = "node.join.rejected"
	NodeLeft         = "node.left"

	SchedulerPlacement = "scheduler.placement"

	OrchestrationRole  = "orchestration.role"
	OrchestrationFinal = "orchestration.final"

	ChatToken    = "chat.token"
	ChatComplete = "chat.complete"
	// ChatModelRouted reports the model chosen for a turn, by Auto or after a
	// failure, with the reason in plain language.
	ChatModelRouted = "chat.model_routed"
	// ChatStopped reports a run the user stopped, and whether a partial
	// answer was kept.
	ChatStopped = "chat.stopped"
	ChatError   = "chat.error"
)

// Bus is an in-process pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
	buffer      int
}

// NewBus creates an event bus with per-subscriber buffer size.
func NewBus(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 64
	}
	return &Bus{
		subscribers: make(map[string]chan Event),
		buffer:      buffer,
	}
}

// Subscribe registers a subscriber and returns its ID and receive channel.
func (b *Bus) Subscribe() (string, <-chan Event) {
	id := uuid.NewString()
	ch := make(chan Event, b.buffer)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()
	return id, ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Bus) Unsubscribe(id string) {
	b.mu.Lock()
	if ch, ok := b.subscribers[id]; ok {
		delete(b.subscribers, id)
		close(ch)
	}
	b.mu.Unlock()
}

// Publish sends an event to all subscribers. Slow subscribers may drop events.
func (b *Bus) Publish(evt Event) {
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	if evt.Contract == "" {
		evt.Contract = contracts.ContractVersion
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow to avoid blocking the bus.
		}
	}
}

// New creates an Event with the given type and optional payload.
func New(eventType string, payload map[string]any) Event {
	return Event{
		ID:        uuid.NewString(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}
