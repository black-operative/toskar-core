// Package runlog traces each orchestrated request (spec §35): the strategy,
// the models, tools, and computers it used, and how long each part took. A
// Collector rides in the request's context, so every layer can add to it.
package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Statuses.
const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusStopped   = "stopped"
)

// ModelUse is one model's part in a run.
type ModelUse struct {
	ModelID string `json:"model_id"`
	Role    string `json:"role,omitempty"`
	Node    string `json:"node,omitempty"`
	Calls   int    `json:"calls"`
	// LoadMs is time spent starting the model for this run, if it had to.
	LoadMs float64 `json:"load_ms,omitempty"`
	// FirstTokenMs is the wait for the first token of the first call,
	// including loading.
	FirstTokenMs     float64 `json:"first_token_ms,omitempty"`
	TTFTMs           float64 `json:"ttft_ms,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	TokPerSec        float64 `json:"tok_per_sec,omitempty"`
	// Backend and Device ran the model, such as vulkan and "AMD Radeon RX
	// 7900 XTX" (#317); empty when not known, such as on a paired computer.
	Backend string `json:"backend,omitempty"`
	Device  string `json:"device,omitempty"`
}

// ToolUse is one tool's part in a run.
type ToolUse struct {
	ToolID   string  `json:"tool_id"`
	Calls    int     `json:"calls"`
	Failures int     `json:"failures,omitempty"`
	TotalMs  float64 `json:"total_ms"`
}

// Run is a traced request.
type Run struct {
	ID string `json:"id"`
	// Contract is the client contract the trace is written in (§68).
	Contract       string   `json:"contract"`
	ConversationID string   `json:"conversation_id,omitempty"`
	ProfileID      string   `json:"profile_id,omitempty"`
	Source         string   `json:"source,omitempty"`
	Strategy       []string `json:"strategy"`
	Effort         string   `json:"effort,omitempty"`
	Status         string   `json:"status"`
	// Error is the English text; ErrorCode and ErrorDetails let clients
	// show it in the App language (multilingual spec §10).
	Error        string         `json:"error,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorDetails map[string]any `json:"error_details,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	LatencyMs    float64        `json:"latency_ms,omitempty"`
	// PipelineMs is the time before the first model call.
	PipelineMs float64    `json:"pipeline_ms,omitempty"`
	Models     []ModelUse `json:"models"`
	Tools      []ToolUse  `json:"tools"`
	Nodes      []string   `json:"nodes"`
	// Workers is how many parts a plan had; 0 when there was no plan.
	Workers  int  `json:"workers,omitempty"`
	Parallel bool `json:"parallel,omitempty"`
	// VerificationPasses counts answer checks; Issues and Fixed what they found.
	VerificationPasses int `json:"verification_passes"`
	VerificationIssues int `json:"verification_issues,omitempty"`
	VerificationFixed  int `json:"verification_fixed,omitempty"`
	// Retries counts answers tried again on another model.
	Retries       int `json:"retries"`
	ContextTokens int `json:"context_tokens,omitempty"`
	ContextLimit  int `json:"context_limit,omitempty"`
	// CacheHits counts tool calls answered from a cache, by tool (§36).
	CacheHits map[string]int `json:"cache_hits,omitempty"`
}

// Collector gathers a run as it happens. Its methods are safe from any
// goroutine and do nothing on a nil Collector.
type Collector struct {
	mu     sync.Mutex
	run    Run
	models map[string]*ModelUse
	tools  map[string]*ToolUse
	nodes  map[string]bool
	now    func() time.Time
	// lang is the App language notes are written in; "" is English.
	lang string
}

// New starts a run.
func New(id, conversationID, profileID, source string) *Collector {
	c := &Collector{models: map[string]*ModelUse{}, tools: map[string]*ToolUse{}, nodes: map[string]bool{}, now: time.Now}
	c.run = Run{ID: id, ConversationID: conversationID, ProfileID: profileID, Source: source, StartedAt: c.now().UTC(), Strategy: []string{}}
	return c
}

type key struct{}

// With carries a collector in ctx.
func With(ctx context.Context, c *Collector) context.Context { return context.WithValue(ctx, key{}, c) }

// From returns the collector ctx carries, or nil.
func From(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(key{}).(*Collector)
	return c
}

func (c *Collector) model(modelID, role, node string) *ModelUse {
	k := modelID + "|" + role + "|" + node
	m, ok := c.models[k]
	if !ok {
		m = &ModelUse{ModelID: modelID, Role: role, Node: node}
		c.models[k] = m
	}
	return m
}

// Strategy notes how the request was handled, such as "Looked up the web
// first". Repeats are kept once.
func (c *Collector) Strategy(note string) {
	if c == nil || note == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.run.Strategy {
		if s == note {
			return
		}
	}
	c.run.Strategy = append(c.run.Strategy, note)
}

// SetLanguage sets the App language Note writes in (multilingual spec §16).
func (c *Collector) SetLanguage(lang string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.lang = lang
	c.mu.Unlock()
}

// Language is the App language the run is written in.
func (c *Collector) Language() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lang
}

// Note adds a chat:run.notes key's text, in the App language, to the
// strategy.
func (c *Collector) Note(key string, params map[string]any) {
	if c == nil {
		return
	}
	c.Strategy(locale.T(c.Language(), "chat:run.notes."+key, params))
}

// Effort records the effort the run used.
func (c *Collector) Effort(e string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.Effort = e
	c.mu.Unlock()
}

// Loaded records time spent starting a model for this run.
func (c *Collector) Loaded(modelID string, d time.Duration) {
	if c == nil || d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The role and computer are not known where loading happens; it is
	// added to the first use of the model on this computer.
	for _, m := range c.models {
		if m.ModelID == modelID && m.LoadMs == 0 {
			m.LoadMs = ms(d)
			return
		}
	}
	c.model(modelID, "", "").LoadMs = ms(d)
}

