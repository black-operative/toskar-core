// Package gjallarhorn is Yggdrasil's notification system
// (docs/features/gjallarhorn-notification-system.md). Every notice first
// becomes a durable notification in the in-app notification center; delivery
// channels such as desktop notifications are attempts recorded beside it.
package gjallarhorn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/locale"
)

// Categories.
const (
	CategoryAutomation = "automation"
	CategoryApproval   = "approval"
	CategoryModel      = "model"
	CategoryTraining   = "training"
	CategoryHealth     = "health"
	CategorySystem     = "system"
)

// Severities.
const (
	SeverityInfo    = "info"
	SeveritySuccess = "success"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Delivery statuses.
const (
	DeliveryDelivered  = "delivered"
	DeliveryFailed     = "failed"
	DeliverySuppressed = "suppressed"
	// DeliveryPending waits for its next attempt: the first one, sent in
	// the background, or a retry (§36).
	DeliveryPending = "pending"
	// DeliveryHeld waits for quiet hours to end (§25).
	DeliveryHeld = "held"
	// DeliveryDigest waits for its destination's daily digest (#111).
	DeliveryDigest = "digest"
	// DeliveryCancelled was never sent, such as when its destination was removed.
	DeliveryCancelled = "cancelled"
)

// EventCreated tells clients a notification was stored.
const EventCreated = "notification.created"

// EventDesktop hands a desktop notice to the desktop app, when it posts them
// itself (TOSKAR_DESKTOP_NOTIFICATIONS=shell): its title and body are in the
// App language, after quiet hours and the user's desktop setting.
const EventDesktop = "notification.desktop"

// dedupeWindow is how long a repeated notice with the same key is folded
// into the first one instead of stored again.
const dedupeWindow = 10 * time.Minute

// Delivery is one channel's attempt to deliver a notification.
type Delivery struct {
	Channel string `json:"channel"`
	// DestinationID is the email or webhook destination, when there is one.
	DestinationID string     `json:"destination_id,omitempty"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	// NextAttemptAt is when a pending or held delivery is tried next.
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// Notification is a stored notice.
type Notification struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id,omitempty"`
	Category   string    `json:"category"`
	Severity   string    `json:"severity"`
	// Title and Body are in English, for logs and clients that read text.
	Title string `json:"title"`
	Body  string `json:"body"`
	// Message is the title and body as catalog keys with their values
	// (multilingual spec §22), so each place that shows the notification
	// writes it in its own language. Notices whose text comes from
	// elsewhere, such as an automation's result, may have none.
	Message *locale.Message `json:"message,omitempty"`
	// Link is an app path to the source, such as /automations?id=….
	Link   string     `json:"link,omitempty"`
	ReadAt *time.Time `json:"read_at,omitempty"`
	// RepeatCount is how many times the same notice came within the dedupe
	// window, shown as "4 times" (§22).
	RepeatCount int        `json:"repeat_count,omitempty"`
	Deliveries  []Delivery `json:"deliveries,omitempty"`
	// lang is the language Title and Body are written in, once a delivery
	// has localized them (see Hub.localized); "" is English.
	lang string
}

// Request asks for a notification.
type Request struct {
	SourceType string
	SourceID   string
	Category   string
	Severity   string
	Title      string
	Body       string
	// Message is the title and body as catalog keys. With a message, Title
	// and Body may be left empty: they are written from it in English.
	Message *locale.Message
	Link    string
	// DedupeKey folds repeats within dedupeWindow into one notification.
	DedupeKey string
	// Channels to deliver to besides the notification center, such as "desktop".
	Channels []string
}

// Channel delivers a stored notification somewhere outside the app.
type Channel interface {
	Name() string
	Deliver(ctx context.Context, n Notification) error
}

// ErrSuppressed means a channel chose not to deliver, such as desktop
// notifications turned off in settings. The notification is still stored.
var ErrSuppressed = errors.New("delivery suppressed")

// Hub stores notifications and fans them out.
type Hub struct {
	db       *sql.DB
	bus      *events.Bus
	channels map[string]Channel
	now      func() time.Time
	secrets  Secrets
	settings SettingsStore
	egress   EgressRecorder
	client   *http.Client
	wake     chan struct{}
}

func newID() string { return uuid.NewString() }

// messageJSON is a message as stored, or NULL for none.
func messageJSON(m *locale.Message) any {
	if m == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return string(raw)
}

// language is the App language (the ui_locale setting), which text that
// leaves the app is written in: desktop notices, email, push, and webhooks.
// "" (the system's language) and an unknown tag are English.
func (h *Hub) language(ctx context.Context) string {
	if h.settings == nil {
		return locale.Source
	}
	tag, _, err := h.settings.Get(ctx, "ui_locale")
	if err != nil {
		return locale.Source
	}
	return locale.Resolve(tag)
}

// localized is n with its title and body written in the App language.
func (h *Hub) localized(ctx context.Context, n Notification) Notification {
	n.lang = h.language(ctx)
	if n.Message != nil {
		n.Title, n.Body = n.Message.Render(n.lang)
	}
	return n
}

// text is a catalog key in the language n is written in.
func (n Notification) text(key string, params map[string]any) string {
	lang := n.lang
	if lang == "" {
		lang = locale.Source
	}
	return locale.T(lang, key, params)
}

// NewHub returns a hub using db and announcing on bus.
func NewHub(db *sql.DB, bus *events.Bus, channels ...Channel) *Hub {
	h := &Hub{db: db, bus: bus, channels: map[string]Channel{}, now: time.Now, client: webhookClient, wake: make(chan struct{}, 1)}
	for _, c := range channels {
		h.channels[c.Name()] = c
	}
	return h
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Notify stores a notification, announces it to the app, and delivers it to
// the requested channels and to every destination that takes it (§15, §26).
// Email and webhooks go out in the background and are retried on their own;
// during quiet hours, deliveries outside the app wait (§25). A repeat with
// the same dedupe key within ten minutes counts on the existing
// notification, which is marked unread again (§22).
func (h *Hub) Notify(ctx context.Context, req Request) (Notification, error) {
	if req.Message != nil && strings.TrimSpace(req.Title) == "" {
		req.Title, req.Body = req.Message.Render(locale.Source)
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return Notification{}, fmt.Errorf("title required")
	}
	if req.Category == "" {
		req.Category = CategorySystem
	}
	if req.Severity == "" {
		req.Severity = SeverityInfo
	}
	now := h.now().UTC()
	if req.DedupeKey != "" {
		var id string
		err := h.db.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ? AND created_at >= ? AND dismissed_at IS NULL ORDER BY created_at DESC LIMIT 1`,
			req.DedupeKey, ts(now.Add(-dedupeWindow))).Scan(&id)
		if err == nil {
			_, _ = h.db.ExecContext(ctx, `UPDATE notifications SET updated_at = ?, read_at = NULL, body = ?, message = ?, repeat_count = repeat_count + 1 WHERE id = ?`,
				ts(now), req.Body, messageJSON(req.Message), id)
			return h.Get(ctx, id)
		}
	}
	n := Notification{
		ID: uuid.NewString(), CreatedAt: now, RepeatCount: 1, SourceType: req.SourceType, SourceID: req.SourceID,
		Category: req.Category, Severity: req.Severity, Title: req.Title, Body: strings.TrimSpace(req.Body), Message: req.Message, Link: req.Link,
	}
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO notifications (id, created_at, updated_at, source_type, source_id, category, severity, title, body, message, link, dedupe_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, ts(now), ts(now), n.SourceType, nullable(n.SourceID), n.Category, n.Severity, n.Title, n.Body, messageJSON(n.Message), nullable(n.Link), nullable(req.DedupeKey))
	if err != nil {
		return Notification{}, err
	}
	if h.bus != nil {
		h.bus.Publish(events.New(EventCreated, map[string]any{
			"id": n.ID, "category": n.Category, "severity": n.Severity, "title": n.Title, "body": n.Body, "message": n.Message, "link": n.Link,
		}))
	}
	heldUntil, held := h.QuietHours(ctx).HeldUntil(n.Severity, now)
	for _, name := range req.Channels {
		if held {
			n.Deliveries = append(n.Deliveries, h.queue(ctx, n, name, "", DeliveryHeld, heldUntil))
			continue
		}
		n.Deliveries = append(n.Deliveries, h.deliver(ctx, n, name))
	}
	if dests, err := h.Destinations(ctx); err == nil {
		queued := false
		for _, d := range dests {
			if !d.Accepts(n) {
				continue
			}
			// Errors go out at once; anything else waits for the digest.
			if d.Digest != nil && n.Severity != SeverityError {
				n.Deliveries = append(n.Deliveries, h.queue(ctx, n, d.channelName(), d.ID, DeliveryDigest, d.Digest.Next(now)))
				continue
			}
			if held {
				n.Deliveries = append(n.Deliveries, h.queue(ctx, n, d.channelName(), d.ID, DeliveryHeld, heldUntil))
				continue
			}
			n.Deliveries = append(n.Deliveries, h.queue(ctx, n, d.channelName(), d.ID, DeliveryPending, now))
			queued = true
		}
		if queued {
			h.poke()
		}
	}
	return n, nil
}

// deliver makes one channel's attempt and records it.
func (h *Hub) deliver(ctx context.Context, n Notification, name string) Delivery {
	d := Delivery{Channel: name, Attempts: 1}
	ch, ok := h.channels[name]
	var err error
	switch {
	case !ok:
		err = fmt.Errorf("channel %q is not available", name)
	default:
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = ch.Deliver(dctx, h.localized(ctx, n))
		cancel()
	}
	now := h.now().UTC()
	switch {
	case err == nil:
		d.Status, d.DeliveredAt = DeliveryDelivered, &now
	case errors.Is(err, ErrSuppressed):
		d.Status = DeliverySuppressed
	default:
		d.Status, d.Error = DeliveryFailed, err.Error()
	}
	var delivered any
	if d.DeliveredAt != nil {
		delivered = ts(*d.DeliveredAt)
	}
	_, _ = h.db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (id, notification_id, channel, status, attempts, last_attempt_at, delivered_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), n.ID, name, d.Status, d.Attempts, ts(now), delivered, nullable(d.Error))
	return d
}

const columns = `id, created_at, source_type, COALESCE(source_id, ''), category, severity, title, body, COALESCE(message, ''), COALESCE(link, ''), read_at, repeat_count`

func scan(row interface{ Scan(...any) error }) (Notification, error) {
	var n Notification
	var created, message string
	var read sql.NullString
	if err := row.Scan(&n.ID, &created, &n.SourceType, &n.SourceID, &n.Category, &n.Severity, &n.Title, &n.Body, &message, &n.Link, &read, &n.RepeatCount); err != nil {
		return Notification{}, err
	}
	if message != "" {
		var m locale.Message
		if json.Unmarshal([]byte(message), &m) == nil {
			n.Message = &m
		}
	}
	if t := parseTS(sql.NullString{String: created, Valid: true}); t != nil {
		n.CreatedAt = *t
	}
	n.ReadAt = parseTS(read)
	return n, nil
}

// Get returns one notification with its deliveries.
func (h *Hub) Get(ctx context.Context, id string) (Notification, error) {
	n, err := scan(h.db.QueryRowContext(ctx, `SELECT `+columns+` FROM notifications WHERE id = ?`, id))
	if err != nil {
		return Notification{}, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT channel, COALESCE(destination_id, ''), status, attempts, delivered_at, next_attempt_at, COALESCE(error, '') FROM notification_deliveries WHERE notification_id = ?`, id)
	if err != nil {
		return n, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Delivery
		var delivered, next sql.NullString
		if err := rows.Scan(&d.Channel, &d.DestinationID, &d.Status, &d.Attempts, &delivered, &next, &d.Error); err != nil {
			return n, err
		}
		d.DeliveredAt = parseTS(delivered)
		d.NextAttemptAt = parseTS(next)
		n.Deliveries = append(n.Deliveries, d)
	}
	return n, rows.Err()
}

// List returns recent notifications that were not dismissed, newest first,
// and how many are unread. category, when set, lists that category only.
func (h *Hub) List(ctx context.Context, unreadOnly bool, limit int, category ...string) ([]Notification, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := `dismissed_at IS NULL`
	args := []any{}
	if unreadOnly {
		where += ` AND read_at IS NULL`
	}
	if len(category) > 0 && category[0] != "" {
		where += ` AND category = ?`
		args = append(args, category[0])
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+columns+` FROM notifications WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	err = h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE dismissed_at IS NULL AND read_at IS NULL`).Scan(&unread)
	return out, unread, err
}

// MarkRead marks notifications read; no ids marks all of them.
func (h *Hub) MarkRead(ctx context.Context, ids []string) error {
	now := ts(h.now())
	if len(ids) == 0 {
		_, err := h.db.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE read_at IS NULL`, now)
		return err
	}
	for _, id := range ids {
		if _, err := h.db.ExecContext(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, ?) WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

// Dismiss hides a notification from the notification center.
func (h *Hub) Dismiss(ctx context.Context, id string) error {
	res, err := h.db.ExecContext(ctx, `UPDATE notifications SET dismissed_at = ?, read_at = COALESCE(read_at, ?) WHERE id = ?`, ts(h.now()), ts(h.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
