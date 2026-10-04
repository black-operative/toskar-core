package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/runlog"
	"time"

	"github.com/yeixio/toskar-core/internal/tools/filesystem"
	"github.com/yeixio/toskar-core/internal/tools/git"
	"github.com/yeixio/toskar-core/internal/tools/internet"
	"github.com/yeixio/toskar-core/internal/tools/terminal"
)

// PendingCall awaits user decision.
type PendingCall struct {
	ID       string
	ToolID   string
	Args     map[string]any
	Reason   string
	Response chan Decision
}

// Decision is the user's tool permission choice.
type Decision struct {
	Allow        bool
	AllowSession bool
}

// Registry manages tools and pending permission prompts.
type Registry struct {
	tools    map[string]Tool
	policy   *PolicyEngine
	bus      *events.Bus
	pending  map[string]*PendingCall
	disabled map[string]struct{}
	activity []Activity
	// observe, when set, hears each call just before it runs, such as to
	// record what leaves this computer (§63).
	observe func(ctx context.Context, toolID string, args map[string]any)
	// cache serves repeats of safe lookups (§36).
	cache ToolCache
	// auditLog records every call and what became of it (Gungnir §13).
	auditLog *AuditLog
	mu       sync.Mutex
}

// SetAudit records every call in log.
func (r *Registry) SetAudit(log *AuditLog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auditLog = log
}

// audit records what became of a call.
func (r *Registry) audit(ctx context.Context, toolID, status, approval, summary string, started time.Time, err error, meta map[string]any) {
	r.mu.Lock()
	log := r.auditLog
	r.mu.Unlock()
	if log == nil {
		return
	}
	run := ToolRun{ToolID: toolID, Status: status, Approval: approval, Summary: summary, Source: egress.RunFrom(ctx).Source}
	if !started.IsZero() {
		run.DurationMS = time.Since(started).Milliseconds()
	}
	if err != nil {
		run.Error = err.Error()
	}
	run.ConversationID, _ = meta["conversation_id"].(string)
	run.TaskID, _ = meta["task_id"].(string)
	log.Record(ctx, run)
}

// ToolCache serves repeat calls of safe lookups, such as a web search made
// a minute ago (spec §36). It decides which tools and arguments it keeps.
type ToolCache interface {
	Lookup(toolID string, args map[string]any) (map[string]any, bool)
	Store(toolID string, args map[string]any, result map[string]any)
}

// SetCache sets the cache that serves repeat calls.
func (r *Registry) SetCache(c ToolCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = c
}

// SetObserver sets a function that hears each call just before it runs.
func (r *Registry) SetObserver(f func(ctx context.Context, toolID string, args map[string]any)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observe = f
}

