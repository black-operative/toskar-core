// Package health tracks a running model and stops it when the runtime
// dies or stops answering. It does not restart the model.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	StatusHealthy      = "healthy"
	StatusDegraded     = "degraded"
	StatusUnresponsive = "unresponsive"
	StatusStopped      = "stopped"
	StatusFailed       = "failed"
	StatusRecovering   = "recovering"
)

const (
	ReasonProcessExit       = "process_exit"
	ReasonHealthProbeFailed = "health_probe_failed"
	ReasonGenerationStalled = "generation_stalled"
	ReasonRuntimeError      = "runtime_error"
	ReasonOOM               = "oom"
	ReasonUnknown           = "unknown"
)

const (
	EventDegraded         = "model.health.degraded"
	EventFailed           = "model.health.failed"
	EventCleanupStarted   = "model.cleanup.started"
	EventCleanupCompleted = "model.cleanup.completed"
	EventCleanupFailed    = "model.cleanup.failed"
)

// Settings are the MVP thresholds. Tests can shorten them.
type Settings struct {
	ProbeInterval      time.Duration
	ProbeTimeout       time.Duration
	ProbeFailures      int
	NoProgressTimeout  time.Duration
	FirstTokenTimeout  time.Duration
	GracefulStopWait   time.Duration
	ForceStopWait      time.Duration
	FailureWindow      time.Duration
	FailuresBeforeFlag int
}

// DefaultSettings matches the product defaults.
func DefaultSettings() Settings {
	return Settings{
		ProbeInterval:      5 * time.Second,
		ProbeTimeout:       2500 * time.Millisecond,
		ProbeFailures:      3,
		NoProgressTimeout:  30 * time.Second,
		FirstTokenTimeout:  60 * time.Second,
		GracefulStopWait:   4 * time.Second,
		ForceStopWait:      3 * time.Second,
		FailureWindow:      10 * time.Minute,
		FailuresBeforeFlag: 2,
	}
}

// Instance identifies one loaded model process.
type Instance struct {
	ModelID        string
	RunningModelID string
	NodeID         string
	RuntimeID      string
	Endpoint       string
}

// Failure is the user-facing result of a dead model. Stderr is for advanced details only.
type Failure struct {
	Kind                 string `json:"kind"`
	Reason               string `json:"reason"`
	Message              string `json:"message"`
	LikelyMemoryPressure bool   `json:"likely_memory_pressure,omitempty"`
	Interrupted          bool   `json:"interrupted,omitempty"`
	ModelID              string `json:"model_id,omitempty"`
	RunningModelID       string `json:"running_model_id,omitempty"`
	NodeID               string `json:"node_id,omitempty"`
	RuntimeID            string `json:"runtime,omitempty"`
	ExitCode             *int   `json:"exit_code,omitempty"`
	LastProbe            string `json:"last_probe,omitempty"`
	StderrTail           string `json:"stderr_tail,omitempty"`
}

func (f Failure) Error() string { return f.Message }

// Encode turns a failure into the chat error payload.
func Encode(f Failure) string {
	if f.Kind == "" {
		f.Kind = "model_health"
	}
	if f.Message == "" {
		f.Message = UserMessage(f.LikelyMemoryPressure)
	}
	b, err := json.Marshal(f)
	if err != nil {
		return f.Message
	}
	return string(b)
}

// Parse reports whether raw is a model-health failure.
func Parse(raw string) (Failure, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return Failure{}, false
	}
	var f Failure
	if err := json.Unmarshal([]byte(raw), &f); err != nil || f.Kind != "model_health" {
		return Failure{}, false
	}
	return f, true
}

// UserMessage is the normal-mode sentence for a stopped model.
func UserMessage(memory bool) string {
	if memory {
		return "This model likely ran out of safe memory on this computer. Toskar stopped it to keep the system stable."
	}
	return "The model stopped responding, so Toskar stopped it and cleaned up the failed process."
}

// Stopper unloads one model instance. Force is used only after Graceful fails.
type Stopper interface {
	Graceful(ctx context.Context, inst Instance) error
	Force(ctx context.Context, inst Instance) error
	Alive(inst Instance) bool
}

