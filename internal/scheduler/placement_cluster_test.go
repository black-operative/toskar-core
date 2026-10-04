package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Placement prefers the paired remote that has the model installed, and honors role pins.
func TestPlaceRolePrefersRemoteInstalledAndPin(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{
		ID:             "team",
		OrchestratorID: "team",
		Roles: []contracts.ModelRole{
			{Role: "coordinator", ModelID: "coord-m", NodeID: "node-a"},
			{Role: "worker", ModelID: "worker-m", NodeID: "node-b"},
		},
	}
	nodes := []NodeCandidate{
		{
			Node:            contracts.Node{ID: "node-a", Name: "A", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline},
			InstalledModels: map[string]struct{}{"coord-m": {}},
		},
		{
			Node:            contracts.Node{ID: "node-b", Name: "B", Paired: true, Status: contracts.NodeStatusOnline, Address: "192.168.1.2:7332"},
			InstalledModels: map[string]struct{}{"worker-m": {}},
		},
	}

	coord, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "coordinator", ModelID: "coord-m", Profile: profile, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coord.NodeID != "node-a" {
		t.Fatalf("coordinator -> %s", coord.NodeID)
	}

	worker, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "worker", ModelID: "worker-m", Profile: profile, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.NodeID != "node-b" {
		t.Fatalf("worker -> %s", worker.NodeID)
	}
}

