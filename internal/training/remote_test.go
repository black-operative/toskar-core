package training

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// httpPeer calls a trainer computer's remote handler, the way *nodes.Client
// does after Bifrost strips its /internal/v1 prefix.
type httpPeer struct{ base string }

func (p httpPeer) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, body)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// switchable lets a test replace the peer's handler while requests run.
type switchable struct{ h atomic.Value }

func (s *switchable) set(h http.Handler) { s.h.Store(&h) }
func (s *switchable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.h.Load().(*http.Handler)).ServeHTTP(w, r)
}

// pair returns a coordinator whose only eligible trainer is a paired
// computer running trainer.
func pair(t *testing.T, trainerMode string) (coord, trainer *harness, peer *switchable) {
	t.Helper()
	trainer = newHarness(t, trainerMode)
	peer = &switchable{}
	peer.set(http.StripPrefix("/internal/v1", trainer.svc.RemoteHandler()))
	srv := httptest.NewServer(peer)
	t.Cleanup(srv.Close)
	coord = newHarness(t, "ok")
	coord.svc.d.Nodes = func(ctx context.Context) ([]Node, error) {
		return []Node{
			// This computer is too small to train, so Norn must pick the peer.
			{ID: "local", Name: "laptop", Local: true, Online: true, Hardware: mac(2)},
			{ID: "studio", Name: "studio", Online: true, Hardware: mac(64)},
		}, nil
	}
	coord.svc.d.Peer = func(ctx context.Context, nodeID string) (Peer, error) {
		if nodeID != "studio" {
			return nil, errors.New("unknown computer")
		}
		return httpPeer{base: srv.URL}, nil
	}
	return coord, trainer, peer
}

func fastPolling(t *testing.T) {
	t.Helper()
	oldPoll, oldLost := remotePollInterval, remoteLostAfter
	remotePollInterval, remoteLostAfter = 20*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { remotePollInterval, remoteLostAfter = oldPoll, oldLost })
}

func readyAI(t *testing.T, h *harness) SpecializedAI {
	t.Helper()
	ctx := context.Background()
	ai, err := h.svc.CreateAI(ctx, CreateInput{Name: "Tire Bot", BaseModelID: "small-q4"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(20)}); err != nil {
		t.Fatal(err)
	}
	return ai
}

func TestTrainOnPairedComputer(t *testing.T) {
	fastPolling(t)
	coord, trainer, _ := pair(t, "ok")
	ctx := context.Background()
	ai := readyAI(t, coord)

	plan, err := coord.svc.Plan(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || plan.Chosen == nil || plan.Chosen.NodeID != "studio" {
		t.Fatalf("plan = %+v", plan)
	}
	job, err := coord.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, coord, job.ID)
	trainer.svc.Wait()
	if job.State != StateComplete || job.NodeID != "studio" {
		t.Fatalf("job = %+v", job)
	}
	// The trainer computer trained on the examples it was sent, with the
	// AI's instructions already in them.
	data, err := os.ReadFile(trainer.trainer.spec.DataDir + "/train.jsonl")
	if err == nil && !strings.Contains(string(data), "You are Tire Bot.") {
		t.Fatalf("train.jsonl = %.200s", data)
	}
	// The adapter came back, and evaluation and serving happen here.
	view, _ := coord.svc.View(ctx, ai.ID)
	if len(view.Revisions) != 1 || !view.Revisions[0].Evaluated {
		t.Fatalf("revisions = %+v", view.Revisions)
	}
	if b, err := os.ReadFile(view.Revisions[0].adapterPath); err != nil || string(b) != "gguf" {
		t.Fatalf("adapter = %q %v", b, err)
	}
	// The trainer computer removed its copy once the coordinator collected it.
	if _, err := os.Stat(trainer.svc.remoteDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("the trainer computer kept the run's files")
	}
	trainer.svc.mu.Lock()
	left := len(trainer.svc.remote)
	trainer.svc.mu.Unlock()
	if left != 0 {
		t.Fatalf("the trainer computer still tracks %d runs", left)
	}
}

func TestCancelRemoteTraining(t *testing.T) {
	fastPolling(t)
	coord, trainer, _ := pair(t, "block")
	ctx := context.Background()
	ai := readyAI(t, coord)
	job, err := coord.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-trainer.trainer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the trainer computer did not start")
	}
	if _, err := coord.svc.CancelJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, coord, job.ID)
	trainer.svc.Wait()
	if job.State != StateCancelled {
		t.Fatalf("state = %s (%s)", job.State, job.Error)
	}
	if _, err := os.Stat(trainer.svc.remoteDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("the trainer computer kept the cancelled run's files")
	}
}

