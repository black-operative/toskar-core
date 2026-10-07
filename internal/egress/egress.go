// Package egress records what left this computer (spec §63). Yggdrasil is
// local-first, so every web search, page fetch, paired computer, external
// server, and connected service a run sends data to is written down, with
// the run it belonged to.
package egress

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Kinds of destination.
const (
	WebSearch      = "web_search"
	WebPage        = "web_page"
	PairedComputer = "paired_computer"
	ExternalServer = "external_server"
	Connector      = "connector"
	// Notification is an email or webhook delivery (Gjallarhorn).
	Notification = "notification"
	// Places is a map service: finding places or a route (Gungnir §25).
	Places = "places"
	// CommunityRatings is a rating shared or withdrawn, or the public
	// ratings summary downloaded (#37).
	CommunityRatings = "community_ratings"
	// UpdateCheck is the daily look at toskar.ai for a newer version.
	UpdateCheck = "update_check"
)

// Sources of a run.
const (
	SourceChat       = "chat"
	SourceAPI        = "api"
	SourceAutomation = "automation"
	SourceTraining   = "training"
)

// Record is one time data left this computer.
type Record struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Destination is where it went: a host, a paired computer's name, or a
	// connected service.
	Destination string `json:"destination"`
	// Detail is what went: a search query, a page address, or what was sent.
	Detail         string `json:"detail,omitempty"`
	Source         string `json:"source,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
}

// maxDetail caps a record's detail.
const maxDetail = 500

// Run identifies the run a record belongs to.
type Run struct {
	Source         string
	ConversationID string
	TaskID         string
}

type runKey struct{}

// WithRun carries the run, so deep callers can attribute what they send.
func WithRun(ctx context.Context, r Run) context.Context {
	return context.WithValue(ctx, runKey{}, r)
}

// RunFrom returns the run a context carries.
func RunFrom(ctx context.Context) Run {
	r, _ := ctx.Value(runKey{}).(Run)
	return r
}

// Log stores records.
type Log struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a log in db.
func New(db *sql.DB) *Log { return &Log{db: db, now: time.Now} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Add records data leaving this computer for the run ctx carries. It never
// fails the work it describes; a record that cannot be written is dropped.
func (l *Log) Add(ctx context.Context, kind, destination, detail string) {
	if l == nil || l.db == nil || destination == "" {
		return
	}
	detail = strings.TrimSpace(detail)
	if utf8.RuneCountInString(detail) > maxDetail {
		detail = string([]rune(detail)[:maxDetail]) + "…"
	}
	run := RunFrom(ctx)
	_, _ = l.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO egress (id, at, kind, destination, detail, source, conversation_id, task_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), l.now().UTC().Format(time.RFC3339Nano), kind, destination, detail, run.Source,
		nullable(run.ConversationID), nullable(run.TaskID))
}

// Filter narrows a listing.
type Filter struct {
	ConversationID string
	Limit          int
}

// List returns records, newest first.
func (l *Log) List(ctx context.Context, f Filter) ([]Record, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT id, at, kind, destination, detail, source, COALESCE(conversation_id, ''), COALESCE(task_id, '') FROM egress`
	args := []any{}
	if f.ConversationID != "" {
		q += ` WHERE conversation_id = ?`
		args = append(args, f.ConversationID)
	}
	q += ` ORDER BY at DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var at string
		if err := rows.Scan(&r.ID, &at, &r.Kind, &r.Destination, &r.Detail, &r.Source, &r.ConversationID, &r.TaskID); err != nil {
			return nil, err
		}
		r.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Summary counts records by kind since a time, for an overview.
func (l *Log) Summary(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT kind, COUNT(*) FROM egress WHERE at >= ? GROUP BY kind`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}
