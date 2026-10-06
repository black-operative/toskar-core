package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/orchestrator"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/scheduler"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Manager creates and runs orchestration tasks.
type Manager struct {
	db           *sql.DB
	bus          *events.Bus
	profiles     *profiles.Manager
	orchRegistry *orchestrator.Registry
	runtimes     *runtimes.Manager
	scheduler    *scheduler.Scheduler
	tools        *tools.Registry
	nodes        func(ctx context.Context) ([]contracts.Node, error)
	modelPath    func(ctx context.Context, modelID string) (string, error)
	placeRole    func(ctx context.Context, profile profiles.Profile, role, modelID string) (string, error)
	generateOn   func(ctx context.Context, nodeID, modelID string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error)

	mu      sync.Mutex
	running map[string]*runningModel
}

type runningModel struct {
	instanceID string
	endpoint   string
	runtimeID  string
}

// NewManager constructs a task manager.
func NewManager(
	db *sql.DB,
	bus *events.Bus,
	profiles *profiles.Manager,
	orch *orchestrator.Registry,
	runtimes *runtimes.Manager,
	sched *scheduler.Scheduler,
	tools *tools.Registry,
	nodes func(ctx context.Context) ([]contracts.Node, error),
	modelPath func(ctx context.Context, modelID string) (string, error),
) *Manager {
	return &Manager{
		db:           db,
		bus:          bus,
		profiles:     profiles,
		orchRegistry: orch,
		runtimes:     runtimes,
		scheduler:    sched,
		tools:        tools,
		nodes:        nodes,
		modelPath:    modelPath,
		running:      make(map[string]*runningModel),
	}
}

// SetClusterHooks wires remote placement and generation.
func (m *Manager) SetClusterHooks(
	place func(ctx context.Context, profile profiles.Profile, role, modelID string) (string, error),
	generate func(ctx context.Context, nodeID, modelID string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error),
) {
	m.placeRole = place
	m.generateOn = generate
}

// Create inserts a pending task.
func (m *Manager) Create(ctx context.Context, profileID, conversationID, prompt string) (contracts.Task, error) {
	task := contracts.Task{
		ID:             uuid.NewString(),
		ProfileID:      profileID,
		ConversationID: conversationID,
		Prompt:         prompt,
		Status:         contracts.TaskPending,
		CreatedAt:      time.Now().UTC(),
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO tasks (id, profile_id, conversation_id, prompt, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		task.ID, nullStr(task.ProfileID), nullStr(task.ConversationID), task.Prompt, task.Status, task.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return task, err
	}
	m.bus.Publish(events.New(events.TaskCreated, map[string]any{"task_id": task.ID}))
	return task, nil
}

// SetChatStatus records where a chat turn's task stands: running when the
// turn starts, then completed, failed (with why), or cancelled when it was
// stopped. A chat's task was never moved past pending before, so every
// chat counted as active on the Performance page. It publishes no task
// events: those raise a "task finished" notice, and a chat reply is its
// own notice.
func (m *Manager) SetChatStatus(ctx context.Context, taskID string, status contracts.TaskStatus, errMsg string) {
	if status == contracts.TaskRunning || status == contracts.TaskPending {
		_, _ = m.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, status, taskID)
		return
	}
	_, _ = m.db.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, completed_at = ? WHERE id = ?`,
		status, nullStr(errMsg), time.Now().UTC().Format(time.RFC3339Nano), taskID)
}

// SettleInterrupted ends tasks left pending or running by an earlier run of
// the daemon, which can't finish them now, and chats from before
// SetChatStatus, which never left pending. It reports how many it ended.
func (m *Manager) SettleInterrupted(ctx context.Context) (int64, error) {
	res, err := m.db.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, completed_at = ? WHERE status IN (?, ?)`,
		contracts.TaskFailed, "Toskar stopped before this finished.", time.Now().UTC().Format(time.RFC3339Nano),
		contracts.TaskPending, contracts.TaskRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Get loads a task by ID.
func (m *Manager) Get(ctx context.Context, id string) (contracts.Task, error) {
	var t contracts.Task
	var profileID, convID, created, completed sql.NullString
	err := m.db.QueryRowContext(ctx, `
		SELECT id, profile_id, conversation_id, prompt, status, COALESCE(result,''), COALESCE(error,''), created_at, completed_at
		FROM tasks WHERE id = ?`, id).Scan(
		&t.ID, &profileID, &convID, &t.Prompt, &t.Status, &t.Result, &t.Error, &created, &completed)
	if err == sql.ErrNoRows {
		return t, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return t, err
	}
	t.ProfileID = profileID.String
	t.ConversationID = convID.String
	t.CreatedAt = parseTime(created.String)
	return t, nil
}

// List returns recent tasks.
func (m *Manager) List(ctx context.Context) ([]contracts.Task, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, profile_id, conversation_id, prompt, status, COALESCE(result,''), COALESCE(error,''), created_at
		FROM tasks ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Task
	for rows.Next() {
		var t contracts.Task
		var profileID, convID, created sql.NullString
		if err := rows.Scan(&t.ID, &profileID, &convID, &t.Prompt, &t.Status, &t.Result, &t.Error, &created); err != nil {
			return nil, err
		}
		t.ProfileID = profileID.String
		t.ConversationID = convID.String
		t.CreatedAt = parseTime(created.String)
		out = append(out, t)
	}
	if out == nil {
		out = []contracts.Task{}
	}
	return out, rows.Err()
}

// Run executes a task asynchronously.
func (m *Manager) Run(ctx context.Context, taskID string) error {
	task, err := m.Get(ctx, taskID)
	if err != nil {
		return err
	}
	profile, err := m.profiles.Get(ctx, task.ProfileID)
	if err != nil {
		return err
	}
	orch, err := m.orchRegistry.Get(profile.OrchestratorID)
	if err != nil {
		return err
	}
	if err := orch.ValidateProfile(ctx, profile); err != nil {
		return err
	}

	_, _ = m.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, contracts.TaskRunning, taskID)
	m.bus.Publish(events.New(events.TaskStarted, map[string]any{"task_id": taskID}))

	go m.runTask(context.Background(), task, profile, orch)
	return nil
}

