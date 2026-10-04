package gjallarhorn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
)

// Digest gathers a destination's notices into one message a day (#111).
// Errors still go out at once.
type Digest struct {
	// At is HH:MM in TimeZone.
	At       string `json:"at"`
	TimeZone string `json:"time_zone"`
}

// Validate checks a digest's time and zone.
func (g Digest) Validate() error {
	if _, ok := parseClock(g.At); !ok {
		return fmt.Errorf("digest at must be HH:MM")
	}
	if _, err := time.LoadLocation(g.TimeZone); err != nil {
		return fmt.Errorf("unknown time zone %q", g.TimeZone)
	}
	return nil
}

// Next is the digest's next time after now, in UTC.
func (g Digest) Next(now time.Time) time.Time {
	loc, err := time.LoadLocation(g.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	at, _ := parseClock(g.At)
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), at/60, at%60, 0, 0, loc)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next.UTC()
}

// digestMax is how many notices a digest lists; the rest are counted.
const digestMax = 30

type digestRow struct {
	id, notificationID string
	attempts           int
}

// deliverDigests sends each destination's digest whose time has come: one
// message listing its waiting notices, newest last.
func (h *Hub) deliverDigests(ctx context.Context) {
	now := h.now().UTC()
	rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT destination_id FROM notification_deliveries WHERE status = ? AND next_attempt_at <= ? AND destination_id IS NOT NULL`,
		DeliveryDigest, ts(now))
	if err != nil {
		return
	}
	var dests []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			dests = append(dests, id)
		}
	}
	rows.Close()
	for _, id := range dests {
		if ctx.Err() != nil {
			return
		}
		h.deliverDigest(ctx, id, now)
	}
}

func (h *Hub) deliverDigest(ctx context.Context, destID string, now time.Time) {
	// A digest that falls in quiet hours waits for them to end.
	if until, held := h.QuietHours(ctx).HeldUntil(SeverityInfo, now); held {
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET next_attempt_at = ? WHERE destination_id = ? AND status = ? AND next_attempt_at <= ?`,
			ts(until), destID, DeliveryDigest, ts(now))
		return
	}
	rows, err := h.db.QueryContext(ctx, `SELECT d.id, d.notification_id, d.attempts FROM notification_deliveries d JOIN notifications n ON n.id = d.notification_id
		WHERE d.destination_id = ? AND d.status = ? AND d.next_attempt_at <= ? ORDER BY n.created_at`, destID, DeliveryDigest, ts(now))
	if err != nil {
		return
	}
	var list []digestRow
	for rows.Next() {
		var r digestRow
		if rows.Scan(&r.id, &r.notificationID, &r.attempts) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	dest, err := h.Destination(ctx, destID)
	if err != nil || !dest.Enabled {
		h.settleDigest(ctx, list, DeliveryCancelled, now, "The destination was removed or turned off.")
		return
	}
	lang := h.language(ctx)
	lines := make([]string, 0, min(len(list), digestMax)+1)
	for i, r := range list {
		if i == digestMax {
			lines = append(lines, locale.T(lang, "notifications:notices.digestMore", map[string]any{"count": len(list) - digestMax}))
			break
		}
		n, err := h.Get(ctx, r.notificationID)
		if err != nil {
			continue
		}
		n = h.localized(ctx, n)
		line := "• " + n.Title
		if n.Body != "" {
			line += ": " + firstLine(n.Body)
		}
		lines = append(lines, line)
	}
	digest := Notification{ID: "digest-" + newID(), CreatedAt: now, SourceType: "digest", Category: CategorySystem, Severity: SeverityInfo, lang: lang,
		Title: locale.T(lang, "notifications:notices.digest", map[string]any{"count": len(list)}),
		Body:  strings.Join(lines, "\n")}
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = h.sendTo(sctx, dest, digest)
	attempts := list[0].attempts + 1
	switch {
	case err == nil:
		h.settleDigest(ctx, list, DeliveryDelivered, now, "")
	case IsPermanent(err) || attempts > len(retryAfter):
		h.settleDigest(ctx, list, DeliveryFailed, now, err.Error())
	default:
		next := now.Add(retryAfter[attempts-1])
		for _, r := range list {
			_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET attempts = ?, last_attempt_at = ?, next_attempt_at = ?, error = ? WHERE id = ?`,
				attempts, ts(now), ts(next), err.Error(), r.id)
		}
	}
}

// settleDigest marks a digest's deliveries delivered, failed, or cancelled.
func (h *Hub) settleDigest(ctx context.Context, list []digestRow, status string, now time.Time, msg string) {
	for _, r := range list {
		if status == DeliveryDelivered {
			_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, attempts = attempts + 1, last_attempt_at = ?, delivered_at = ?, next_attempt_at = NULL, error = NULL WHERE id = ?`,
				status, ts(now), ts(now), r.id)
			continue
		}
		_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, last_attempt_at = ?, next_attempt_at = NULL, error = ? WHERE id = ?`,
			status, ts(now), nullable(msg), r.id)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}
