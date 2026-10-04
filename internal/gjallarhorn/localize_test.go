package gjallarhorn

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/locale"
)

type recordingChannel struct {
	mu   sync.Mutex
	got  []Notification
	name string
}

func (c *recordingChannel) Name() string { return c.name }

func (c *recordingChannel) Deliver(_ context.Context, n Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, n)
	return nil
}

func modelReady() *locale.Message {
	return &locale.Message{
		Title: locale.Key("notifications:notices.modelReady", nil),
		Body:  []locale.Text{locale.Key("notifications:notices.modelReadyBody", map[string]any{"model": "Qwen 2.5 7B"})},
	}
}

// A notice is stored with its message and English text, and written in the
// App language where it leaves the app: desktop notices, email, push, and
// webhooks (multilingual spec §22).
func TestNoticesAreWrittenInTheAppLanguage(t *testing.T) {
	ctx := context.Background()
	hub, _ := newHub(t)
	desktop := &recordingChannel{name: "desktop"}
	hub.channels["desktop"] = desktop
	settings := &memSettings{m: map[string]string{"ui_locale": "de"}}
	hub.SetSettings(settings)

	n, err := hub.Notify(ctx, Request{Category: CategoryModel, Severity: SeveritySuccess, Message: modelReady(), Channels: []string{"desktop"}})
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Model ready" || n.Body != "Qwen 2.5 7B finished downloading and is ready to use." {
		t.Fatalf("stored English text = %q / %q", n.Title, n.Body)
	}
	stored, err := hub.Get(ctx, n.ID)
	if err != nil || stored.Message == nil || stored.Message.Title.Key != "notifications:notices.modelReady" {
		t.Fatalf("stored message = %+v %v", stored.Message, err)
	}

	if len(desktop.got) != 1 {
		t.Fatalf("desktop got %d notices", len(desktop.got))
	}
	german := desktop.got[0]
	if german.Title == "Model ready" || german.Title == "" || !strings.Contains(german.Body, "Qwen 2.5 7B") {
		t.Fatalf("desktop notice is not in German: %q / %q", german.Title, german.Body)
	}

	// Email, push, and webhooks write their own words in the App language too.
	german.RepeatCount = 3
	email := string(emailMessage(EmailConfig{From: "y@example.com", To: []string{"me@example.com"}}, german, hub.now()))
	if strings.Contains(email, "Sent by Toskar") || !strings.Contains(email, "Toskar") {
		t.Fatalf("email footer is not in German:\n%s", email)
	}
	push := ntfyMessageFor(NtfyConfig{Topic: "t"}, german)
	if push.Message == "You have a new Toskar notification." {
		t.Fatalf("private push text is not in German: %q", push.Message)
	}
	payload := payloadFor(german)
	if payload.Language != "de" || payload.Message == nil || payload.Title != german.Title {
		t.Fatalf("webhook payload = %+v", payload)
	}

	// With the system language, text that leaves the app is in English.
	settings.m["ui_locale"] = ""
	if got := hub.localized(ctx, stored); got.Title != "Model ready" || payloadFor(got).Language != "en" {
		t.Fatalf("system language = %q, %q", got.Title, payloadFor(got).Language)
	}
}

// A notice written as text, such as an automation's result, keeps its text.
func TestTextNoticesStayAsTheyAre(t *testing.T) {
	ctx := context.Background()
	hub, _ := newHub(t)
	hub.SetSettings(&memSettings{m: map[string]string{"ui_locale": "ja"}})
	n, err := hub.Notify(ctx, Request{Title: "Morning price", Body: "The laptop is $420."})
	if err != nil {
		t.Fatal(err)
	}
	if got := hub.localized(ctx, n); got.Title != "Morning price" || got.Body != "The laptop is $420." {
		t.Fatalf("text notice = %q / %q", got.Title, got.Body)
	}
}
