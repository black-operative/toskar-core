package scheduler

import (
	"context"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Scheduler (Norn) performs deterministic placement.
type Scheduler struct {
	bus *events.Bus
}

func New(bus *events.Bus) *Scheduler {
	return &Scheduler{bus: bus}
}

// PlaceRole selects a node and emits a placement event.
func (s *Scheduler) PlaceRole(ctx context.Context, input ScoreInput) (PlacementDecision, error) {
	decision, err := Place(input)
	if err != nil {
		return decision, err
	}
	if s.bus != nil {
		s.bus.Publish(events.New(events.SchedulerPlacement, map[string]any{
			"role":     input.Role,
			"model_id": input.ModelID,
			"node_id":  decision.NodeID,
			"score":    decision.Score,
			"reason":   decision.Reason,
		}))
	}
	return decision, nil
}

// BuildCandidates helper from contract nodes.
func BuildCandidates(nodes []contracts.Node, installed map[string][]string, freeMem map[string]uint64) []NodeCandidate {
	var out []NodeCandidate
	for _, n := range nodes {
		models := map[string]struct{}{}
		for _, mid := range installed[n.ID] {
			models[mid] = struct{}{}
		}
		out = append(out, NodeCandidate{
			Node:            n,
			InstalledModels: models,
			FreeMemory:      freeMem[n.ID],
		})
	}
	return out
}