// Activity is a short diagnostics record. It does not include file contents.
type Activity struct {
	ToolID     string    `json:"tool_id"`
	Status     string    `json:"status"`
	Summary    string    `json:"summary,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
	At         time.Time `json:"at"`
}

// NewRegistry registers built-in tools.
func NewRegistry(workspace string, bus *events.Bus) *Registry {
	r := &Registry{
		tools:    make(map[string]Tool),
		policy:   NewPolicyEngine(),
		bus:      bus,
		pending:  make(map[string]*PendingCall),
		disabled: map[string]struct{}{},
	}
	for _, t := range []Tool{
		internet.NewSearch(nil),
		internet.NewOpen(nil),
		filesystem.NewSearch(workspace),
		filesystem.NewRead(workspace),
		filesystem.NewWrite(workspace),
		terminal.New(),
		git.NewStatus(workspace),
		git.NewDiff(workspace),
		git.NewLog(workspace),
		git.NewShow(workspace),
		git.NewAdd(workspace),
		git.NewCommit(workspace),
		git.NewPush(workspace),
	} {
		r.tools[t.ID()] = t
	}
	return r
}

// Register adds a tool that needs app services, such as files.create.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.ID()] = t
}

// Unregister removes a tool, such as one of a disconnected service.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, id)
}

func (r *Registry) List() []Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) Get(id string) (Tool, error) {
	r.mu.Lock()
	t, ok := r.tools[Canonical(id)]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", id)
	}
	return t, nil
}

// Execute runs a tool respecting policy; may block on pending approval.
// meta is merged into tool.* event payloads (e.g. conversation_id, task_id).
func (r *Registry) Execute(ctx context.Context, toolID string, args map[string]any, policy string, reason string, meta map[string]any) (map[string]any, error) {
	if f := formatFor(toolID); f != "" {
		// pdf.create and the like are files.create in that format.
		withFormat := make(map[string]any, len(args)+1)
		for k, v := range args {
			withFormat[k] = v
		}
		withFormat["format"] = f
		args = withFormat
	}
	toolID = Canonical(toolID)
	t, err := r.Get(toolID)
	if err != nil {
		return nil, err
	}
	if r.IsDisabled(toolID) {
		err := fmt.Errorf("tool %q is disabled", toolID)
		r.record(Activity{ToolID: toolID, Status: "disabled", Error: err.Error(), At: time.Now()})
		r.audit(ctx, toolID, RunDisabled, "", activitySummary(args), time.Time{}, err, meta)
		return nil, err
	}
	if def, ok := Lookup(toolID); ok {
		var argErr error
		if args, argErr = CheckArgs(def, args); argErr != nil {
			r.record(Activity{ToolID: toolID, Status: "malformed", Summary: activitySummary(args), Error: argErr.Error(), At: time.Now()})
			r.bus.Publish(events.New(events.ToolFailed, mergeMeta(meta, map[string]any{
				"tool_id": toolID, "error": argErr.Error(), "malformed": true, "kind": ErrKindInvalid,
			})))
			r.audit(ctx, toolID, RunRefused, "", activitySummary(args), time.Time{}, argErr, meta)
			return nil, argErr
		}
	}
	if err := implausibleCall(toolID, args); err != nil {
		r.record(Activity{ToolID: toolID, Status: "malformed", Summary: activitySummary(args), Error: err.Error(), At: time.Now()})
		r.bus.Publish(events.New(events.ToolFailed, mergeMeta(meta, map[string]any{
			"tool_id": toolID, "error": err.Error(), "malformed": true,
		})))
		r.audit(ctx, toolID, RunRefused, "", activitySummary(args), time.Time{}, err, meta)
		return nil, err
	}
	allowed, needsPrompt, err := r.policy.Decide(toolID, policy)
	if err != nil {
		r.audit(ctx, toolID, RunDenied, "", activitySummary(args), time.Time{}, err, meta)
		return nil, err
	}
	approval := ApprovedByProfile
	if needsPrompt {
		decision, err := r.requestApproval(ctx, toolID, args, reason, meta)
		if err != nil {
			r.audit(ctx, toolID, RunDenied, "", activitySummary(args), time.Time{}, err, meta)
			return nil, err
		}
		if !decision.Allow {
			err := fmt.Errorf("tool %q denied by user", toolID)
			r.audit(ctx, toolID, RunDenied, ApprovedByYou, activitySummary(args), time.Time{}, err, meta)
			return nil, err
		}
		approval = ApprovedByYou
		if decision.AllowSession {
			r.policy.AllowSession(toolID)
			approval = ApprovedSession
		}
	} else if !allowed {
		err := fmt.Errorf("tool %q not allowed", toolID)
		r.audit(ctx, toolID, RunDenied, "", activitySummary(args), time.Time{}, err, meta)
		return nil, err
	}

	summary := activitySummary(args)
	started := time.Now()
	r.mu.Lock()
	cache := r.cache
	r.mu.Unlock()
	if cache != nil {
		// A repeat is answered from the cache: the tool does not run, and
		// nothing leaves this computer.
		if result, ok := cache.Lookup(toolID, args); ok {
			r.record(Activity{ToolID: toolID, Status: "cached", Summary: summary, At: started})
			r.bus.Publish(events.New(events.ToolCompleted, mergeMeta(meta, map[string]any{
				"tool_id": toolID, "duration_ms": int64(0), "summary": summary, "cached": true,
			})))
			runlog.From(ctx).CacheHit(toolID)
			r.audit(ctx, toolID, RunCached, approval, summary, started, nil, meta)
			return result, nil
		}
	}
	r.record(Activity{ToolID: toolID, Status: "started", Summary: summary, At: started})
	r.bus.Publish(events.New(events.ToolStarted, mergeMeta(meta, map[string]any{"tool_id": toolID, "summary": summary})))
	// Every call has a time limit; cancelling the turn stops it sooner.
	callCtx, cancel := context.WithTimeout(ctx, Timeout(toolID))
	r.mu.Lock()
	observe := r.observe
	r.mu.Unlock()
	if observe != nil {
		observe(ctx, toolID, args)
	}
	result, err := t.Execute(callCtx, args)
	cancel()
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		r.record(Activity{ToolID: toolID, Status: "failed", Summary: summary, DurationMS: elapsed, Error: err.Error(), At: time.Now()})
		r.bus.Publish(events.New(events.ToolFailed, mergeMeta(meta, map[string]any{
			"tool_id": toolID, "error": err.Error(), "kind": ErrorKind(err), "duration_ms": elapsed, "summary": summary,
		})))
		r.audit(ctx, toolID, RunFailed, approval, summary, started, err, meta)
		return nil, err
	}
	if cache != nil {
		cache.Store(toolID, args, result)
	}
	r.record(Activity{ToolID: toolID, Status: "completed", Summary: summary, DurationMS: elapsed, At: time.Now()})
	r.bus.Publish(events.New(events.ToolCompleted, mergeMeta(meta, map[string]any{
		"tool_id": toolID, "duration_ms": elapsed, "summary": summary,
	})))
	r.audit(ctx, toolID, RunCompleted, approval, summary, started, nil, meta)
	return result, nil
}

func activitySummary(args map[string]any) string {
	for _, key := range []string{"query", "url", "path", "command"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			value = strings.TrimSpace(value)
			if len(value) > 160 {
				value = value[:160] + "…"
			}
			return value
		}
	}
	return ""
}

func (r *Registry) IsDisabled(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.disabled[id]
	return ok
}

func (r *Registry) SetEnabled(id string, enabled bool) error {
	if _, ok := Lookup(id); !ok {
		return fmt.Errorf("tool %q not found", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if enabled {
		delete(r.disabled, id)
	} else {
		r.disabled[id] = struct{}{}
	}
	return nil
}

func (r *Registry) Disabled() map[string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]struct{}, len(r.disabled))
	for id := range r.disabled {
		out[id] = struct{}{}
	}
	return out
}

func (r *Registry) Recent() []Activity {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Activity, len(r.activity))
	copy(out, r.activity)
	return out
}

// Note appends a diagnostics record that does not include file contents.
func (r *Registry) Note(item Activity) {
	if item.At.IsZero() {
		item.At = time.Now()
	}
	r.record(item)
}

func (r *Registry) record(item Activity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activity = append(r.activity, item)
	if len(r.activity) > 50 {
		r.activity = r.activity[len(r.activity)-50:]
	}
}

func (r *Registry) requestApproval(ctx context.Context, toolID string, args map[string]any, reason string, meta map[string]any) (Decision, error) {
	id := uuid.NewString()
	resp := make(chan Decision, 1)
	pc := &PendingCall{ID: id, ToolID: toolID, Args: args, Reason: reason, Response: resp}
	r.mu.Lock()
	r.pending[id] = pc
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}()

	payload := mergeMeta(meta, map[string]any{
		"request_id": id,
		"tool_id":    toolID,
		"args":       sanitizeArgs(args),
		"reason":     reason,
	})
	r.bus.Publish(events.New(events.ToolRequested, payload))

	select {
	case <-ctx.Done():
		return Decision{}, ctx.Err()
	case d := <-resp:
		return d, nil
	}
}

// Decide resolves a pending tool call from the API.
func (r *Registry) Decide(requestID string, allow, allowSession bool) error {
	r.mu.Lock()
	pc, ok := r.pending[requestID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("pending request %q not found", requestID)
	}
	select {
	case pc.Response <- Decision{Allow: allow, AllowSession: allowSession}:
		return nil
	default:
		return fmt.Errorf("request %q already resolved", requestID)
	}
}

func mergeMeta(meta, base map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(meta))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range meta {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// sanitizeArgs truncates long string values so prompts never dump huge payloads.
func sanitizeArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch s := v.(type) {
		case string:
			if len(s) > 500 {
				out[k] = s[:500] + "…"
			} else {
				out[k] = s
			}
		default:
			out[k] = v
		}
	}
	return out
}