// Event is a structured health or cleanup notice.
type Event struct {
	Type    string
	Payload map[string]any
}

// Monitor supervises running model instances.
type Monitor struct {
	Settings Settings
	Probe    func(ctx context.Context, endpoint string) error
	Stopper  Stopper
	Memory   func(ctx context.Context) bool
	Publish  func(Event)
	Log      *slog.Logger
	Now      func() time.Time

	mu        sync.Mutex
	instances map[string]*tracked
	failures  map[string][]time.Time
}

type tracked struct {
	Instance
	status         string
	probeFailures  int
	lastProbeAt    time.Time
	lastProbeErr   string
	lastProgressAt time.Time
	generating     bool
	sawProgress    bool
	accepting      bool
	intentional    bool
	exitCode       *int
	stderrTail     string
	reason         string
	memory         bool
	wake           chan struct{}
	cancelGen      context.CancelFunc
	cleaned        bool
}

// NewMonitor builds a monitor. Probe and Stopper may be nil until wired.
func NewMonitor(settings Settings) *Monitor {
	if settings.ProbeFailures <= 0 {
		settings = DefaultSettings()
	}
	return &Monitor{
		Settings:  settings,
		Now:       time.Now,
		instances: map[string]*tracked{},
		failures:  map[string][]time.Time{},
		Log:       slog.Default(),
	}
}

// Register starts tracking a model the daemon expects to stay up.
func (m *Monitor) Register(inst Instance) {
	if inst.RunningModelID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.instances[inst.RunningModelID]; ok && cur.accepting && !cur.cleaned {
		cur.Instance = inst
		return
	}
	now := m.now()
	m.instances[inst.RunningModelID] = &tracked{
		Instance:       inst,
		status:         StatusHealthy,
		lastProgressAt: now,
		accepting:      true,
		wake:           make(chan struct{}, 1),
	}
}

// MarkIntentional tells the watcher that this stop was requested.
func (m *Monitor) MarkIntentional(runningID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.instances[runningID]; ok {
		t.intentional = true
		t.accepting = false
	}
}

// BeginGeneration arms the no-progress watchdog for one request.
func (m *Monitor) BeginGeneration(runningID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.instances[runningID]
	if !ok {
		return ctx, cancel
	}
	t.generating = true
	t.sawProgress = false
	t.lastProgressAt = m.now()
	if t.cancelGen != nil {
		t.cancelGen()
	}
	t.cancelGen = cancel
	return ctx, cancel
}

// EndGeneration clears the watchdog after a request finishes normally.
func (m *Monitor) EndGeneration(runningID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.instances[runningID]; ok {
		t.generating = false
		t.cancelGen = nil
	}
}

// Generating lists the models this monitor watches that are answering a
// request now, by model ID.
func (m *Monitor) Generating() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for _, t := range m.instances {
		if t.generating && t.ModelID != "" {
			out[t.ModelID] = true
		}
	}
	return out
}

// NoteProgress records a token or other inference activity.
func (m *Monitor) NoteProgress(runningID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.instances[runningID]; ok {
		t.lastProgressAt = m.now()
		t.sawProgress = true
	}
}

// ByEndpoint finds the instance serving this URL.
func (m *Monitor) ByEndpoint(endpoint string) (Instance, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	endpoint = strings.TrimRight(endpoint, "/")
	for _, t := range m.instances {
		if strings.TrimRight(t.Endpoint, "/") == endpoint && t.accepting && !t.cleaned {
			return t.Instance, true
		}
	}
	return Instance{}, false
}

// Accepting reports whether new work may be sent to this instance.
func (m *Monitor) Accepting(runningID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.instances[runningID]
	return ok && t.accepting && !t.cleaned && t.status != StatusFailed && t.status != StatusUnresponsive && t.status != StatusStopped
}

// Status returns the current health label.
func (m *Monitor) Status(runningID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.instances[runningID]
	if !ok {
		return ""
	}
	return t.status
}