func TestPlacePinnedNodeOfflineFailsClearly(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{
		Roles: []contracts.ModelRole{
			{Role: "worker", ModelID: "m1", NodeID: "node-b"},
		},
	}
	nodes := []NodeCandidate{
		{
			Node:            contracts.Node{ID: "node-a", Name: "Desktop", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
		{
			Node:            contracts.Node{ID: "node-b", Name: "Laptop", Paired: true, Status: contracts.NodeStatusOffline, Address: "10.0.0.2:7332"},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
	}
	_, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "worker", ModelID: "m1", Profile: profile, Nodes: nodes,
	})
	if err == nil {
		t.Fatal("expected pin-offline error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Laptop") || !strings.Contains(msg, "offline") {
		t.Fatalf("want clear offline message, got %q", msg)
	}
}

func TestPlaceAutomaticSkipsOfflinePeer(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{
		Roles: []contracts.ModelRole{
			{Role: "worker", ModelID: "m1"}, // Automatic — no pin
		},
	}
	nodes := []NodeCandidate{
		{
			Node:            contracts.Node{ID: "node-a", Name: "Desktop", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
		{
			Node:            contracts.Node{ID: "node-b", Name: "Laptop", Paired: true, Status: contracts.NodeStatusOffline, Address: "10.0.0.2:7332"},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
	}
	d, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "worker", ModelID: "m1", Profile: profile, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.NodeID != "node-a" {
		t.Fatalf("expected local failover to node-a, got %s", d.NodeID)
	}
}

func TestPlaceAutomaticSpreadsTeamRolesAcrossNodes(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{
		NodePolicy: contracts.NodePolicy{Mode: "automatic"},
		Roles: []contracts.ModelRole{
			{Role: "coordinator", ModelID: "m1"},
			{Role: "worker", ModelID: "m1"},
		},
	}
	nodes := []NodeCandidate{
		{
			Node:            contracts.Node{ID: "node-a", Name: "Desktop", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
		{
			Node:            contracts.Node{ID: "node-b", Name: "Laptop", Paired: true, Status: contracts.NodeStatusOnline, Address: "10.0.0.2:7332"},
			InstalledModels: map[string]struct{}{"m1": {}},
		},
	}
	coord, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "coordinator", ModelID: "m1", Profile: profile, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "worker", ModelID: "m1", Profile: profile, Nodes: nodes,
		AvoidNodeIDs: []string{coord.NodeID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.NodeID == coord.NodeID {
		t.Fatalf("expected worker on a different node than coordinator (%s)", coord.NodeID)
	}
}

func TestPlaceRolePrefersNodeWithModelInstalled(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{
		ID: "p", OrchestratorID: "simple",
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "only-on-b"}},
	}
	nodes := []NodeCandidate{
		{
			Node:            contracts.Node{ID: "node-a", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline},
			InstalledModels: map[string]struct{}{},
		},
		{
			Node:            contracts.Node{ID: "node-b", Paired: true, Status: contracts.NodeStatusOnline, Address: "10.0.0.2:7332"},
			InstalledModels: map[string]struct{}{"only-on-b": {}},
		},
	}
	d, err := s.PlaceRole(context.Background(), ScoreInput{
		Role: "assistant", ModelID: "only-on-b", Profile: profile, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.NodeID != "node-b" {
		t.Fatalf("expected node-b, got %s", d.NodeID)
	}
}

// A computer already answering something gives way to an idle one that has
// the model (#111).
func TestPlaceAutomaticPrefersAnIdleComputer(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{ID: "general", Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}}}
	nodes := []NodeCandidate{
		{Node: contracts.Node{ID: "here", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline}, InstalledModels: map[string]struct{}{"m": {}}},
		{Node: contracts.Node{ID: "gpu-box", Paired: true, Status: contracts.NodeStatusOnline, Address: "10.0.0.2:7332"}, InstalledModels: map[string]struct{}{"m": {}}},
	}
	idle, err := s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: profile, Nodes: nodes})
	if err != nil || idle.NodeID != "here" {
		t.Fatalf("both idle -> %s, %v", idle.NodeID, err)
	}
	busy, err := s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: profile, Nodes: nodes,
		ActiveTasks: map[string]int{"here": 1}})
	if err != nil || busy.NodeID != "gpu-box" {
		t.Fatalf("this computer busy -> %s, %v", busy.NodeID, err)
	}
}

// Work goes to another computer with the model while this one trains, yet a
// training computer that is the only one with the model still answers (#111).
func TestPlaceAvoidsAComputerThatIsTraining(t *testing.T) {
	s := New(nil)
	profile := contracts.AIProfile{ID: "general", Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}}}
	here := NodeCandidate{Node: contracts.Node{ID: "here", IsLocal: true, Paired: true, Status: contracts.NodeStatusOnline}, InstalledModels: map[string]struct{}{"m": {}}}
	gpu := NodeCandidate{Node: contracts.Node{ID: "gpu-box", Paired: true, Status: contracts.NodeStatusOnline, Address: "10.0.0.2:7332"}, InstalledModels: map[string]struct{}{"m": {}}}
	training := map[string]bool{"here": true}

	got, err := s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: profile, Nodes: []NodeCandidate{here, gpu}, Training: training,
		RunningModels: map[string]map[string]struct{}{"here": {"m": {}}}})
	if err != nil || got.NodeID != "gpu-box" {
		t.Fatalf("training here, model on both -> %s, %v", got.NodeID, err)
	}
	got, err = s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: profile, Nodes: []NodeCandidate{here}, Training: training})
	if err != nil || got.NodeID != "here" {
		t.Fatalf("only computer, training -> %s, %v", got.NodeID, err)
	}
	gpuNoModel := gpu
	gpuNoModel.InstalledModels = map[string]struct{}{}
	got, err = s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: profile, Nodes: []NodeCandidate{here, gpuNoModel}, Training: training})
	if err != nil || got.NodeID != "here" {
		t.Fatalf("other computer lacks the model -> %s, %v", got.NodeID, err)
	}
	local := profile
	local.NodePolicy.Mode = "prefer_local"
	got, err = s.PlaceRole(context.Background(), ScoreInput{Role: "assistant", ModelID: "m", Profile: local, Nodes: []NodeCandidate{here, gpu}, Training: training})
	if err != nil || got.NodeID != "here" {
		t.Fatalf("prefer_local while training -> %s, %v", got.NodeID, err)
	}
}
