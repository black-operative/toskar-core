package gjallarhorn

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
)

// SettingsStore keeps quiet hours with the other settings.
type SettingsStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// EgressRecorder records data leaving this computer (§63). Email and
// webhook deliveries are recorded as kind "notification".
type EgressRecorder interface {
	Add(ctx context.Context, kind, destination, detail string)
}

// EgressKind is how notification deliveries appear in What left this computer.
const EgressKind = "notification"

// quietKey is the setting that stores quiet hours.
const quietKey = "notification_quiet_hours"

// QuietHours hold notices from channels outside the app overnight (§24–25).
// Held notices are in the notification center at once and go out when quiet
// hours end.
type QuietHours struct {
	Enabled bool `json:"enabled"`
	// Start and End are HH:MM in TimeZone; End may be the next day.
	Start    string `json:"start"`
	End      string `json:"end"`
	TimeZone string `json:"time_zone"`
	// Allow is errors (the default: errors still go out, everything else
	// waits) or nothing (everything waits).
	Allow string `json:"allow"`
}

// DefaultQuietHours is off, 22:00–07:00, letting errors through.
func DefaultQuietHours() QuietHours {
	return QuietHours{Start: "22:00", End: "07:00", TimeZone: "UTC", Allow: "errors"}
}

func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

// Validate checks quiet hours.
func (q QuietHours) Validate() error {
	if _, ok := parseClock(q.Start); !ok {
		return fmt.Errorf("start must be HH:MM")
	}
	if _, ok := parseClock(q.End); !ok {
		return fmt.Errorf("end must be HH:MM")
	}
	if q.Start == q.End {
		return fmt.Errorf("start and end must differ")
	}
	if _, err := time.LoadLocation(q.TimeZone); err != nil {
		return fmt.Errorf("unknown time zone %q", q.TimeZone)
	}
	if q.Allow != "errors" && q.Allow != "nothing" {
		return fmt.Errorf("allow must be errors or nothing")
	}
	return nil
}

// HeldUntil reports whether a notice of severity waits at now, and until when.
func (q QuietHours) HeldUntil(severity string, now time.Time) (time.Time, bool) {
	if !q.Enabled || q.Validate() != nil {
		return time.Time{}, false
	}
	if q.Allow == "errors" && severity == SeverityError {
		return time.Time{}, false
	}
	loc, _ := time.LoadLocation(q.TimeZone)
	local := now.In(loc)
	start, _ := parseClock(q.Start)
	end, _ := parseClock(q.End)
	minute := local.Hour()*60 + local.Minute()
	inside := (start < end && minute >= start && minute < end) || (start > end && (minute >= start || minute < end))
	if !inside {
		return time.Time{}, false
	}
	until := time.Date(local.Year(), local.Month(), local.Day(), end/60, end%60, 0, 0, loc)
	if !until.After(local) {
		until = until.AddDate(0, 0, 1)
	}
	return until.UTC(), true
}

// QuietHours returns the stored quiet hours, or the defaults.
func (h *Hub) QuietHours(ctx context.Context) QuietHours {
	q := DefaultQuietHours()
	if h.settings == nil {
		return q
	}
	if raw, ok, err := h.settings.Get(ctx, quietKey); err == nil && ok && raw != "" {
		_ = json.Unmarshal([]byte(raw), &q)
	}
	return q
}

// SetQuietHours stores quiet hours.
func (h *Hub) SetQuietHours(ctx context.Context, q QuietHours) (QuietHours, error) {
	if q.Allow == "" {
		q.Allow = "errors"
	}
	if err := q.Validate(); err != nil {
		return QuietHours{}, err
	}
	if h.settings == nil {
		return QuietHours{}, fmt.Errorf("settings are not available")
	}
	b, _ := json.Marshal(q)
	if err := h.settings.Set(ctx, quietKey, string(b)); err != nil {
		return QuietHours{}, err
	}
	return q, nil
}

// SetSettings gives the hub a place for quiet hours.
func (h *Hub) SetSettings(s SettingsStore) { h.settings = s }

// SetEgress records email and webhook deliveries in What left this computer.
func (h *Hub) SetEgress(e EgressRecorder) { h.egress = e }

// retryAfter is the wait before each retry of a failed delivery (§37).
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

