package scheduler

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestScorePinnedNodeWins(t *testing.T) {
	input := ScoreInput{
		Role:    "worker",
		ModelID: "m1",
		Profile: contracts.AIProfile{
			Roles: []contracts.ModelRole{{Role: "worker", ModelID: "m1", NodeID: "node-a"}},
		},
		Nodes: []NodeCandidate{
			{Node: contracts.Node{ID: "node-a", Status: contracts.NodeStatusOnline, Paired: true, IsLocal: true}, InstalledModels: map[string]struct{}{"m1": {}}},
			{Node: contracts.Node{ID: "node-b", Status: contracts.NodeStatusOnline, Paired: true}, InstalledModels: map[string]struct{}{"m1": {}}},
		},
	}
	scored := Score(input)
	if len(scored) < 2 {
		t.Fatal("expected scores")
	}
	if scored[0].NodeID != "node-a" || scored[0].Score <= scored[1].Score {
		// find node-a score
		var aScore, bScore int
		for _, s := range scored {
			if s.NodeID == "node-a" {
				aScore = s.Score
			}
			if s.NodeID == "node-b" {
				bScore = s.Score
			}
		}
		if aScore <= bScore {
			t.Fatalf("pinned node should win: a=%d b=%d", aScore, bScore)
		}
	}
}

func TestScoreLocalTieBreaker(t *testing.T) {
	input := ScoreInput{
		Role:    "worker",
		ModelID: "m1",
		Profile: contracts.AIProfile{NodePolicy: contracts.NodePolicy{Mode: "automatic"}},
		Nodes: []NodeCandidate{
			{Node: contracts.Node{ID: "local", Status: contracts.NodeStatusOnline, IsLocal: true, Paired: true}, InstalledModels: map[string]struct{}{"m1": {}}, FreeMemory: 8e9},
			{Node: contracts.Node{ID: "remote", Status: contracts.NodeStatusOnline, Paired: true}, InstalledModels: map[string]struct{}{"m1": {}}, FreeMemory: 8e9},
		},
	}
	scored := Score(input)
	var localScore, remoteScore int
	for _, s := range scored {
		if s.NodeID == "local" {
			localScore = s.Score
		}
		if s.NodeID == "remote" {
			remoteScore = s.Score
		}
	}
	if localScore <= remoteScore {
		t.Fatalf("local should beat remote on tie: local=%d remote=%d", localScore, remoteScore)
	}
}
