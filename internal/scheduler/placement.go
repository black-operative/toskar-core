package scheduler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// PlacementDecision is the chosen node with diagnostic reason.
type PlacementDecision struct {
	NodeID string
	Score  int
	Reason string
}

// Place selects the best node for a role.
func Place(input ScoreInput) (PlacementDecision, error) {
	if err := pinnedNodeError(input); err != nil {
		return PlacementDecision{}, err
	}
	scored := Score(input)
	if len(scored) == 0 {
		return PlacementDecision{}, fmt.Errorf("no nodes available for role %q", input.Role)
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	best := scored[0]
	if best.Score < -100 {
		return PlacementDecision{}, fmt.Errorf("no compatible node for role %q", input.Role)
	}
	reason := strings.Join(best.Reasons, "; ")
	if reason == "" {
		reason = "highest score"
	}
	return PlacementDecision{
		NodeID: best.NodeID,
		Score:  best.Score,
		Reason: reason,
	}, nil
}

// pinnedNodeError returns a clear user-facing error when a role is pinned to a
// missing or offline node (M7: fail understandably; do not silently re-place).
func pinnedNodeError(input ScoreInput) error {
	var pinID string
	for _, r := range input.Profile.Roles {
		if r.Role == baseRole(input.Role) && strings.TrimSpace(r.NodeID) != "" {
			pinID = r.NodeID
			break
		}
	}
	if pinID == "" {
		return nil
	}

	var pinned *NodeCandidate
	for i := range input.Nodes {
		if input.Nodes[i].Node.ID == pinID {
			pinned = &input.Nodes[i]
			break
		}
	}
	label := pinID
	if pinned != nil && pinned.Node.Name != "" {
		label = pinned.Node.Name
	}

	if pinned == nil {
		return fmt.Errorf( //nolint:staticcheck // ST1005: sentence shown in the UI
			"%s is not available (required for role %s). Pair that computer again, or set the role to Automatic placement.",
			label, input.Role,
		)
	}
	st := pinned.Node.Status
	if st != contracts.NodeStatusOnline && st != "" {
		return fmt.Errorf( //nolint:staticcheck // ST1005: sentence shown in the UI
			"%s is offline (required for role %s). Turn that computer on and open Toskar, or set the role to Automatic placement.",
			label, input.Role,
		)
	}
	return nil
}
