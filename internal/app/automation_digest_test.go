package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// The digest goes out once a day at its time, with every automation's
// latest result and failures, in its own chat, and says nothing when
// nothing ran (#204).
func TestAutomationDigest(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := repositories.NewAutomationRepo(db.SQL)
	convs := repositories.NewConversationRepo(db.SQL)
	settings := repositories.NewSettingsRepo(db.SQL)
	var notices []automations.Notice
	a := &App{Automations: repo, Conversations: convs, Settings: settings, AutomationRunner: &automations.Runner{
		Notify: noticeFunc(func(_ context.Context, n automations.Notice) error { notices = append(notices, n); return nil }),
	}}
	_ = settings.Set(ctx, settingDigest, "08:00")
	_ = settings.Set(ctx, settingDigestZone, "UTC")

	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	add := func(name string) automations.Automation {
		created, err := repo.Create(ctx, automations.CreateInput{
			Name: name, Prompt: "x", ModelID: "auto",
			Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 6},
		}, day(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	news, price := add("Morning news"), add("Price watch")
	finish := func(a automations.Automation, at time.Time, text, failure string) {
		run, _, err := repo.Claim(ctx, a.ID, at, at, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if failure != "" {
			err = repo.FailRun(ctx, run.ID, failure, automations.Execution{}, at)
		} else {
			err = repo.CompleteRun(ctx, run.ID, automations.Execution{Text: text}, at)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	finish(news, day(6, 20), "Old news.", "")
	finish(news, day(7, 6), "Three stories today.", "")
	finish(price, day(7, 7), "", "the shop timed out")

	// Turned on at 07:00: it waits for 08:00 rather than sending at once.
	if err := a.sendDigestIfDue(ctx, day(7, 7)); err != nil || len(notices) != 0 {
		t.Fatalf("early: %v, %d notices", err, len(notices))
	}
	if err := a.sendDigestIfDue(ctx, day(7, 7).Add(30*time.Minute)); err != nil || len(notices) != 0 {
		t.Fatalf("still early: %v, %d notices", err, len(notices))
	}
	if err := a.sendDigestIfDue(ctx, day(7, 9)); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].ConversationID == "" || !strings.Contains(notices[0].Body, "2 automations ran: Morning news, Price watch") {
		t.Fatalf("notices = %+v", notices)
	}
	messages, _ := convs.ListMessages(ctx, notices[0].ConversationID)
	if len(messages) != 1 {
		t.Fatalf("%d messages", len(messages))
	}
	body := messages[0].Content
	for _, want := range []string{"### Morning news", "Three stories today.", "### Price watch", "Failed once: the shop timed out"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Old news.") {
		t.Errorf("digest shows an older result:\n%s", body)
	}

	// Once a day: again the same day sends nothing.
	if err := a.sendDigestIfDue(ctx, day(7, 15)); err != nil || len(notices) != 1 {
		t.Fatalf("again: %v, %d notices", err, len(notices))
	}
	// The next day, the same chat.
	finish(news, day(8, 6), "Two stories.", "")
	if err := a.sendDigestIfDue(ctx, day(8, 8)); err != nil || len(notices) != 2 || notices[1].ConversationID != notices[0].ConversationID {
		t.Fatalf("next day: %v, %+v", err, notices)
	}
	// Nothing ran: nothing sent.
	if err := a.sendDigestIfDue(ctx, day(9, 8)); err != nil || len(notices) != 2 {
		t.Fatalf("quiet day: %v, %d notices", err, len(notices))
	}
	// Off.
	_ = settings.Set(ctx, settingDigest, "")
	finish(news, day(9, 9), "More.", "")
	if err := a.sendDigestIfDue(ctx, day(10, 9)); err != nil || len(notices) != 2 {
		t.Fatalf("off: %v, %d notices", err, len(notices))
	}
}