// queue records a delivery that waits: pending (go out now, in the
// background) or held (quiet hours).
func (h *Hub) queue(ctx context.Context, n Notification, channel, destinationID, status string, at time.Time) Delivery {
	d := Delivery{Channel: channel, DestinationID: destinationID, Status: status, NextAttemptAt: &at}
	_, _ = h.db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (id, notification_id, channel, status, attempts, destination_id, next_attempt_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`,
		newID(), n.ID, channel, status, nullable(destinationID), ts(at))
	return d
}

// Start runs the delivery worker until ctx ends: it sends queued and held
// deliveries when they are due and retries failed ones (§36).
func (h *Hub) Start(ctx context.Context) {
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			h.DeliverDue(ctx)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			case <-h.wake:
			}
		}
	}()
}

func (h *Hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

type due struct {
	id, notificationID, channel, destinationID, status string
	attempts                                           int
	firstAttempt                                       sql.NullString
}

// DeliverDue attempts every delivery whose time has come.
func (h *Hub) DeliverDue(ctx context.Context) {
	now := h.now().UTC()
	rows, err := h.db.QueryContext(ctx, `SELECT id, notification_id, channel, COALESCE(destination_id, ''), status, attempts, first_attempt_at
		FROM notification_deliveries WHERE status IN (?, ?) AND next_attempt_at <= ? ORDER BY next_attempt_at LIMIT 25`,
		DeliveryPending, DeliveryHeld, ts(now))
	if err != nil {
		return
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.notificationID, &d.channel, &d.destinationID, &d.status, &d.attempts, &d.firstAttempt) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		if ctx.Err() != nil {
			return
		}
		h.attemptDue(ctx, d)
	}
	h.deliverDigests(ctx)
}

func (h *Hub) attemptDue(ctx context.Context, d due) {
	n, err := h.Get(ctx, d.notificationID)
	if err != nil {
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, next_attempt_at = NULL WHERE id = ?`, DeliveryCancelled, d.id)
		return
	}
	now := h.now().UTC()
	// Quiet hours may have started since the delivery was queued.
	if until, held := h.QuietHours(ctx).HeldUntil(n.Severity, now); held {
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, next_attempt_at = ? WHERE id = ?`, DeliveryHeld, ts(until), d.id)
		return
	}
	err = h.send(ctx, n, d.channel, d.destinationID)
	attempts := d.attempts + 1
	first := ts(now)
	if d.firstAttempt.Valid {
		first = d.firstAttempt.String
	}
	switch {
	case err == nil:
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, attempts = ?, first_attempt_at = ?, last_attempt_at = ?, delivered_at = ?, next_attempt_at = NULL, error = NULL WHERE id = ?`,
			DeliveryDelivered, attempts, first, ts(now), ts(now), d.id)
	case IsPermanent(err) || attempts > len(retryAfter):
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, attempts = ?, first_attempt_at = ?, last_attempt_at = ?, next_attempt_at = NULL, error = ? WHERE id = ?`,
			DeliveryFailed, attempts, first, ts(now), err.Error(), d.id)
	default:
		next := now.Add(retryAfter[attempts-1])
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, attempts = ?, first_attempt_at = ?, last_attempt_at = ?, next_attempt_at = ?, error = ? WHERE id = ?`,
			DeliveryPending, attempts, first, ts(now), ts(next), err.Error(), d.id)
	}
}

// send makes one attempt on a channel or destination.
func (h *Hub) send(ctx context.Context, n Notification, channel, destinationID string) error {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if destinationID == "" {
		ch, ok := h.channels[channel]
		if !ok {
			return PermanentError{Err: fmt.Errorf("channel %q is not available", channel)}
		}
		err := ch.Deliver(dctx, h.localized(ctx, n))
		if err == ErrSuppressed {
			return PermanentError{Err: err}
		}
		return err
	}
	dest, err := h.Destination(ctx, destinationID)
	if err != nil {
		return PermanentError{Err: fmt.Errorf("the destination was removed")}
	}
	if !dest.Enabled {
		return PermanentError{Err: fmt.Errorf("the destination is turned off")}
	}
	return h.sendTo(dctx, dest, h.localized(ctx, n))
}

// sendTo delivers to a destination and records what left this computer.
func (h *Hub) sendTo(ctx context.Context, dest Destination, n Notification) error {
	secret := ""
	if h.secrets != nil {
		secret, _ = h.secrets.Read(secretName(dest.ID))
	}
	var err error
	host := ""
	switch dest.Kind {
	case KindWebhook:
		if u, perr := url.Parse(dest.Webhook.URL); perr == nil {
			host = u.Host
		}
		err = sendWebhook(ctx, h.client, *dest.Webhook, secret, n, h.now())
	case KindEmail:
		host = net.JoinHostPort(dest.Email.Host, strconv.Itoa(dest.Email.Port))
		err = sendEmail(ctx, *dest.Email, secret, n, h.now())
	case KindNtfy:
		if u, perr := url.Parse(dest.Ntfy.Server); perr == nil {
			host = u.Host
		}
		err = sendNtfy(ctx, h.client, *dest.Ntfy, secret, n)
	default:
		return PermanentError{Err: fmt.Errorf("unknown destination kind %q", dest.Kind)}
	}
	if h.egress != nil && host != "" {
		h.egress.Add(ctx, EgressKind, host, dest.Name+": "+n.Title)
	}
	return err
}

// TestDestination sends a test notification to a destination now, and
// returns the error, if any, in plain words.
func (h *Hub) TestDestination(ctx context.Context, id string) error {
	dest, err := h.Destination(ctx, id)
	if err != nil {
		return err
	}
	now := h.now().UTC()
	n := Notification{ID: "test-" + newID(), CreatedAt: now, SourceType: "test", Category: CategorySystem, Severity: SeverityInfo,
		Message: &locale.Message{
			Title: locale.Key("notifications:notices.test", nil),
			Body:  []locale.Text{locale.Key("notifications:notices.testBody", map[string]any{"destination": dest.Name})},
		}}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return h.sendTo(tctx, dest, h.localized(tctx, n))
}
