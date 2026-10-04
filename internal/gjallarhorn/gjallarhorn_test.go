package gjallarhorn

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
)

type fakeChannel struct {
	name string
	err  error
	got  []Notification
}

func (c *fakeChannel) Name() string { return c.name }
func (c *fakeChannel) Deliver(_ context.Context, n Notification) error {
	c.got = append(c.got, n)
	return c.err
}

func newHub(t *testing.T, channels ...Channel) (*Hub, *events.Bus) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.NewBus(16)
	return NewHub(db.SQL, bus, channels...), bus
}

func TestNotifyStoresAnnouncesAndDelivers(t *testing.T) {
	desktop := &fakeChannel{name: "desktop"}
	broken := &fakeChannel{name: "email", err: errors.New("smtp down")}
	quiet := &fakeChannel{name: "push", err: ErrSuppressed}
	hub, bus := newHub(t, desktop, broken, quiet)
	subID, sub := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	ctx := context.Background()

	n, err := hub.Notify(ctx, Request{SourceType: "automation", SourceID: "a1", Category: CategoryAutomation, Severity: SeveritySuccess,
		Title: "Daily report", Body: "Ready.", Link: "/automations?id=a1", Channels: []string{"desktop", "email", "push"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(desktop.got) != 1 || desktop.got[0].Title != "Daily report" {
		t.Fatal("desktop delivery")
	}
	select {
	case evt := <-sub:
		if evt.Type != EventCreated || evt.Payload["title"] != "Daily report" {
			t.Fatalf("event = %+v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	// A failed delivery does not erase the notification.
	got, err := hub.Get(ctx, n.ID)
	if err != nil || got.Link != "/automations?id=a1" || len(got.Deliveries) != 3 {
		t.Fatalf("stored = %+v %v", got, err)
	}
	status := map[string]string{}
	for _, d := range got.Deliveries {
		status[d.Channel] = d.Status
	}
	if status["desktop"] != DeliveryDelivered || status["email"] != DeliveryFailed || status["push"] != DeliverySuppressed {
		t.Fatalf("deliveries = %+v", got.Deliveries)
	}
}

func TestDedupeReadAndDismiss(t *testing.T) {
	hub, _ := newHub(t)
	ctx := context.Background()
	a, _ := hub.Notify(ctx, Request{Title: "Node offline", DedupeKey: "node:n1", Body: "first"})
	_ = hub.MarkRead(ctx, []string{a.ID})
	b, _ := hub.Notify(ctx, Request{Title: "Node offline", DedupeKey: "node:n1", Body: "again"})
	if a.ID != b.ID || b.ReadAt != nil || b.Body != "again" {
		t.Fatalf("a repeat folds into the first and is unread again: %+v", b)
	}
	_, _ = hub.Notify(ctx, Request{Title: "Download finished"})
	list, unread, err := hub.List(ctx, false, 0)
	if err != nil || len(list) != 2 || unread != 2 || list[0].Title != "Download finished" {
		t.Fatalf("list = %+v unread=%d %v", list, unread, err)
	}
	if err := hub.MarkRead(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, unread, _ := hub.List(ctx, false, 0); unread != 0 {
		t.Fatal("all read")
	}
	if err := hub.Dismiss(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if list, _, _ := hub.List(ctx, false, 0); len(list) != 1 {
		t.Fatal("dismissed notifications leave the center")
	}
	if _, err := hub.Notify(ctx, Request{}); err == nil {
		t.Fatal("a title is required")
	}
}
