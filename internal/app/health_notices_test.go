package app

import (
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
)

func healthEvent(typ string, payload map[string]any) events.Event { return events.New(typ, payload) }

// A computer is announced when it goes offline and when it comes back, once
// per change; one that was never announced offline is not announced back
// (Gjallarhorn §23).
func TestComputerHealthNotifiesOnChanges(t *testing.T) {
	h := newHealthNotices()
	name := func(id string) string { return id }
	off := healthEvent(events.NodeOffline, map[string]any{"node_id": "n1", "name": "Studio"})
	on := healthEvent(events.NodeOnline, map[string]any{"node_id": "n1", "name": "Studio"})

	if _, ok := h.notice(on, name, true); ok {
		t.Fatal("a computer that was never offline was announced back")
	}
	req, ok := h.notice(off, name, true)
	if title, _ := rendered(req, "en"); !ok || title != "Studio went offline" || req.Category != gjallarhorn.CategoryHealth || req.Severity != gjallarhorn.SeverityWarning {
		t.Fatalf("offline = %+v %v", req, ok)
	}
	if _, ok := h.notice(off, name, true); ok {
		t.Fatal("the same offline state was announced twice")
	}
	if req, ok := h.notice(on, name, true); !ok || title(req) != "Studio is back online" || req.Severity != gjallarhorn.SeveritySuccess {
		t.Fatalf("back = %+v %v", req, ok)
	}
	if _, ok := h.notice(off, name, false); ok {
		t.Fatal("announced with offline alerts turned off")
	}
	if _, ok := h.notice(on, name, true); ok {
		t.Fatal("announced back without an offline notice")
	}
}

// A model is announced when it keeps crashing, not on every crash, and not
// again for an hour (§29).
func TestModelCrashingNotifiesWhenRepeated(t *testing.T) {
	h := newHealthNotices()
	clock := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return clock }
	name := func(id string) string { return "Qwen 2.5 7B" }
	crash := healthEvent(events.ModelHealthFailed, map[string]any{"model_id": "qwen", "likely_memory_pressure": true})

	if _, ok := h.notice(crash, name, true); ok {
		t.Fatal("one crash was announced")
	}
	clock = clock.Add(40 * time.Minute)
	if _, ok := h.notice(crash, name, true); ok {
		t.Fatal("crashes 40 minutes apart count as repeated")
	}
	clock = clock.Add(5 * time.Minute)
	req, ok := h.notice(crash, name, true)
	if !ok || title(req) != "Qwen 2.5 7B keeps crashing" || req.Severity != gjallarhorn.SeverityError {
		t.Fatalf("crashing = %+v %v", req, ok)
	}
	if _, body := rendered(req, "en"); !strings.Contains(body, "running out of memory") || !strings.Contains(body, "stopped 2 times") {
		t.Fatalf("body %q lacks the crash count or the memory hint", body)
	}
	clock = clock.Add(10 * time.Minute)
	if _, ok := h.notice(crash, name, true); ok {
		t.Fatal("announced again within the hour")
	}
	clock = clock.Add(time.Hour)
	_, _ = h.notice(crash, name, true)
	clock = clock.Add(time.Minute)
	if _, ok := h.notice(crash, name, true); !ok {
		t.Fatal("not announced again after the hour")
	}
}

func title(req gjallarhorn.Request) string {
	t, _ := rendered(req, "en")
	return t
}