func TestCancelWhileSendingRun(t *testing.T) {
	fastPolling(t)
	coord, trainer, peer := pair(t, "block")
	ctx := context.Background()
	remote := http.StripPrefix("/internal/v1", trainer.svc.RemoteHandler())
	// The trainer computer starts the run, and the job is cancelled before
	// its reply reaches the coordinator.
	peer.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/training/runs") {
			remote.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		remote.ServeHTTP(rec, r)
		var started struct{ ID string }
		if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
			t.Error(err)
		}
		<-trainer.trainer.started
		if _, err := coord.svc.CancelJob(ctx, started.ID); err != nil {
			t.Error(err)
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	ai := readyAI(t, coord)
	job, err := coord.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, coord, job.ID)
	trainer.svc.Wait()
	if job.State != StateCancelled {
		t.Fatalf("state = %s (%s)", job.State, job.Error)
	}
	if _, err := os.Stat(trainer.svc.remoteDir(job.ID)); !os.IsNotExist(err) {
		t.Fatal("the trainer computer kept the cancelled run's files")
	}
}

func TestRemoteFailureIsReported(t *testing.T) {
	fastPolling(t)
	coord, trainer, _ := pair(t, "fail")
	ctx := context.Background()
	ai := readyAI(t, coord)
	job, _ := coord.svc.StartTraining(ctx, ai.ID, "")
	job = waitJob(t, coord, job.ID)
	trainer.svc.Wait()
	if job.State != StateFailed || !strings.Contains(job.Error, "on studio") || !strings.Contains(job.Error, "out of memory") {
		t.Fatalf("job = %s %q", job.State, job.Error)
	}
}

func TestLostContactFailsTheJob(t *testing.T) {
	fastPolling(t)
	coord, trainer, peer := pair(t, "block")
	ctx := context.Background()
	ai := readyAI(t, coord)
	job, _ := coord.svc.StartTraining(ctx, ai.ID, "")
	select {
	case <-trainer.trainer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the trainer computer did not start")
	}
	peer.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusServiceUnavailable)
	}))
	job = waitJob(t, coord, job.ID)
	if job.State != StateFailed || !strings.Contains(job.Error, "lost contact with studio") {
		t.Fatalf("job = %s %q", job.State, job.Error)
	}
	// Stop the orphaned run on the trainer side for the test's cleanup.
	trainer.svc.mu.Lock()
	for _, run := range trainer.svc.remote {
		run.cancel()
	}
	trainer.svc.mu.Unlock()
	trainer.svc.Wait()
}

func TestOlderPeerIsNotEligible(t *testing.T) {
	coord, _, peer := pair(t, "ok")
	peer.set(http.NotFoundHandler())
	plan, err := coord.svc.Plan(context.Background(), readyAI(t, coord).ID)
	if err != nil {
		t.Fatal(err)
	}
	var studio NodeFit
	for _, f := range plan.Fits {
		if f.NodeID == "studio" {
			studio = f
		}
	}
	if studio.Eligible || !strings.Contains(studio.Reason, "Update Toskar on studio") {
		t.Fatalf("studio fit = %+v", studio)
	}
	if plan.Ready {
		t.Fatal("no computer can train, so the plan must not be ready")
	}
}

func TestChooseTheTrainingComputer(t *testing.T) {
	fastPolling(t)
	coord, trainer, _ := pair(t, "ok")
	ctx := context.Background()
	// Give this computer enough memory; Norn now prefers it.
	coord.svc.d.Nodes = func(ctx context.Context) ([]Node, error) {
		return []Node{
			{ID: "local", Name: "laptop", Local: true, Online: true, Hardware: mac(64)},
			{ID: "studio", Name: "studio", Online: true, Hardware: mac(64)},
		}, nil
	}
	ai := readyAI(t, coord)
	if plan, _ := coord.svc.Plan(ctx, ai.ID); plan.Chosen == nil || plan.Chosen.NodeID != "local" {
		t.Fatalf("Norn should keep a tie local: %+v", plan.Chosen)
	}
	if _, err := coord.svc.StartTraining(ctx, ai.ID, "nowhere"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unknown computer: %v", err)
	}
	job, err := coord.svc.StartTraining(ctx, ai.ID, "studio")
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, coord, job.ID)
	trainer.svc.Wait()
	if job.State != StateComplete || job.NodeID != "studio" {
		t.Fatalf("job = %+v", job)
	}
}

func TestRemoteRunValidation(t *testing.T) {
	h := newHarness(t, "ok")
	for _, req := range []RemoteRunRequest{
		{ID: "../escape", Repo: "r", Architecture: "qwen2", TrainJSONL: "{}"},
		{ID: "ok", Architecture: "qwen2", TrainJSONL: "{}"},
		{ID: "ok2", Repo: "r", Architecture: "qwen2"},
	} {
		if err := h.svc.StartRemoteRun(req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}
