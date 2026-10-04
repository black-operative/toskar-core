package training

import (
	"context"
	"strings"
	"testing"
)

// A sandboxed app cannot run the trainer's downloaded Python (#95). Its own
// computer is not offered for training, and a paired computer is chosen.
func TestSandboxedComputerTrainsOnAPairedOne(t *testing.T) {
	fastPolling(t)
	coord, _, _ := pair(t, "ok")
	coord.svc.d.Nodes = func(ctx context.Context) ([]Node, error) {
		return []Node{
			// Big enough to train, but sandboxed.
			{ID: "local", Name: "laptop", Local: true, Online: true, Hardware: mac(64)},
			{ID: "studio", Name: "studio", Online: true, Hardware: mac(64)},
		}, nil
	}
	coord.py.sandboxed = true
	ctx := context.Background()
	ai := readyAI(t, coord)

	plan, err := coord.svc.Plan(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || plan.Chosen == nil || plan.Chosen.NodeID != "studio" {
		t.Fatalf("plan = %+v", plan)
	}
	local := fitNamed(plan.Fits, "local")
	if local.Eligible || local.Label != FitUnsupported || !strings.Contains(local.Reason, "App Sandbox") ||
		!strings.Contains(local.Reason, "Pair a computer running Toskar Core") {
		t.Fatalf("local fit = %+v", local)
	}
	for _, b := range coord.svc.Backends(ctx) {
		if b["supported"] != false || !strings.Contains(b["reason"].(string), "App Sandbox") {
			t.Fatalf("backend = %+v", b)
		}
	}
	job, err := coord.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, coord, job.ID); j.State != StateComplete || j.NodeID != "studio" {
		t.Fatalf("job = %+v", j)
	}
}

// A paired computer that is sandboxed says so, and is not chosen.
func TestSandboxedPeerIsNotChosen(t *testing.T) {
	fastPolling(t)
	coord, trainer, _ := pair(t, "ok")
	trainer.py.sandboxed = true
	ctx := context.Background()
	ai := readyAI(t, coord)

	plan, err := coord.svc.Plan(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	studio := fitNamed(plan.Fits, "studio")
	if studio.Eligible || !strings.Contains(studio.Reason, "App Sandbox") || !strings.Contains(studio.Reason, "on studio") {
		t.Fatalf("studio fit = %+v", studio)
	}
	if plan.Ready {
		t.Fatalf("the laptop is too small and the studio is sandboxed, yet the plan is ready: %+v", plan)
	}
	// A run sent to it anyway is refused with the reason.
	err = trainer.svc.StartRemoteRun(RemoteRunRequest{ID: "r1", Repo: "x", Architecture: "qwen2", Hyper: Hyper{Iters: 1}, TrainJSONL: tireExamples(2)})
	if err == nil || !strings.Contains(err.Error(), "App Sandbox") {
		t.Fatalf("remote run: %v", err)
	}
}

func fitNamed(fits []NodeFit, id string) NodeFit {
	for _, f := range fits {
		if f.NodeID == id {
			return f
		}
	}
	return NodeFit{}
}
