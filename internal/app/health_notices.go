package app

import (
	"context"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/locale"
)

// Model crashes notify only when they repeat (Gjallarhorn §23, §29): one
// crash is handled by answering on another model.
const (
	crashWindow   = 30 * time.Minute
	crashRepeat   = 2
	crashCooldown = time.Hour
)

// healthNotices turns computer and model health into notifications on
// changes only (§23): a computer going offline and coming back, and a model
// that keeps crashing, instead of the same state again and again.
type healthNotices struct {
	mu       sync.Mutex
	now      func() time.Time
	offline  map[string]bool
	crashes  map[string][]time.Time
	notified map[string]time.Time
}

func newHealthNotices() *healthNotices {
	return &healthNotices{now: time.Now, offline: map[string]bool{}, crashes: map[string][]time.Time{}, notified: map[string]time.Time{}}
}

// notice returns the notification for a health event, if it is a change
// worth one. peerAlerts is the "Notify when a paired computer goes offline"
// setting.
func (h *healthNotices) notice(evt events.Event, name func(modelID string) string, peerAlerts bool) (gjallarhorn.Request, bool) {
	str := func(k string) string { v, _ := evt.Payload[k].(string); return v }
	h.mu.Lock()
	defer h.mu.Unlock()
	switch evt.Type {
	case events.NodeOffline:
		id := str("node_id")
		if id == "" || !peerAlerts || h.offline[id] {
			return gjallarhorn.Request{}, false
		}
		h.offline[id] = true
		return gjallarhorn.Request{SourceType: "node", SourceID: id, Category: gjallarhorn.CategoryHealth, Severity: gjallarhorn.SeverityWarning,
			Message: computerNotice("computerOffline", str("name"), "computerOfflineBody"),
			Link:    "/nodes", DedupeKey: "node.offline:" + id}, true
	case events.NodeOnline:
		id := str("node_id")
		if !h.offline[id] {
			// Only a computer that was announced offline is announced back.
			return gjallarhorn.Request{}, false
		}
		delete(h.offline, id)
		return gjallarhorn.Request{SourceType: "node", SourceID: id, Category: gjallarhorn.CategoryHealth, Severity: gjallarhorn.SeveritySuccess,
			Message: computerNotice("computerOnline", str("name"), "computerOnlineBody"),
			Link:    "/nodes", DedupeKey: "node.online:" + id}, true
	case events.ModelHealthFailed:
		model := str("model_id")
		if model == "" {
			return gjallarhorn.Request{}, false
		}
		now := h.now()
		recent := h.crashes[model][:0]
		for _, t := range h.crashes[model] {
			if now.Sub(t) < crashWindow {
				recent = append(recent, t)
			}
		}
		recent = append(recent, now)
		h.crashes[model] = recent
		if len(recent) < crashRepeat || now.Sub(h.notified[model]) < crashCooldown {
			return gjallarhorn.Request{}, false
		}
		h.notified[model] = now
		m := notice("modelCrashing", map[string]any{"model": name(model)},
			"modelCrashingBody", map[string]any{"count": len(recent), "minutes": int(crashWindow.Minutes())})
		if memory, _ := evt.Payload["likely_memory_pressure"].(bool); memory {
			m.Body = append(m.Body, locale.Key("notifications:notices.modelCrashingMemory", nil))
		}
		return gjallarhorn.Request{SourceType: "model", SourceID: model, Category: gjallarhorn.CategoryHealth, Severity: gjallarhorn.SeverityError,
			Message: m, Link: "/models", DedupeKey: "model.crashing:" + model}, true
	}
	return gjallarhorn.Request{}, false
}

// computerNotice is a notice about a paired computer, by name when it has one.
func computerNotice(title, name, body string) *locale.Message {
	if name == "" {
		return notice(title+"Unnamed", nil, body, nil)
	}
	return notice(title, map[string]any{"computer": name}, body, nil)
}

// healthNotice is the notification for a health event, if any.
func (a *App) healthNotice(ctx context.Context, evt events.Event) (gjallarhorn.Request, bool) {
	if a.health == nil {
		return gjallarhorn.Request{}, false
	}
	peerAlerts := true
	if evt.Type == events.NodeOffline && a.Settings != nil {
		peerAlerts, _ = a.Settings.GetBool(ctx, "notify_peer_offline", true)
	}
	return a.health.notice(evt, a.modelName, peerAlerts)
}
