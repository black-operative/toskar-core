package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Observations are how a model ran on this computer, which a shared rating
// includes only when the person chooses to.
type Observations struct {
	TokensPerSecond float64 `json:"tokens_per_second,omitempty"`
	TTFTMillis      int     `json:"ttft_ms,omitempty"`
	// Starts and StartFailures count model starts.
	Starts        int  `json:"starts,omitempty"`
	StartFailures int  `json:"start_failures,omitempty"`
	Crashed       bool `json:"crashed,omitempty"`
	OutOfMemory   bool `json:"out_of_memory,omitempty"`
	// ContextBand is the most context a reply used: 0-8k, 8-32k, 32-128k,
	// or 128k+.
	ContextBand string `json:"context_band,omitempty"`
}

// String is the observations in a line, for What left this computer.
func (o Observations) String() string {
	var parts []string
	if o.TokensPerSecond > 0 {
		parts = append(parts, strconv.FormatFloat(o.TokensPerSecond, 'f', 1, 64)+" tokens/s")
	}
	if o.TTFTMillis > 0 {
		parts = append(parts, strconv.Itoa(o.TTFTMillis)+" ms to first token")
	}
	if o.Starts > 0 {
		parts = append(parts, strconv.Itoa(o.Starts-o.StartFailures)+" of "+strconv.Itoa(o.Starts)+" starts worked")
	}
	if o.Crashed {
		parts = append(parts, "crashed")
	}
	if o.OutOfMemory {
		parts = append(parts, "ran out of memory")
	}
	if o.ContextBand != "" {
		parts = append(parts, o.ContextBand+" context")
	}
	return strings.Join(parts, ", ")
}

// Observation windows.
const (
	observeWindow = 30 * 24 * time.Hour
	keepEvents    = 90 * 24 * time.Hour
	// minSamples is how many replies a median needs.
	minSamples = 3
)

// RecordStart notes a model start on this computer and whether it failed.
// Only a failure of the model itself counts (pluginapi.ErrLoadFailed); a
// start that was cancelled, or a runtime that is not installed, says
// nothing about the model.
func (s *Service) RecordStart(ctx context.Context, modelID string, err error) {
	if modelID == "" || (err != nil && !errors.Is(err, pluginapi.ErrLoadFailed)) {
		return
	}
	kind, oom := "start", false
	if err != nil {
		kind, oom = "start_failed", s.OutOfMemory != nil && s.OutOfMemory(err.Error())
	}
	s.recordEvent(ctx, modelID, kind, oom)
}

// RecordCrash notes a model on this computer that stopped working.
func (s *Service) RecordCrash(ctx context.Context, modelID string, oom bool) {
	if modelID != "" {
		s.recordEvent(ctx, modelID, "crash", oom)
	}
}

func (s *Service) recordEvent(ctx context.Context, modelID, kind string, oom bool) {
	if s.DB == nil {
		return
	}
	now := s.now().UTC()
	ctx = context.WithoutCancel(ctx)
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO model_runtime_events (model_id, kind, out_of_memory, at) VALUES (?, ?, ?, ?)`,
		modelID, kind, oom, now.Format(time.RFC3339))
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM model_runtime_events WHERE at < ?`, now.Add(-keepEvents).Format(time.RFC3339))
}

// Observe is how a model ran on this computer in the last 30 days, or nil
// when nothing was measured. Replies that ran on a paired computer are
// left out.
func (s *Service) Observe(ctx context.Context, modelID string) (*Observations, error) {
	since := s.now().UTC().Add(-observeWindow)
	var o Observations
	rows, err := s.DB.QueryContext(ctx, `
		SELECT eval_tok_per_sec, ttft_ms, total_tokens, role_steps_json FROM generation_metrics
		WHERE model_id = ? AND created_at >= ? ORDER BY created_at DESC LIMIT 1000`, modelID, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	var tps, ttft []float64
	most := 0
	for rows.Next() {
		var speed, first float64
		var total int
		var steps string
		if err := rows.Scan(&speed, &first, &total, &steps); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if !s.ranHere(steps) {
			continue
		}
		if speed > 0 && speed <= 10000 {
			tps = append(tps, speed)
		}
		if first > 0 && first <= 600000 {
			ttft = append(ttft, first)
		}
		most = max(most, total)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tps) >= minSamples {
		o.TokensPerSecond = math.Round(median(tps)*10) / 10
	}
	if len(ttft) >= minSamples {
		o.TTFTMillis = int(math.Round(median(ttft)))
	}
	if most > 0 {
		o.ContextBand = contextBand(most)
	}
	var crashes, ooms int
	err = s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(kind IN ('start', 'start_failed')), 0), COALESCE(SUM(kind = 'start_failed'), 0),
		       COALESCE(SUM(kind = 'crash'), 0), COALESCE(SUM(out_of_memory), 0)
		FROM model_runtime_events WHERE model_id = ? AND at >= ?`, modelID, since.Format(time.RFC3339)).
		Scan(&o.Starts, &o.StartFailures, &crashes, &ooms)
	if err != nil {
		return nil, err
	}
	o.Crashed, o.OutOfMemory = crashes > 0, ooms > 0
	if o == (Observations{}) {
		return nil, nil
	}
	return &o, nil
}

// ranHere is true when no step of a reply ran on another computer.
func (s *Service) ranHere(stepsJSON string) bool {
	var steps []struct {
		NodeID string `json:"node_id"`
	}
	if json.Unmarshal([]byte(stepsJSON), &steps) != nil {
		return true
	}
	local := ""
	if s.LocalNode != nil {
		local = s.LocalNode()
	}
	for _, st := range steps {
		if st.NodeID != "" && st.NodeID != local {
			return false
		}
	}
	return true
}

func median(v []float64) float64 {
	v = slices.Clone(v)
	slices.Sort(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func contextBand(tokens int) string {
	switch {
	case tokens < 8192:
		return "0-8k"
	case tokens < 32768:
		return "8-32k"
	case tokens < 131072:
		return "32-128k"
	}
	return "128k+"
}