// Wake is closed-equivalent: it fires when the instance becomes terminal.
func (m *Monitor) Wake(runningID string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.instances[runningID]
	if !ok {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return t.wake
}

// ReportProcessExit records an unexpected process death. Intentional stops are ignored.
func (m *Monitor) ReportProcessExit(runningID string, exitCode int, stderrTail string) {
	m.mu.Lock()
	t, ok := m.instances[runningID]
	if !ok || t.intentional || t.cleaned {
		m.mu.Unlock()
		return
	}
	code := exitCode
	t.exitCode = &code
	t.stderrTail = stderrTail
	reason := ReasonProcessExit
	memory := m.memoryLocked() || looksLikeOOM(stderrTail)
	if memory {
		reason = ReasonOOM
	}
	t.reason = reason
	t.memory = memory
	m.mu.Unlock()
	m.fail(runningID, reason, memory)
}

// ReportRuntimeError records a fatal runtime or out-of-memory response.
func (m *Monitor) ReportRuntimeError(runningID, raw string) bool {
	if !looksFatal(raw) {
		return false
	}
	memory := looksLikeOOM(raw)
	reason := ReasonRuntimeError
	if memory {
		reason = ReasonOOM
	}
	m.mu.Lock()
	if t, ok := m.instances[runningID]; ok {
		t.stderrTail = clip(raw, 800)
		t.memory = t.memory || memory || m.memoryLocked()
		memory = t.memory
	} else {
		m.mu.Unlock()
		return false
	}
	m.mu.Unlock()
	m.fail(runningID, reason, memory)
	return true
}

// ProbeOnce runs one health check. One failure degrades; repeated failures declare the model unresponsive.
func (m *Monitor) ProbeOnce(runningID string) {
	m.mu.Lock()
	t, ok := m.instances[runningID]
	if !ok || !t.accepting || t.cleaned || t.intentional {
		m.mu.Unlock()
		return
	}
	endpoint := t.Endpoint
	m.mu.Unlock()

	err := m.probe(endpoint)
	m.mu.Lock()
	t, ok = m.instances[runningID]
	if !ok || t.cleaned || t.intentional {
		m.mu.Unlock()
		return
	}
	t.lastProbeAt = m.now()
	if err == nil {
		t.probeFailures = 0
		t.lastProbeErr = ""
		if t.status == StatusDegraded {
			t.status = StatusHealthy
		}
		m.mu.Unlock()
		return
	}
	t.probeFailures++
	t.lastProbeErr = err.Error()
	count := t.probeFailures
	limit := m.Settings.ProbeFailures
	if t.status == StatusHealthy {
		t.status = StatusDegraded
		m.publishLocked(Event{Type: EventDegraded, Payload: m.payloadLocked(t, ReasonHealthProbeFailed)})
	}
	m.log("probe failed", "model_id", t.ModelID, "node_id", t.NodeID, "failures", count, "error", err.Error())
	m.mu.Unlock()
	if count >= limit {
		m.fail(runningID, ReasonHealthProbeFailed, false)
	}
}

// CheckGeneration probes when an active request has made no progress. A healthy runtime is left alone.
func (m *Monitor) CheckGeneration(runningID string) (Failure, bool) {
	m.mu.Lock()
	t, ok := m.instances[runningID]
	if !ok || !t.generating || t.cleaned {
		failed := ok && (t.status == StatusFailed || t.status == StatusUnresponsive || t.status == StatusStopped) && t.generating
		var fail Failure
		if failed {
			fail = m.failureLocked(t)
		}
		m.mu.Unlock()
		if failed {
			return fail, true
		}
		return Failure{}, false
	}
	if terminal(t.status) {
		fail := m.failureLocked(t)
		m.mu.Unlock()
		return fail, true
	}
	limit := m.Settings.NoProgressTimeout
	if !t.sawProgress {
		limit = m.Settings.FirstTokenTimeout
	}
	stalled := m.now().Sub(t.lastProgressAt) > limit
	endpoint := t.Endpoint
	m.mu.Unlock()
	if !stalled {
		return Failure{}, false
	}
	m.log("generation made no progress", "running_model_id", runningID)
	if err := m.probe(endpoint); err == nil {
		return Failure{}, false
	}
	m.mu.Lock()
	if t, ok = m.instances[runningID]; ok {
		t.lastProbeErr = "no progress and health probe failed"
	}
	m.mu.Unlock()
	m.fail(runningID, ReasonGenerationStalled, false)
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok = m.instances[runningID]
	if !ok {
		return Failure{}, false
	}
	return m.failureLocked(t), true
}

// FailureOf returns the recorded failure when the instance is already terminal.
func (m *Monitor) FailureOf(runningID string) (Failure, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.instances[runningID]
	if !ok || !terminal(t.status) {
		return Failure{}, false
	}
	return m.failureLocked(t), true
}

// UnstableNodes lists computers where this model failed repeatedly in the current window.
func (m *Monitor) UnstableNodes(modelID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	seen := map[string]struct{}{}
	for key, times := range m.failures {
		model, node, ok := splitKey(key)
		if !ok || model != modelID {
			continue
		}
		if m.countRecentLocked(times) >= m.Settings.FailuresBeforeFlag {
			if _, dup := seen[node]; dup {
				continue
			}
			seen[node] = struct{}{}
			out = append(out, node)
		}
	}
	return out
}

// Run probes registered instances until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	interval := m.Settings.ProbeInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.probeAll()
		}
	}
}

