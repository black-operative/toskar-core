package scheduler

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func threeComputers(withModel ...string) []NodeCandidate {
	has := map[string]bool{}
	for _, id := range withModel {
		has[id] = true
	}
	var out []NodeCandidate
	for _, n := range []contracts.Node{
		{ID: "here", Status: contracts.NodeStatusOnline, Paired: true, IsLocal: true},
		{ID: "studio", Status: contracts.NodeStatusOnline, Paired: true},
		{ID: "pc", Status: contracts.NodeStatusOnline, Paired: true},
	} {
		c := NodeCandidate{Node: n, InstalledModels: map[string]struct{}{}}
		if has[n.ID] {
			c.InstalledModels["m"] = struct{}{}
		}
		out = append(out, c)
	}
	return out
}

func placed(t *testing.T, in ScoreInput) string {
	t.Helper()
	d, err := Place(in)
	if err != nil {
		return ""
	}
	return d.NodeID
}

// A profile's execution rules (§20): denied computers and this computer
// only are hard limits; preferred computers win among those that can run
// the model.
func TestExecutionPolicy(t *testing.T) {
	all := threeComputers("here", "studio", "pc")
	in := ScoreInput{Role: "assistant", ModelID: "m", Nodes: all}

	in.Profile.NodePolicy = contracts.NodePolicy{Mode: "automatic", PreferredNodes: []string{"pc"}}
	if got := placed(t, in); got != "pc" {
		t.Fatalf("preferred: %q", got)
	}
	in.Profile.NodePolicy = contracts.NodePolicy{Mode: "automatic", DeniedNodes: []string{"here"}}
	for _, s := range Score(in) {
		if s.NodeID == "here" {
			t.Fatal("a denied computer was scored")
		}
	}
	in.Profile.NodePolicy = contracts.NodePolicy{Mode: "automatic", Remote: "off", PreferredNodes: []string{"studio"}}
	in.Nodes = threeComputers("here", "studio")
	if got := placed(t, in); got != "here" {
		t.Fatalf("this computer only placed on %q", got)
	}
	// The model is not here, so there is nowhere to run it.
	in.Nodes = threeComputers("studio")
	if got := placed(t, in); got != "" {
		t.Fatalf("this computer only placed on %q without the model", got)
	}
}

// Retrying on another computer never picks the one that failed, and only
// one that has the model.
func TestExcludeAndRequireModel(t *testing.T) {
	in := ScoreInput{Role: "assistant", ModelID: "m", Nodes: threeComputers("here", "studio"),
		Profile:        contracts.AIProfile{NodePolicy: contracts.NodePolicy{Mode: "automatic"}},
		ExcludeNodeIDs: []string{"here"}, RequireModel: true}
	if got := placed(t, in); got != "studio" {
		t.Fatalf("retry placed on %q, want studio", got)
	}
	in.ExcludeNodeIDs = []string{"here", "studio"}
	if got := placed(t, in); got != "" {
		t.Fatalf("no other computer has the model, but placed on %q", got)
	}
}

// A worker slot follows the worker role's pin.
func TestWorkerSlotFollowsPin(t *testing.T) {
	in := ScoreInput{Role: "worker:2", ModelID: "m", Nodes: threeComputers("here", "studio", "pc"),
		Profile: contracts.AIProfile{NodePolicy: contracts.NodePolicy{Mode: "automatic"},
			Roles: []contracts.ModelRole{{Role: "worker", NodeID: "studio"}}}}
	if got := placed(t, in); got != "studio" {
		t.Fatalf("worker:2 placed on %q, want the worker's pinned studio", got)
	}
}
