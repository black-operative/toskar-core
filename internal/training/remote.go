package training

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Training on a paired computer.
//
// The computer that owns the AI (the coordinator) prepares the examples,
// sends them to the computer Norn picked, mirrors the run's progress into its
// own job, and downloads the adapter when the run completes. Evaluation and
// serving stay on the coordinator. Runs share the trainer computer's single
// training slot with its own jobs. Bifrost authenticates every call as a
// paired computer.

// RemoteProtocol is the version of the training routes a computer serves.
const RemoteProtocol = 1

// maxRemoteBody caps the examples one run may send.
const maxRemoteBody = 64 << 20

// Peer sends authenticated requests to a paired computer. *nodes.Client
// implements it.
type Peer interface {
	Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

// RemoteRunRequest starts a run on a paired computer. The examples already
// include the AI's instructions.
type RemoteRunRequest struct {
	ID           string `json:"id"`
	Repo         string `json:"repo"`
	Architecture string `json:"architecture"`
	Hyper        Hyper  `json:"hyper"`
	TrainJSONL   string `json:"train_jsonl"`
	ValidJSONL   string `json:"valid_jsonl"`
}

// RemoteRunStatus is a run as the trainer computer reports it.
type RemoteRunStatus struct {
	ID        string   `json:"id"`
	State     State    `json:"state"`
	Progress  Progress `json:"progress"`
	Error     string   `json:"error,omitempty"`
	TrainLoss *float64 `json:"train_loss,omitempty"`
	ValLoss   *float64 `json:"val_loss,omitempty"`
}

// RemoteCapabilities tells a coordinator whether this computer can train.
type RemoteCapabilities struct {
	Protocol  int             `json:"protocol"`
	Backends  []RemoteBackend `json:"backends"`
	Cached    map[string]bool `json:"cached"`
	Busy      bool            `json:"busy"`
	Installed map[string]bool `json:"env_installed"`
}

// RemoteBackend is one trainer on the trainer computer.
type RemoteBackend struct {
	ID        string `json:"id"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// remoteRun is a run this computer is executing for a coordinator.
type remoteRun struct {
	mu       sync.Mutex
	status   RemoteRunStatus
	cancel   context.CancelFunc
	adapter  string
	finished time.Time
	// deleted means the coordinator removed the run while it was still
	// running, so the run removes its own files when it stops.
	deleted bool
}

func (r *remoteRun) snapshot() RemoteRunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// remoteRunTTL removes finished runs the coordinator never collected.
const remoteRunTTL = 24 * time.Hour

func (s *Service) remoteDir(id string) string { return filepath.Join(s.d.DataDir, "remote", id) }

// RemoteHandler serves the trainer-computer side of the protocol under
// /training/. Mount it behind Bifrost's peer authentication.
func (s *Service) RemoteHandler() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/training/capabilities", s.handleCapabilities).Methods(http.MethodGet)
	r.HandleFunc("/training/runs", s.handleStartRun).Methods(http.MethodPost)
	r.HandleFunc("/training/runs/{id}", s.handleRunStatus).Methods(http.MethodGet)
	r.HandleFunc("/training/runs/{id}", s.handleDeleteRun).Methods(http.MethodDelete)
	r.HandleFunc("/training/runs/{id}/cancel", s.handleCancelRun).Methods(http.MethodPost)
	r.HandleFunc("/training/runs/{id}/adapter", s.handleRunAdapter).Methods(http.MethodGet)
	return r
}

func remoteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func remoteErr(w http.ResponseWriter, status int, msg string) {
	remoteJSON(w, status, map[string]string{"error": msg})
}

func (s *Service) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	var hw contracts.HardwareInventory
	if nodes, err := s.d.Nodes(r.Context()); err == nil {
		for _, n := range nodes {
			if n.Local {
				hw = n.Hardware
			}
		}
	}
	caps := RemoteCapabilities{Protocol: RemoteProtocol, Cached: map[string]bool{}, Installed: map[string]bool{}}
	for _, t := range s.d.Trainers {
		ok, why := s.supportsHere(t, hw)
		caps.Backends = append(caps.Backends, RemoteBackend{ID: t.ID(), Supported: ok, Reason: why})
		caps.Installed[t.ID()] = s.d.Python.Status(t.Environment()).Installed
	}
	for _, repo := range strings.Split(r.URL.Query().Get("repos"), ",") {
		if repo = strings.TrimSpace(repo); repo != "" {
			caps.Cached[repo] = s.cached(repo)
		}
	}
	caps.Busy = len(s.slot) > 0
	remoteJSON(w, http.StatusOK, caps)
}

func (s *Service) handleStartRun(w http.ResponseWriter, r *http.Request) {
	var req RemoteRunRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRemoteBody)).Decode(&req); err != nil {
		remoteErr(w, http.StatusBadRequest, "invalid run")
		return
	}
	if err := s.StartRemoteRun(req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrConflict) {
			status = http.StatusConflict
		}
		remoteErr(w, status, err.Error())
		return
	}
	remoteJSON(w, http.StatusAccepted, map[string]string{"id": req.ID})
}

func (s *Service) remoteRunFor(w http.ResponseWriter, r *http.Request) *remoteRun {
	s.mu.Lock()
	run := s.remote[mux.Vars(r)["id"]]
	s.mu.Unlock()
	if run == nil {
		remoteErr(w, http.StatusNotFound, "run not found")
	}
	return run
}

func (s *Service) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	if run := s.remoteRunFor(w, r); run != nil {
		remoteJSON(w, http.StatusOK, run.snapshot())
	}
}

func (s *Service) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if run := s.remoteRunFor(w, r); run != nil {
		run.cancel()
		remoteJSON(w, http.StatusAccepted, run.snapshot())
	}
}

func (s *Service) handleRunAdapter(w http.ResponseWriter, r *http.Request) {
	run := s.remoteRunFor(w, r)
	if run == nil {
		return
	}
	st := run.snapshot()
	if st.State != StateComplete {
		remoteErr(w, http.StatusConflict, "the run has not completed")
		return
	}
	f, err := os.Open(run.adapter)
	if err != nil {
		remoteErr(w, http.StatusGone, "the adapter is gone")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func (s *Service) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	s.mu.Lock()
	run := s.remote[id]
	delete(s.remote, id)
	s.mu.Unlock()
	if run != nil {
		run.mu.Lock()
		run.deleted = true
		run.mu.Unlock()
		run.cancel()
	}
	_ = os.RemoveAll(s.remoteDir(id))
	w.WriteHeader(http.StatusNoContent)
}

// StartRemoteRun runs training for a coordinator on this computer.
func (s *Service) StartRemoteRun(req RemoteRunRequest) error {
	if req.ID == "" || strings.ContainsAny(req.ID, `/\.`) {
		return fmt.Errorf("invalid run id")
	}
	if req.Repo == "" || req.Architecture == "" || strings.TrimSpace(req.TrainJSONL) == "" {
		return fmt.Errorf("the run is missing its model or examples")
	}
	var hw contracts.HardwareInventory
	if nodes, err := s.d.Nodes(context.Background()); err == nil {
		for _, n := range nodes {
			if n.Local {
				hw = n.Hardware
			}
		}
	}
	trainer := TrainerFor(s.d.Trainers, hw)
	if trainer == nil {
		return fmt.Errorf("no trainer is available")
	}
	if ok, why := s.supportsHere(trainer, hw); !ok {
		return fmt.Errorf("%s", why)
	}
	s.gcRemoteRuns()
	s.mu.Lock()
	if _, exists := s.remote[req.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("run %s already exists: %w", req.ID, ErrConflict)
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &remoteRun{status: RemoteRunStatus{ID: req.ID, State: StateQueued, Progress: Progress{Detail: "Waiting to start", Iters: req.Hyper.Iters, Epochs: req.Hyper.Epochs}}, cancel: cancel}
	s.remote[req.ID] = run
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.executeRemote(ctx, run, req, trainer)
		// A delete that arrived while the run was stopping may have raced
		// its last writes.
		run.mu.Lock()
		deleted := run.deleted
		run.mu.Unlock()
		if deleted {
			_ = os.RemoveAll(s.remoteDir(req.ID))
		}
	}()
	return nil
}

func (s *Service) executeRemote(ctx context.Context, run *remoteRun, req RemoteRunRequest, trainer Trainer) {
	set := func(f func(st *RemoteRunStatus)) {
		run.mu.Lock()
		f(&run.status)
		run.mu.Unlock()
	}
	finish := func(state State, err error) {
		set(func(st *RemoteRunStatus) {
			st.State = state
			st.Progress.RemainingSec = nil
			if err != nil {
				st.Error = err.Error()
			}
		})
		run.mu.Lock()
		run.finished = time.Now()
		run.mu.Unlock()
		// Keep only the adapter for the coordinator to collect.
		_ = os.RemoveAll(filepath.Join(s.remoteDir(req.ID), "work"))
		s.d.Logger.Info("remote training run finished", "run_id", req.ID, "state", state)
	}

	select {
	case s.slot <- struct{}{}:
		defer func() { <-s.slot }()
	case <-ctx.Done():
		finish(StateCancelled, nil)
		return
	}
	dir := s.remoteDir(req.ID)
	dataDir := filepath.Join(dir, "work", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		finish(StateFailed, err)
		return
	}
	valid := req.ValidJSONL
	if strings.TrimSpace(valid) == "" {
		valid = req.TrainJSONL
	}
	for name, body := range map[string]string{"train.jsonl": req.TrainJSONL, "valid.jsonl": valid} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(body), 0o644); err != nil {
			finish(StateFailed, err)
			return
		}
	}
	adapter := filepath.Join(dir, "adapter.gguf")
	res, err := s.execute(ctx, execSpec{
		name: "a paired computer's AI", trainer: trainer, repo: req.Repo, architecture: req.Architecture, hyper: req.Hyper,
		workDir: filepath.Join(dir, "work"), dataDir: dataDir, adapterOut: adapter,
		logPath: filepath.Join(s.d.LogsDir, "training-remote-"+req.ID+".log"),
	}, func(st State, detail string) {
		set(func(r *RemoteRunStatus) { r.State, r.Progress.Detail = st, detail })
	}, func(p Progress) {
		set(func(r *RemoteRunStatus) {
			if p.Iters == 0 {
				p.Iters, p.Epochs = r.Progress.Iters, r.Progress.Epochs
			}
			r.Progress = p
		})
	})
	switch {
	case errors.Is(err, ErrCancelled) || ctx.Err() != nil:
		_ = os.Remove(adapter)
		finish(StateCancelled, nil)
	case err != nil:
		_ = os.Remove(adapter)
		finish(StateFailed, err)
	default:
		run.mu.Lock()
		run.adapter = res.AdapterPath
		run.status.TrainLoss, run.status.ValLoss = res.TrainLoss, res.ValLoss
		run.mu.Unlock()
		finish(StateComplete, nil)
	}
}

// gcRemoteRuns forgets finished runs no coordinator collected.
func (s *Service) gcRemoteRuns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, run := range s.remote {
		run.mu.Lock()
		stale := !run.finished.IsZero() && time.Since(run.finished) > remoteRunTTL
		run.mu.Unlock()
		if stale {
			delete(s.remote, id)
			_ = os.RemoveAll(s.remoteDir(id))
		}
	}
}

// remoteCapabilities asks a paired computer whether it can train.
func (s *Service) remoteCapabilities(ctx context.Context, nodeID string, repos []string) (RemoteCapabilities, error) {
	if s.d.Peer == nil {
		return RemoteCapabilities{}, fmt.Errorf("remote training is not configured")
	}
	peer, err := s.d.Peer(ctx, nodeID)
	if err != nil {
		return RemoteCapabilities{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	resp, err := peer.Do(ctx, http.MethodGet, "/internal/v1/training/capabilities?repos="+strings.Join(repos, ","), nil)
	if err != nil {
		return RemoteCapabilities{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return RemoteCapabilities{}, errOldPeer
	}
	var caps RemoteCapabilities
	if err := decodePeer(resp, &caps); err != nil {
		return RemoteCapabilities{}, err
	}
	return caps, nil
}

var errOldPeer = errors.New("that computer runs a Toskar version without remote training")

func decodePeer(resp *http.Response, v any) error {
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// remotePollInterval and remoteLostAfter govern how the coordinator follows a
// run. A computer that stops answering for remoteLostAfter fails the job.
var (
	remotePollInterval = 2 * time.Second
	remoteLostAfter    = 90 * time.Second
)

// remoteSendTimeout bounds sending a run's examples, which a cancel waits for.
const remoteSendTimeout = 2 * time.Minute

// runOnPeer sends a run to job.NodeID, follows it, and downloads the adapter
// to adapterOut.
func (s *Service) runOnPeer(ctx context.Context, job Job, req RemoteRunRequest, adapterOut string,
	setState func(State, string), progress func(Progress)) (RunResult, error) {
	name := job.NodeName
	if name == "" {
		name = "the other computer"
	}
	if s.d.Peer == nil {
		return RunResult{}, fmt.Errorf("remote training is not configured")
	}
	peer, err := s.d.Peer(ctx, job.NodeID)
	if err != nil {
		return RunResult{}, err
	}
	if s.d.Sent != nil {
		s.d.Sent(ctx, name, fmt.Sprintf("training examples and instructions for %s", req.Repo))
	}
	bg := context.Background()
	cleanup := func() {
		c, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		if resp, err := peer.Do(c, http.MethodDelete, "/internal/v1/training/runs/"+req.ID, nil); err == nil {
			resp.Body.Close()
		}
	}

	setState(StatePreparing, "Sending examples to "+name)
	body, _ := json.Marshal(req)
	// The trainer computer may start the run before its reply arrives, so a
	// cancel must not abandon the request: only the reply says whether there
	// is a run to stop.
	sctx, sendDone := context.WithTimeout(context.WithoutCancel(ctx), remoteSendTimeout)
	resp, err := peer.Do(sctx, http.MethodPost, "/internal/v1/training/runs", bytes.NewReader(body))
	if err != nil {
		sendDone()
		// The run may have started even though the reply was lost.
		cleanup()
		if ctx.Err() != nil {
			return RunResult{}, ErrCancelled
		}
		return RunResult{}, fmt.Errorf("could not reach %s: %w", name, err)
	}
	err = decodePeer(resp, nil)
	resp.Body.Close()
	sendDone()
	if err != nil {
		return RunResult{}, fmt.Errorf("%s refused the run: %w", name, err)
	}
	if ctx.Err() != nil {
		cleanup()
		return RunResult{}, ErrCancelled
	}

	lastContact := time.Now()
	var st RemoteRunStatus
	for {
		select {
		case <-ctx.Done():
			s.cancelOnPeer(peer, req.ID)
			cleanup()
			return RunResult{}, ErrCancelled
		case <-time.After(remotePollInterval):
		}
		pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		resp, err := peer.Do(pctx, http.MethodGet, "/internal/v1/training/runs/"+req.ID, nil)
		if err == nil {
			err = decodePeer(resp, &st)
			resp.Body.Close()
		}
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			if time.Since(lastContact) > remoteLostAfter {
				s.cancelOnPeer(peer, req.ID)
				return RunResult{}, fmt.Errorf("lost contact with %s during training: %w", name, err)
			}
			continue
		}
		lastContact = time.Now()
		p := st.Progress
		if st.State == StateQueued {
			p.Detail = name + " is finishing another training run"
		} else if p.Detail != "" {
			p.Detail = p.Detail + " on " + name
		}
		progress(p)
		switch st.State {
		case StateComplete:
			if err := s.downloadAdapter(ctx, peer, req.ID, adapterOut); err != nil {
				cleanup()
				if ctx.Err() != nil {
					return RunResult{}, ErrCancelled
				}
				return RunResult{}, fmt.Errorf("download the adapter from %s: %w", name, err)
			}
			cleanup()
			return RunResult{AdapterPath: adapterOut, TrainLoss: st.TrainLoss, ValLoss: st.ValLoss}, nil
		case StateFailed:
			cleanup()
			return RunResult{}, fmt.Errorf("on %s: %s", name, st.Error)
		case StateCancelled:
			cleanup()
			return RunResult{}, fmt.Errorf("the run was cancelled on %s", name)
		default:
			setState(st.State, p.Detail)
		}
	}
}

func (s *Service) cancelOnPeer(peer Peer, id string) {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if resp, err := peer.Do(c, http.MethodPost, "/internal/v1/training/runs/"+id+"/cancel", nil); err == nil {
		resp.Body.Close()
	}
}

func (s *Service) downloadAdapter(ctx context.Context, peer Peer, id, out string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	resp, err := peer.Do(ctx, http.MethodGet, "/internal/v1/training/runs/"+id+"/adapter", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodePeer(resp, nil)
	}
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}