func (m *Manager) runTask(ctx context.Context, task contracts.Task, profile profiles.Profile, orch pluginapi.Orchestrator) {
	env := &execEnv{m: m, ctx: ctx, profile: profile, taskID: task.ID}
	stream, err := orch.Run(ctx, task, profile, env)
	if err != nil {
		m.failTask(ctx, task.ID, err)
		return
	}
	var result strings.Builder
	stepIndex := 0
	for evt := range stream {
		m.recordStep(ctx, task.ID, stepIndex, evt)
		stepIndex++
		if evt.Content != "" && evt.Done {
			result.WriteString(evt.Content)
		}
	}
	now := time.Now().UTC()
	_, _ = m.db.ExecContext(ctx, `UPDATE tasks SET status = ?, result = ?, completed_at = ? WHERE id = ?`,
		contracts.TaskCompleted, result.String(), now.Format(time.RFC3339Nano), task.ID)
	m.bus.Publish(events.New(events.TaskCompleted, map[string]any{"task_id": task.ID, "result": result.String()}))
}

func (m *Manager) failTask(ctx context.Context, taskID string, err error) {
	now := time.Now().UTC()
	_, _ = m.db.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, completed_at = ? WHERE id = ?`,
		contracts.TaskFailed, err.Error(), now.Format(time.RFC3339Nano), taskID)
	m.bus.Publish(events.New(events.TaskFailed, map[string]any{"task_id": taskID, "error": err.Error()}))
}

func (m *Manager) recordStep(ctx context.Context, taskID string, idx int, evt pluginapi.OrchestrationEvent) {
	_, _ = m.db.ExecContext(ctx, `
		INSERT INTO task_steps (id, task_id, step_index, role, node_id, status, input, output, meta_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		uuid.NewString(), taskID, idx, evt.Role, nullStr(evt.NodeID), evt.Type, "", evt.Content, "{}")
}

type execEnv struct {
	m       *Manager
	ctx     context.Context
	profile profiles.Profile
	taskID  string
}

func (e *execEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	modelID := e.modelForRole(role)
	nodeID, err := e.NodeForRole(role)
	if err != nil {
		return nil, err
	}
	if e.m.generateOn != nil {
		return e.m.generateOn(ctx, nodeID, modelID, messages)
	}
	endpoint, err := e.m.ensureModelRunning(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return e.m.runtimes.Chat(ctx, pluginapi.ChatRequest{
		ModelEndpoint: endpoint,
		Messages:      messages,
		Stream:        true,
	})
}

func (e *execEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	policy := tools.PolicyForProfile(e.profile, toolID)
	return e.m.tools.Execute(ctx, toolID, args, policy, "orchestrator requested tool", map[string]any{
		"task_id": e.taskID,
	})
}

func (e *execEnv) Emit(eventType string, payload map[string]any) {
	e.m.bus.Publish(events.New(eventType, payload))
}

func (e *execEnv) NodeForRole(role string) (string, error) {
	modelID := e.modelForRole(role)
	if e.m.placeRole != nil {
		return e.m.placeRole(e.ctx, e.profile, role, modelID)
	}
	nodes, err := e.m.nodes(e.ctx)
	if err != nil {
		return "", err
	}
	installed := map[string][]string{}
	for _, n := range nodes {
		if n.IsLocal {
			installed[n.ID] = []string{modelID}
		}
	}
	candidates := scheduler.BuildCandidates(nodes, installed, nil)
	decision, err := e.m.scheduler.PlaceRole(e.ctx, scheduler.ScoreInput{
		Role:    role,
		ModelID: modelID,
		Profile: e.profile,
		Nodes:   candidates,
	})
	return decision.NodeID, err
}

func (e *execEnv) modelForRole(role string) string {
	if id := profiles.RoleModel(e.profile, role); id != "" {
		return id
	}
	for _, r := range e.profile.Roles {
		if r.ModelID != "" {
			return r.ModelID
		}
	}
	return ""
}

func (m *Manager) ensureModelRunning(ctx context.Context, modelID string) (string, error) {
	if modelID == "" {
		return "", contracts.Errorf("NO_MODEL_ASSIGNED", nil, "no model assigned")
	}
	m.mu.Lock()
	if rm, ok := m.running[modelID]; ok {
		m.mu.Unlock()
		return rm.endpoint, nil
	}
	m.mu.Unlock()

	path, err := m.modelPath(ctx, modelID)
	if err != nil {
		return "", err
	}
	running, err := m.runtimes.StartModel(ctx, "llamacpp", runtimes.ModelStartConfig{
		ModelID:   modelID,
		ModelPath: path,
	})
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.running[modelID] = &runningModel{instanceID: running.ID, endpoint: running.Endpoint, runtimeID: running.RuntimeID}
	m.mu.Unlock()
	return running.Endpoint, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
