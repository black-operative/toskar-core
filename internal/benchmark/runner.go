package benchmark

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ModelPathFunc resolves an installed model path.
type ModelPathFunc func(ctx context.Context, modelID string) (string, error)

// StartModelFunc loads a model and returns a running instance.
type StartModelFunc func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error)

// StopModelFunc unloads a running instance.
type StopModelFunc func(ctx context.Context, instanceID string) error

// ListRunningFunc lists loaded models.
type ListRunningFunc func(ctx context.Context) ([]pluginapi.RunningModel, error)

// ChatFunc streams a completion.
type ChatFunc func(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error)

// Runner executes multi-model workload benchmarks.
type Runner struct {
	ModelPath   ModelPathFunc
	StartModel  StartModelFunc
	StopModel   StopModelFunc
	ListRunning ListRunningFunc
	Chat        ChatFunc
	// Admit, when set, waits until no higher-priority work such as a chat
	// is using this computer (spec §60). Each model is admitted on its own,
	// because loading it unloads the others.
	Admit func(ctx context.Context, waiting func(reason string)) (release func(), err error)

	mu   sync.Mutex
	jobs map[string]*contracts.BenchmarkJob
	canc map[string]context.CancelFunc
}

// NewRunner creates an empty job registry.
func NewRunner() *Runner {
	return &Runner{
		jobs: make(map[string]*contracts.BenchmarkJob),
		canc: make(map[string]context.CancelFunc),
	}
}

// ListWorkloads returns the catalog.
func (r *Runner) ListWorkloads() []contracts.BenchmarkWorkload {
	return Workloads()
}

// Start validates and launches a benchmark job.
func (r *Runner) Start(parent context.Context, req contracts.BenchmarkRequest) (*contracts.BenchmarkJob, error) {
	if len(req.ModelIDs) < 1 {
		return nil, fmt.Errorf("select at least one model")
	}
	if len(req.ModelIDs) > 6 {
		return nil, fmt.Errorf("select at most 6 models")
	}
	if len(req.WorkloadIDs) == 0 {
		for _, w := range Workloads() {
			req.WorkloadIDs = append(req.WorkloadIDs, w.ID)
		}
	}
	var workloads []contracts.BenchmarkWorkload
	for _, id := range req.WorkloadIDs {
		w, ok := WorkloadByID(id)
		if !ok {
			return nil, fmt.Errorf("unknown workload %q", id)
		}
		workloads = append(workloads, w)
	}
	runs := req.Runs
	if runs <= 0 {
		runs = 2
	}
	if runs > 5 {
		runs = 5
	}
	req.Runs = runs

	promptCount := 0
	for _, w := range workloads {
		promptCount += len(w.Prompts)
	}
	// warm-up + measured runs per prompt per model
	stepsPerModel := promptCount * (1 + runs)
	totalSteps := stepsPerModel * len(req.ModelIDs)

	job := &contracts.BenchmarkJob{
		ID:      uuid.NewString(),
		Status:  contracts.BenchmarkPending,
		Request: req,
		Progress: contracts.BenchmarkProgress{
			Phase:      "queued",
			TotalSteps: totalSteps,
			Message:    "Waiting to start…",
		},
		Samples:   []contracts.BenchmarkSample{},
		Summaries: []contracts.BenchmarkModelSummary{},
		Winners:   map[string]string{},
		CreatedAt: time.Now().UTC(),
	}

	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.jobs[job.ID] = job
	r.canc[job.ID] = cancel
	r.mu.Unlock()

	go r.execute(ctx, job, workloads)
	return cloneJob(job), nil
}

// Get returns a job snapshot.
func (r *Runner) Get(id string) (*contracts.BenchmarkJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, fmt.Errorf("benchmark %q not found", id)
	}
	return cloneJob(job), nil
}