func (m *Monitor) probeAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.instances))
	for id, t := range m.instances {
		if t.accepting && !t.cleaned && !t.intentional {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.ProbeOnce(id)
	}
}

func (m *Monitor) fail(runningID, reason string, memoryHint bool) {
	m.mu.Lock()
	t, ok := m.instances[runningID]
	if !ok || t.cleaned || t.intentional {
		m.mu.Unlock()
		return
	}
	if memoryHint || m.memoryLocked() {
		t.memory = true
		if reason == ReasonProcessExit || reason == ReasonUnknown || reason == ReasonGenerationStalled || reason == ReasonHealthProbeFailed {
			if t.memory && (reason == ReasonProcessExit || looksLikeOOM(t.stderrTail)) {
				reason = ReasonOOM
			}
		}
	}
	if reason == ReasonOOM {
		t.memory = true
	}
	t.reason = reason
	if t.status != StatusFailed {
		t.status = StatusUnresponsive
		if reason == ReasonProcessExit || reason == ReasonOOM || reason == ReasonRuntimeError {
			t.status = StatusFailed
		}
	}
	t.accepting = false
	payload := m.payloadLocked(t, reason)
	m.publishLocked(Event{Type: EventFailed, Payload: payload})
	m.log("model failed", "model_id", t.ModelID, "node_id", t.NodeID, "reason", reason, "exit_code", t.exitCode)
	if t.cancelGen != nil {
		t.cancelGen()
	}
	select {
	case t.wake <- struct{}{}:
	default:
	}
	m.mu.Unlock()
	m.cleanup(runningID)
}

