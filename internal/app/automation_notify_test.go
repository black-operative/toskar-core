package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestAutomationNotifierHonorsTaskSetting(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	settings := repositories.NewSettingsRepo(db.SQL)
	ctx := context.Background()
	sent := 0
	n := automationNotifier{
		settings: settings,
		send: noticeFunc(func(context.Context, automations.Notice) error {
			sent++
			return nil
		}),
	}
	if err := n.Notify(ctx, automations.Notice{Title: "Morning price", Body: "The laptop is $420."}); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d", sent)
	}
	if err := settings.SetBool(ctx, "notify_task_finish", false); err != nil {
		t.Fatal(err)
	}
	err = n.Notify(ctx, automations.Notice{Title: "Morning price", Body: "The laptop is $420."})
	if !errors.Is(err, automations.ErrNotifyDisabled) {
		t.Fatalf("disabled notify err = %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent while notifications are off = %d", sent)
	}
}

type noticeFunc func(context.Context, automations.Notice) error

func (f noticeFunc) Notify(ctx context.Context, notice automations.Notice) error {
	return f(ctx, notice)
}

func TestAutomationNoticesGoToTheNotificationCenter(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := repositories.NewSettingsRepo(db.SQL)
	ctx := context.Background()
	sent := 0
	desktop := desktopChannel{settings: settings, send: noticeFunc(func(context.Context, automations.Notice) error { sent++; return nil })}
	hub := gjallarhorn.NewHub(db.SQL, events.NewBus(8), desktop)
	n := automationNotifier{settings: settings, hub: hub}

	if err := n.Notify(ctx, automations.Notice{Title: "Morning price", Body: "The laptop is $420.", AutomationID: "a1"}); err != nil {
		t.Fatal(err)
	}
	// Desktop notices off: still kept in the notification center.
	_ = settings.SetBool(ctx, "notify_task_finish", false)
	if err := n.Notify(ctx, automations.Notice{Title: "Morning price", Body: "Could not run.", AutomationID: "a1", Failure: true}); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("desktop notices sent = %d", sent)
	}
	list, unread, _ := hub.List(ctx, false, 0)
	if len(list) != 2 || unread != 2 || list[0].Severity != gjallarhorn.SeverityError || list[0].Link != "/automations?id=a1" {
		t.Fatalf("center = %+v", list)
	}
	got, _ := hub.Get(ctx, list[0].ID)
	if len(got.Deliveries) != 1 || got.Deliveries[0].Status != gjallarhorn.DeliverySuppressed {
		t.Fatalf("deliveries = %+v", got.Deliveries)
	}
}

// rendered is a notice's title and body in lang.
func rendered(req gjallarhorn.Request, lang string) (title, body string) {
	if req.Message == nil {
		return req.Title, req.Body
	}
	return req.Message.Render(lang)
}

func TestEventsBecomeNotifications(t *testing.T) {
	a := &App{}
	req, ok := noticeForEvent(a, events.New(events.ModelDownloadCompleted, map[string]any{"model_id": "llama-1b"}))
	if title, _ := rendered(req, "en"); !ok || title != "Model ready" || req.Category != gjallarhorn.CategoryModel || req.Link != "/models" {
		t.Fatalf("download = %+v", req)
	}
	// The same notice in the App language, wherever it is shown (§22).
	if title, body := rendered(req, "de"); title == "Model ready" || !strings.Contains(body, "llama-1b") {
		t.Fatalf("German download notice = %q / %q", title, body)
	}
	if _, ok := noticeForEvent(a, events.New(events.ChatToken, nil)); ok {
		t.Fatal("chat tokens are not notifications")
	}
}

func TestDesktopNoticesGoToTheDesktopAppWhenItPostsThem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := repositories.NewSettingsRepo(db.SQL)
	ctx := context.Background()
	bus := events.NewBus(16)
	_, stream := bus.Subscribe()
	ranOSA := 0
	desktop := desktopChannel{
		settings: settings,
		send:     noticeFunc(func(context.Context, automations.Notice) error { ranOSA++; return nil }),
		toShell:  shellNotices(bus, "shell"),
	}
	hub := gjallarhorn.NewHub(db.SQL, bus, desktop)
	n := automationNotifier{settings: settings, hub: hub}
	if err := n.Notify(ctx, automations.Notice{Title: "Morning price", Body: "The laptop is $420.", AutomationID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if ranOSA != 0 {
		t.Fatalf("the daemon also posted the notice itself (%d)", ranOSA)
	}
	var got *events.Event
	for len(stream) > 0 {
		evt := <-stream
		if evt.Type == gjallarhorn.EventDesktop {
			got = &evt
		}
	}
	if got == nil || got.Payload["title"] != "Morning price" || got.Payload["body"] != "The laptop is $420." || got.Payload["link"] != "/automations?id=a1" || got.Payload["id"] == "" {
		t.Fatalf("desktop event = %+v", got)
	}
	list, _, _ := hub.List(ctx, false, 0)
	stored, _ := hub.Get(ctx, list[0].ID)
	if d := stored.Deliveries; len(d) != 1 || d[0].Status != gjallarhorn.DeliveryDelivered {
		t.Fatalf("deliveries = %+v", d)
	}

	// Desktop notices off: nothing goes to the app either.
	_ = settings.SetBool(ctx, "notify_task_finish", false)
	_ = n.Notify(ctx, automations.Notice{Title: "Evening price", Body: "Same.", AutomationID: "a1"})
	for len(stream) > 0 {
		if evt := <-stream; evt.Type == gjallarhorn.EventDesktop {
			t.Fatalf("sent with desktop notices off: %+v", evt)
		}
	}

	// Without the setting the daemon posts them itself, as before.
	if shellNotices(bus, "") != nil || shellNotices(nil, "shell") != nil {
		t.Fatal("expected no hand-off")
	}
}

// A result posted to the chat an automation came from is an answer there,
// marked with its automation, and its notice opens that chat (#204).
func TestAutomationResultInItsChat(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	convs := repositories.NewConversationRepo(db.SQL)
	conv, err := convs.Create(ctx, "Morning news", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Conversations: convs}
	automation := automations.Automation{ID: "a1", Name: "Morning news", ConversationID: conv.ID}
	if err := a.postAutomationResult(ctx, automation, automations.Run{ID: "r1"}, "Three stories today."); err != nil {
		t.Fatal(err)
	}
	messages, _ := convs.ListMessages(ctx, conv.ID)
	if len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Meta == nil || messages[0].Meta.AutomationRun == nil ||
		messages[0].Meta.AutomationRun.RunID != "r1" || messages[0].Meta.AutomationRun.Name != "Morning news" {
		t.Fatalf("messages = %+v", messages)
	}
	if err := a.postAutomationResult(ctx, automations.Automation{ID: "a1", ConversationID: "deleted"}, automations.Run{ID: "r2"}, "x"); err == nil {
		t.Fatal("posted to a chat that doesn't exist")
	}

	hub := gjallarhorn.NewHub(db.SQL, events.NewBus(8))
	n := automationNotifier{hub: hub}
	if err := n.Notify(ctx, automations.Notice{Title: "Morning news", Body: "Three stories today.", AutomationID: "a1", ConversationID: conv.ID}); err != nil {
		t.Fatal(err)
	}
	list, _, _ := hub.List(ctx, false, 0)
	if len(list) != 1 || list[0].Link != "/chat?c="+conv.ID {
		t.Fatalf("center = %+v", list)
	}
}