// List returns recent jobs newest first.
func (r *Runner) List() []contracts.BenchmarkJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]contracts.BenchmarkJob, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, *cloneJob(j))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Cancel requests cancellation of a running job.
func (r *Runner) Cancel(id string) error {
	r.mu.Lock()
	cancel, ok := r.canc[id]
	job := r.jobs[id]
	r.mu.Unlock()
	if !ok || job == nil {
		return fmt.Errorf("benchmark %q not found", id)
	}
	cancel()
	return nil
}

func (r *Runner) execute(ctx context.Context, job *contracts.BenchmarkJob, workloads []contracts.BenchmarkWorkload) {
	now := time.Now().UTC()
	r.patch(job.ID, func(j *contracts.BenchmarkJob) {
		j.Status = contracts.BenchmarkRunning
		j.StartedAt = &now
		j.Progress.Phase = "running"
		j.Progress.Message = "Starting benchmark…"
	})

	defer func() {
		r.mu.Lock()
		delete(r.canc, job.ID)
		r.mu.Unlock()
	}()

	for _, modelID := range job.Request.ModelIDs {
		if ctx.Err() != nil {
			r.failOrCancel(job.ID, ctx.Err())
			return
		}
		if err := r.benchmarkModel(ctx, job, modelID, workloads); err != nil {
			if ctx.Err() != nil {
				r.failOrCancel(job.ID, ctx.Err())
				return
			}
			r.patch(job.ID, func(j *contracts.BenchmarkJob) {
				j.Status = contracts.BenchmarkFailed
				j.Error = err.Error()
				j.Progress.Phase = "failed"
				j.Progress.Message = err.Error()
				done := time.Now().UTC()
				j.CompletedAt = &done
			})
			return
		}
	}

	r.patch(job.ID, func(j *contracts.BenchmarkJob) {
		j.Summaries = summarize(j.Samples)
		j.Winners = pickWinners(j.Summaries)
		j.Status = contracts.BenchmarkCompleted
		j.Progress.Phase = "completed"
		j.Progress.Percent = 100
		j.Progress.Message = "Benchmark complete"
		j.Progress.CompletedSteps = j.Progress.TotalSteps
		done := time.Now().UTC()
		j.CompletedAt = &done
	})
}

func (r *Runner) benchmarkModel(
	ctx context.Context,
	job *contracts.BenchmarkJob,
	modelID string,
	workloads []contracts.BenchmarkWorkload,
) error {
	if r.Admit != nil {
		release, err := r.Admit(ctx, func(reason string) {
			r.patch(job.ID, func(j *contracts.BenchmarkJob) {
				j.Progress.CurrentModel = modelID
				j.Progress.Phase = "waiting"
				j.Progress.Message = reason
			})
		})
		if err != nil {
			return err
		}
		defer release()
	}
	r.patch(job.ID, func(j *contracts.BenchmarkJob) {
		j.Progress.CurrentModel = modelID
		j.Progress.Phase = "loading"
		j.Progress.Message = "Loading " + modelID
	})

	path, err := r.ModelPath(ctx, modelID)
	if err != nil {
		return fmt.Errorf("model %q: %w", modelID, err)
	}

	// Free VRAM/RAM between models for fairer comparisons.
	if r.ListRunning != nil && r.StopModel != nil {
		if running, err := r.ListRunning(ctx); err == nil {
			for _, m := range running {
				if m.ModelID != modelID {
					_ = r.StopModel(ctx, m.ID)
				}
			}
		}
	}

	loadStart := time.Now()
	running, err := r.StartModel(ctx, modelID, path)
	if err != nil {
		return fmt.Errorf("start %q: %w", modelID, err)
	}
	loadMs := time.Since(loadStart).Seconds() * 1000
	// Every sample says where the model ran (#317).
	backend, device := "", ""
	if acc := running.Acceleration; acc != nil {
		backend, device = acc.Backend, strings.Join(acc.Devices, ", ")
	}

	for _, workload := range workloads {
		for _, prompt := range workload.Prompts {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.patch(job.ID, func(j *contracts.BenchmarkJob) {
				j.Progress.CurrentWorkload = workload.ID
				j.Progress.CurrentPrompt = prompt.ID
				j.Progress.Phase = "warmup"
				j.Progress.Message = fmt.Sprintf("Warm-up %s / %s", modelID, prompt.Label)
			})
			warmup, err := r.runOnce(ctx, running.Endpoint, modelID, workload.ID, prompt.ID, 0, true, loadMs)
			if err != nil {
				warmup.Error = err.Error()
			}
			warmup.Backend, warmup.Device = backend, device
			r.appendSample(job.ID, warmup)

			for i := 1; i <= job.Request.Runs; i++ {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				r.patch(job.ID, func(j *contracts.BenchmarkJob) {
					j.Progress.Phase = "measuring"
					j.Progress.Message = fmt.Sprintf("Measuring %s / %s (run %d/%d)", modelID, prompt.Label, i, job.Request.Runs)
				})
				sample, err := r.runOnce(ctx, running.Endpoint, modelID, workload.ID, prompt.ID, i, false, loadMs)
				if err != nil {
					sample.Error = err.Error()
				}
				sample.Backend, sample.Device = backend, device
				r.appendSample(job.ID, sample)
			}
		}
	}
	return nil
}

