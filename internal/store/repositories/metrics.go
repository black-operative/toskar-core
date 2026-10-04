package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// MetricsRepo stores per-turn generation performance samples.
type MetricsRepo struct {
	db *sql.DB
}

func NewMetricsRepo(db *sql.DB) *MetricsRepo {
	return &MetricsRepo{db: db}
}

// Insert records a generation sample.
func (r *MetricsRepo) Insert(ctx context.Context, run contracts.GenerationRun) (contracts.GenerationRun, error) {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.RuntimeID == "" {
		run.RuntimeID = "llamacpp"
	}
	if run.TotalTokens == 0 {
		run.TotalTokens = run.PromptTokens + run.CompletionTokens
	}
	normalizeRunClusterFields(&run)
	stepsJSON, err := json.Marshal(run.RoleSteps)
	if err != nil {
		return run, err
	}
	if len(run.RoleSteps) == 0 {
		stepsJSON = []byte("[]")
	}
	cross := 0
	if run.CrossMachine {
		cross = 1
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO generation_metrics (
			id, conversation_id, conversation_title, message_id, profile_id, profile_name,
			model_id, runtime_id, prompt_tokens, completion_tokens, total_tokens,
			ttft_ms, prompt_ms, eval_ms, total_ms, prompt_tok_per_sec, eval_tok_per_sec,
			role_steps_json, cross_machine, node_count, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, nullIfEmpty(run.ConversationID), nullIfEmpty(run.ConversationTitle),
		nullIfEmpty(run.MessageID), nullIfEmpty(run.ProfileID), nullIfEmpty(run.ProfileName),
		run.ModelID, run.RuntimeID, run.PromptTokens, run.CompletionTokens, run.TotalTokens,
		run.TTFTMs, run.PromptMs, run.EvalMs, run.TotalMs, run.PromptTokPerSec, run.EvalTokPerSec,
		string(stepsJSON), cross, run.NodeCount,
		run.CreatedAt.Format(time.RFC3339Nano),
	)
	return run, err
}

// ListFilter controls sorting and filtering for the Performance tab.
type ListFilter struct {
	Sort  string // created_at | model_id | conversation_title | profile_name | ttft_ms | eval_ms | total_ms | eval_tok_per_sec | prompt_tok_per_sec | completion_tokens | cross_machine | node_count
	Order string // asc | desc
	Limit int
}

var allowedSort = map[string]string{
	"created_at":         "created_at",
	"model":              "model_id",
	"model_id":           "model_id",
	"chat":               "conversation_title",
	"conversation":       "conversation_title",
	"conversation_title": "conversation_title",
	"profile":            "profile_name",
	"profile_name":       "profile_name",
	"ttft_ms":            "ttft_ms",
	"ttft":               "ttft_ms",
	"prompt_ms":          "prompt_ms",
	"eval_ms":            "eval_ms",
	"eval":               "eval_ms",
	"total_ms":           "total_ms",
	"total":              "total_ms",
	"eval_tok_per_sec":   "eval_tok_per_sec",
	"tok_per_sec":        "eval_tok_per_sec",
	"prompt_tok_per_sec": "prompt_tok_per_sec",
	"completion_tokens":  "completion_tokens",
	"tokens":             "completion_tokens",
	"cross_machine":      "cross_machine",
	"node_count":         "node_count",
}

// List returns generation samples with sorting.
func (r *MetricsRepo) List(ctx context.Context, filter ListFilter) ([]contracts.GenerationRun, error) {
	sortCol := "created_at"
	if mapped, ok := allowedSort[strings.ToLower(filter.Sort)]; ok {
		sortCol = mapped
	}
	order := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		order = "ASC"
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	// sortCol is from an allowlist only — safe to interpolate.
	q := fmt.Sprintf(`
		SELECT id, COALESCE(conversation_id,''), COALESCE(conversation_title,''), COALESCE(message_id,''),
			COALESCE(profile_id,''), COALESCE(profile_name,''), model_id, runtime_id,
			prompt_tokens, completion_tokens, total_tokens,
			ttft_ms, prompt_ms, eval_ms, total_ms, prompt_tok_per_sec, eval_tok_per_sec,
			COALESCE(role_steps_json,'[]'), COALESCE(cross_machine,0), COALESCE(node_count,0), created_at
		FROM generation_metrics
		ORDER BY %s %s
		LIMIT ?`, sortCol, order)

	rows, err := r.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []contracts.GenerationRun
	for rows.Next() {
		var run contracts.GenerationRun
		var created, stepsJSON string
		var cross int
		if err := rows.Scan(
			&run.ID, &run.ConversationID, &run.ConversationTitle, &run.MessageID,
			&run.ProfileID, &run.ProfileName, &run.ModelID, &run.RuntimeID,
			&run.PromptTokens, &run.CompletionTokens, &run.TotalTokens,
			&run.TTFTMs, &run.PromptMs, &run.EvalMs, &run.TotalMs,
			&run.PromptTokPerSec, &run.EvalTokPerSec,
			&stepsJSON, &cross, &run.NodeCount, &created,
		); err != nil {
			return nil, err
		}
		run.CrossMachine = cross != 0
		run.CreatedAt = parseTime(created)
		if stepsJSON != "" && stepsJSON != "[]" {
			_ = json.Unmarshal([]byte(stepsJSON), &run.RoleSteps)
		}
		if run.RoleSteps == nil {
			run.RoleSteps = []contracts.GenerationRoleStep{}
		}
		out = append(out, run)
	}
	if out == nil {
		out = []contracts.GenerationRun{}
	}
	return out, rows.Err()
}

// DeleteAll clears all performance samples.
func (r *MetricsRepo) DeleteAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM generation_metrics`)
	return err
}

func normalizeRunClusterFields(run *contracts.GenerationRun) {
	nodes := map[string]struct{}{}
	for _, s := range run.RoleSteps {
		if s.NodeID != "" {
			nodes[s.NodeID] = struct{}{}
		}
	}
	run.NodeCount = len(nodes)
	run.CrossMachine = len(nodes) > 1
}