func (m *Monitor) cleanup(runningID string) {
	m.mu.Lock()
	t, ok := m.instances[runningID]
	if !ok || t.cleaned {
		m.mu.Unlock()
		return
	}
	t.status = StatusRecovering
	t.accepting = false
	inst := t.Instance
	m.publishLocked(Event{Type: EventCleanupStarted, Payload: m.payloadLocked(t, t.reason)})
	m.log("cleanup started", "model_id", inst.ModelID, "running_model_id", inst.RunningModelID, "node_id", inst.NodeID)
	m.mu.Unlock()

	if m.Stopper != nil {
		ctx, cancel := context.WithTimeout(context.Background(), m.Settings.GracefulStopWait)
		gracefulErr := m.Stopper.Graceful(ctx, inst)
		cancel()
		if m.Stopper.Alive(inst) {
			m.log("graceful stop failed", "model_id", inst.ModelID, "error", errString(gracefulErr))
			ctx, cancel = context.WithTimeout(context.Background(), m.Settings.ForceStopWait)
			err := m.Stopper.Force(ctx, inst)
			cancel()
			if err != nil || m.Stopper.Alive(inst) {
				m.log("forced kill failed", "model_id", inst.ModelID, "error", errString(err))
				m.mu.Lock()
				if cur, ok := m.instances[runningID]; ok {
					m.publishLocked(Event{Type: EventCleanupFailed, Payload: m.payloadLocked(cur, cur.reason)})
				}
				m.mu.Unlock()
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok = m.instances[runningID]
	if !ok {
		return
	}
	t.cleaned = true
	t.accepting = false
	t.generating = false
	t.status = StatusStopped
	m.recordFailureLocked(t.ModelID, t.NodeID)
	m.publishLocked(Event{Type: EventCleanupCompleted, Payload: m.payloadLocked(t, t.reason)})
	m.log("cleanup completed", "model_id", t.ModelID, "node_id", t.NodeID, "reason", t.reason)
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (m *Monitor) recordFailureLocked(modelID, nodeID string) {
	if modelID == "" {
		return
	}
	key := modelID + "\x00" + nodeID
	now := m.now()
	recent := m.failures[key]
	cutoff := now.Add(-m.Settings.FailureWindow)
	kept := recent[:0]
	for _, ts := range recent {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	m.failures[key] = kept
	if m.countRecentLocked(kept) >= m.Settings.FailuresBeforeFlag {
		m.log("model unstable on node", "model_id", modelID, "node_id", nodeID, "failures", len(kept))
	}
}

func (m *Monitor) countRecentLocked(times []time.Time) int {
	cutoff := m.now().Add(-m.Settings.FailureWindow)
	n := 0
	for _, ts := range times {
		if !ts.Before(cutoff) {
			n++
		}
	}
	return n
}

func (m *Monitor) probe(endpoint string) error {
	if m.Probe == nil {
		return nil
	}
	timeout := m.Settings.ProbeTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return m.Probe(ctx, endpoint)
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) memoryLocked() bool {
	if m.Memory == nil {
		return false
	}
	return m.Memory(context.Background())
}

func (m *Monitor) publishLocked(evt Event) {
	if m.Publish != nil {
		m.Publish(evt)
	}
}

func (m *Monitor) log(msg string, args ...any) {
	if m.Log != nil {
		m.Log.Info(msg, args...)
	}
}

func (m *Monitor) payloadLocked(t *tracked, reason string) map[string]any {
	payload := map[string]any{
		"model_id":               t.ModelID,
		"running_model_id":       t.RunningModelID,
		"node_id":                t.NodeID,
		"reason":                 reason,
		"timestamp":              m.now().UTC().Format(time.RFC3339),
		"likely_memory_pressure": t.memory,
	}
	if t.exitCode != nil {
		payload["exit_code"] = *t.exitCode
	}
	return payload
}

func (m *Monitor) failureLocked(t *tracked) Failure {
	return Failure{
		Kind:                 "model_health",
		Reason:               t.reason,
		Message:              UserMessage(t.memory),
		LikelyMemoryPressure: t.memory,
		Interrupted:          t.sawProgress,
		ModelID:              t.ModelID,
		RunningModelID:       t.RunningModelID,
		NodeID:               t.NodeID,
		RuntimeID:            t.RuntimeID,
		ExitCode:             t.exitCode,
		LastProbe:            t.lastProbeErr,
		StderrTail:           t.stderrTail,
	}
}

func terminal(status string) bool {
	switch status {
	case StatusFailed, StatusUnresponsive, StatusStopped:
		return true
	default:
		return false
	}
}

func looksLikeOOM(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"out of memory",
		"cannot allocate",
		"failed to allocate",
		"insufficient memory",
		"compute error",
		"oom",
		"kiogpucommandbuffercallbackerroroutofmemory",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func looksFatal(text string) bool {
	if looksLikeOOM(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{"ggml_assert", "fatal error", "cuda error"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func splitKey(key string) (string, string, bool) {
	model, node, ok := strings.Cut(key, "\x00")
	return model, node, ok
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// OutOfMemory reports whether an error says a model ran out of memory: a
// health failure with memory pressure, or a runtime or system message.
func OutOfMemory(text string) bool {
	if f, ok := Parse(text); ok {
		return f.LikelyMemoryPressure
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"out of memory", "cannot allocate", "failed to allocate", "insufficient memory",
		"not enough memory", "oom killed", "(oom)", "kiogpucommandbuffercallbackerroroutofmemory",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// WithMessage replaces the sentence shown for an encoded health failure, or
// returns message alone for any other error.
func WithMessage(raw, message string) string {
	if f, ok := Parse(raw); ok {
		f.Message = message
		return Encode(f)
	}
	return message
}