func (r *Runner) runOnce(
	ctx context.Context,
	endpoint, modelID, workloadID, promptID string,
	runIndex int,
	warmup bool,
	loadMs float64,
) (contracts.BenchmarkSample, error) {
	sample := contracts.BenchmarkSample{
		ModelID:    modelID,
		WorkloadID: workloadID,
		PromptID:   promptID,
		RunIndex:   runIndex,
		Warmup:     warmup,
		LoadMs:     loadMs,
	}
	w, ok := WorkloadByID(workloadID)
	if !ok {
		return sample, fmt.Errorf("workload missing")
	}
	var text string
	for _, p := range w.Prompts {
		if p.ID == promptID {
			text = p.Text
			break
		}
	}
	if text == "" {
		return sample, fmt.Errorf("prompt missing")
	}

	ch, err := r.Chat(ctx, pluginapi.ChatRequest{
		ModelEndpoint: endpoint,
		Messages:      []pluginapi.ChatMessage{{Role: "user", Content: text}},
		Stream:        true,
		MaxTokens:     192,
		Temperature:   0.2,
	})
	if err != nil {
		return sample, err
	}
	var metrics *pluginapi.GenerationMetrics
	for chunk := range ch {
		if chunk.Error != "" {
			return sample, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
	}
	if metrics != nil {
		sample.TTFTMs = metrics.TTFTMs
		sample.PromptMs = metrics.PromptMs
		sample.EvalMs = metrics.EvalMs
		sample.TotalMs = metrics.TotalMs
		sample.PromptTokPerSec = metrics.PromptTokPerSec
		sample.EvalTokPerSec = metrics.EvalTokPerSec
		sample.PromptTokens = metrics.PromptTokens
		sample.CompletionTokens = metrics.CompletionTokens
	}
	return sample, nil
}

func (r *Runner) appendSample(jobID string, sample contracts.BenchmarkSample) {
	r.patch(jobID, func(j *contracts.BenchmarkJob) {
		j.Samples = append(j.Samples, sample)
		j.Summaries = summarize(j.Samples)
		j.Winners = pickWinners(j.Summaries)
		j.Progress.CompletedSteps++
		if j.Progress.TotalSteps > 0 {
			j.Progress.Percent = (j.Progress.CompletedSteps * 100) / j.Progress.TotalSteps
			if j.Progress.Percent > 99 && j.Status == contracts.BenchmarkRunning {
				j.Progress.Percent = 99
			}
		}
	})
}

func (r *Runner) failOrCancel(jobID string, err error) {
	r.patch(jobID, func(j *contracts.BenchmarkJob) {
		done := time.Now().UTC()
		j.CompletedAt = &done
		j.Summaries = summarize(j.Samples)
		j.Winners = pickWinners(j.Summaries)
		if err == context.Canceled {
			j.Status = contracts.BenchmarkCancelled
			j.Progress.Phase = "cancelled"
			j.Progress.Message = "Cancelled"
			return
		}
		j.Status = contracts.BenchmarkFailed
		j.Error = err.Error()
		j.Progress.Phase = "failed"
		j.Progress.Message = err.Error()
	})
}

func (r *Runner) patch(id string, fn func(*contracts.BenchmarkJob)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[id]; ok {
		fn(job)
	}
}

func cloneJob(j *contracts.BenchmarkJob) *contracts.BenchmarkJob {
	cp := *j
	cp.Samples = append([]contracts.BenchmarkSample(nil), j.Samples...)
	cp.Summaries = append([]contracts.BenchmarkModelSummary(nil), j.Summaries...)
	cp.Winners = map[string]string{}
	for k, v := range j.Winners {
		cp.Winners[k] = v
	}
	cp.Request.ModelIDs = append([]string(nil), j.Request.ModelIDs...)
	cp.Request.WorkloadIDs = append([]string(nil), j.Request.WorkloadIDs...)
	if j.StartedAt != nil {
		t := *j.StartedAt
		cp.StartedAt = &t
	}
	if j.CompletedAt != nil {
		t := *j.CompletedAt
		cp.CompletedAt = &t
	}
	return &cp
}

func summarize(samples []contracts.BenchmarkSample) []contracts.BenchmarkModelSummary {
	type key struct{ model, workload string }
	type agg struct {
		n                         int
		ttft, prompt, eval, total float64
		pTps, eTps, load          float64
	}
	acc := map[key]*agg{}
	for _, s := range samples {
		if s.Warmup || s.Error != "" {
			continue
		}
		k := key{s.ModelID, s.WorkloadID}
		a := acc[k]
		if a == nil {
			a = &agg{}
			acc[k] = a
		}
		a.n++
		a.ttft += s.TTFTMs
		a.prompt += s.PromptMs
		a.eval += s.EvalMs
		a.total += s.TotalMs
		a.pTps += s.PromptTokPerSec
		a.eTps += s.EvalTokPerSec
		if s.LoadMs > a.load {
			a.load = s.LoadMs
		}
	}
	out := make([]contracts.BenchmarkModelSummary, 0, len(acc))
	for k, a := range acc {
		if a.n == 0 {
			continue
		}
		n := float64(a.n)
		avgEval := a.eTps / n
		avgTtft := a.ttft / n
		// Prefer high decode speed; lightly penalize slow first token.
		score := avgEval
		if avgTtft > 0 {
			score = avgEval - (avgTtft / 5000)
		}
		out = append(out, contracts.BenchmarkModelSummary{
			ModelID:            k.model,
			WorkloadID:         k.workload,
			Samples:            a.n,
			AvgTTFTMs:          avgTtft,
			AvgPromptMs:        a.prompt / n,
			AvgEvalMs:          a.eval / n,
			AvgTotalMs:         a.total / n,
			AvgPromptTokPerSec: a.pTps / n,
			AvgEvalTokPerSec:   avgEval,
			LoadMs:             a.load,
			WinnerScore:        score,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WorkloadID != out[j].WorkloadID {
			return out[i].WorkloadID < out[j].WorkloadID
		}
		return out[i].WinnerScore > out[j].WinnerScore
	})
	return out
}

func pickWinners(summaries []contracts.BenchmarkModelSummary) map[string]string {
	winners := map[string]string{}
	best := map[string]float64{}
	for _, s := range summaries {
		if cur, ok := best[s.WorkloadID]; !ok || s.WinnerScore > cur {
			best[s.WorkloadID] = s.WinnerScore
			winners[s.WorkloadID] = s.ModelID
		}
	}
	return winners
}