// GenerationMetrics are what one model call reported.
type GenerationMetrics struct {
	TTFTMs           float64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	TokPerSec        float64
	// Backend and Device ran the call, when known.
	Backend, Device string
}

// ModelCall records one model call: firstToken is the wall time to its
// first token, and m what the runtime reported, if anything.
func (c *Collector) ModelCall(modelID, role, node string, firstToken time.Duration, m *GenerationMetrics) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A load recorded before the call was attributed to a placeholder.
	var load float64
	if pending, ok := c.models[modelID+"||"]; ok && (role != "" || node != "") {
		load = pending.LoadMs
		delete(c.models, modelID+"||")
	}
	u := c.model(modelID, role, node)
	u.LoadMs += load
	u.Calls++
	if u.FirstTokenMs == 0 && firstToken > 0 {
		u.FirstTokenMs = ms(firstToken)
	}
	if m != nil {
		if u.TTFTMs == 0 {
			u.TTFTMs = m.TTFTMs
		}
		u.PromptTokens += m.PromptTokens
		u.CompletionTokens += m.CompletionTokens
		u.CachedTokens += m.CachedTokens
		if m.Backend != "" {
			u.Backend, u.Device = m.Backend, m.Device
		}
		if m.TokPerSec > 0 {
			u.TokPerSec = m.TokPerSec
		}
	}
	if node != "" {
		c.nodes[node] = true
	}
}

// ToolCall records one tool call.
func (c *Collector) ToolCall(toolID string, d time.Duration, failed bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tools[toolID]
	if !ok {
		t = &ToolUse{ToolID: toolID}
		c.tools[toolID] = t
	}
	t.Calls++
	t.TotalMs += ms(d)
	if failed {
		t.Failures++
	}
}

// CacheHit records a tool call answered from a cache.
func (c *Collector) CacheHit(toolID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run.CacheHits == nil {
		c.run.CacheHits = map[string]int{}
	}
	c.run.CacheHits[toolID]++
}

// Plan records a plan's parts.
func (c *Collector) Plan(workers int, parallel bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.Workers, c.run.Parallel = workers, parallel
	c.mu.Unlock()
}

// Verified records one answer check.
func (c *Collector) Verified(issues, fixed int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.VerificationPasses++
	c.run.VerificationIssues += issues
	c.run.VerificationFixed += fixed
	c.mu.Unlock()
}

// Retried records an answer tried again on another model.
func (c *Collector) Retried() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.Retries++
	c.mu.Unlock()
}

// Context records the prompt size the model saw and its window.
func (c *Collector) Context(tokens, limit int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.ContextTokens, c.run.ContextLimit = tokens, limit
	c.mu.Unlock()
}

// Pipeline records the time before the first model call.
func (c *Collector) Pipeline(d time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.run.PipelineMs = ms(d)
	c.mu.Unlock()
}

// Finish ends the run and returns it. err is why it failed, or nil; its
// stable code, if it has one, goes with the text.
func (c *Collector) Finish(status string, err error) Run {
	if c == nil {
		return Run{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UTC()
	r := c.run
	r.Contract = contracts.ContractVersion
	r.Status, r.CompletedAt = status, &now
	if err != nil {
		r.Error = err.Error()
		r.ErrorCode, r.ErrorDetails = contracts.ErrorCode(err)
	}
	r.LatencyMs = ms(now.Sub(r.StartedAt))
	r.Models = []ModelUse{}
	for _, m := range c.models {
		r.Models = append(r.Models, *m)
	}
	sort.Slice(r.Models, func(i, j int) bool {
		return r.Models[i].Role+r.Models[i].ModelID < r.Models[j].Role+r.Models[j].ModelID
	})
	r.Tools = []ToolUse{}
	for _, t := range c.tools {
		r.Tools = append(r.Tools, *t)
	}
	sort.Slice(r.Tools, func(i, j int) bool { return r.Tools[i].ToolID < r.Tools[j].ToolID })
	r.Nodes = []string{}
	for n := range c.nodes {
		r.Nodes = append(r.Nodes, n)
	}
	sort.Strings(r.Nodes)
	return r
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// ErrNotFound is returned for an unknown run.
var ErrNotFound = contracts.NewError("RUN_NOT_FOUND", nil, errors.New("run not found"))

// Store keeps runs.
type Store struct{ db *sql.DB }

// NewStore returns a store in db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Save stores a finished run.
func (s *Store) Save(ctx context.Context, r Run) error {
	if s == nil || s.db == nil || r.ID == "" {
		return nil
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var completed any
	if r.CompletedAt != nil {
		completed = r.CompletedAt.Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runs (id, conversation_id, source, status, started_at, completed_at, detail_json) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status = excluded.status, completed_at = excluded.completed_at, detail_json = excluded.detail_json`,
		r.ID, nullable(r.ConversationID), r.Source, r.Status, r.StartedAt.Format(time.RFC3339Nano), completed, string(raw))
	return err
}

// Get returns one run.
func (s *Store) Get(ctx context.Context, id string) (Run, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT detail_json FROM runs WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	var r Run
	return r, json.Unmarshal([]byte(raw), &r)
}

// List returns recent runs, newest first, optionally for one conversation.
func (s *Store) List(ctx context.Context, conversationID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q, args := `SELECT detail_json FROM runs`, []any{}
	if conversationID != "" {
		q += ` WHERE conversation_id = ?`
		args = append(args, conversationID)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r Run
		if json.Unmarshal([]byte(raw), &r) == nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}
