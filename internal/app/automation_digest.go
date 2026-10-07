package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The automation digest (#204): once a day, at the time the person chose,
// one answer in an "Automation digest" chat says what every automation
// found since the last one, and one notification opens it. It's put
// together from the results, with no model, so it's quick and always the
// same shape.
const (
	settingDigest             = "automation_digest"
	settingDigestZone         = "automation_digest_zone"
	settingDigestLast         = "automation_digest_last"
	settingDigestConversation = "automation_digest_conversation"
)

// validDigestTime is "" (off) or a time of day such as 08:00.
func validDigestTime(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

// digestLoop checks once a minute whether the digest is due.
func (a *App) digestLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := a.sendDigestIfDue(ctx, time.Now()); err != nil && a.Logger != nil {
			a.Logger.Warn("automation digest", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sendDigestIfDue sends the digest when today's time has passed and it
// hasn't gone out since. A computer that was off at the time sends it once
// when it's back.
func (a *App) sendDigestIfDue(ctx context.Context, now time.Time) error {
	if a.Settings == nil || a.Automations == nil || a.Conversations == nil {
		return nil
	}
	at, _ := a.Settings.GetString(ctx, settingDigest, "")
	if at == "" {
		return nil
	}
	clock, err := time.Parse("15:04", at)
	if err != nil {
		return nil
	}
	zone, _ := a.Settings.GetString(ctx, settingDigestZone, "")
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		loc = time.Local
	}
	local := now.In(loc)
	slot := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	if now.Before(slot) {
		slot = slot.AddDate(0, 0, -1)
	}
	lastText, _ := a.Settings.GetString(ctx, settingDigestLast, "")
	last, err := time.Parse(time.RFC3339, lastText)
	if err != nil {
		// Just turned on: the first digest goes out at the next time, with
		// the day before it, rather than at once.
		return a.Settings.Set(ctx, settingDigestLast, slot.UTC().Format(time.RFC3339))
	}
	if !last.Before(slot) {
		return nil
	}
	if err := a.sendDigest(ctx, last, now); err != nil {
		return err
	}
	return a.Settings.Set(ctx, settingDigestLast, now.UTC().Format(time.RFC3339))
}

// digestEntry is one automation's part of the digest.
type digestEntry struct {
	name      string
	result    string
	succeeded int
	failed    int
	lastError string
}

// sendDigest posts what ran between since and now, and notifies. With
// nothing to say, it says nothing.
func (a *App) sendDigest(ctx context.Context, since, now time.Time) error {
	runs, err := a.Automations.FinishedSince(ctx, since)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return nil
	}
	entries := map[string]*digestEntry{}
	var order []string
	for _, run := range runs {
		e, ok := entries[run.AutomationID]
		if !ok {
			automation, err := a.Automations.Get(ctx, run.AutomationID)
			if err != nil {
				continue
			}
			e = &digestEntry{name: automation.Name}
			entries[run.AutomationID] = e
			order = append(order, run.AutomationID)
		}
		if run.Status == automations.RunSucceeded {
			e.succeeded++
			if text := automations.ResultProse(run.Result); text != "" {
				e.result = text
			}
		} else {
			e.failed++
			e.lastError = run.Error
		}
	}
	if len(order) == 0 {
		return nil
	}
	lang := a.appLanguage(ctx)
	var b strings.Builder
	b.WriteString(locale.T(lang, "automations:digest.intro", map[string]any{"count": len(order)}))
	for _, id := range order {
		e := entries[id]
		fmt.Fprintf(&b, "\n\n### %s\n\n", e.name)
		if e.result != "" {
			b.WriteString(e.result)
		} else {
			b.WriteString(locale.T(lang, "automations:digest.noResult", nil))
		}
		if e.failed > 0 {
			b.WriteString("\n\n_" + locale.T(lang, "automations:digest.failed", map[string]any{"count": e.failed, "error": oneLineText(e.lastError, 160)}) + "_")
		}
	}
	conversationID, err := a.digestConversation(ctx, lang)
	if err != nil {
		return err
	}
	if _, err := a.Conversations.AddMessageWithMeta(ctx, conversationID, "assistant", b.String(), &contracts.MessageMeta{Contract: contracts.ContractVersion}); err != nil {
		return err
	}
	if a.AutomationRunner == nil || a.AutomationRunner.Notify == nil {
		return nil
	}
	names := make([]string, 0, len(order))
	for _, id := range order {
		names = append(names, entries[id].name)
	}
	message := locale.Message{
		Title: locale.Key("notifications:notices.automationDigest", nil),
		Body:  []locale.Text{locale.Key("notifications:notices.automationDigestBody", map[string]any{"count": len(order), "names": oneLineText(strings.Join(names, ", "), 160)})},
	}
	title, body := message.Render(locale.Source)
	return a.AutomationRunner.Notify.Notify(ctx, automations.Notice{Title: title, Body: body, Message: &message, ConversationID: conversationID})
}

// digestConversation is the digest's chat, made the first time and again
// when it was deleted.
func (a *App) digestConversation(ctx context.Context, lang string) (string, error) {
	if id, _ := a.Settings.GetString(ctx, settingDigestConversation, ""); id != "" {
		if _, err := a.Conversations.Get(ctx, id); err == nil {
			return id, nil
		}
	}
	conv, err := a.Conversations.Create(ctx, locale.T(lang, "automations:digest.title", nil), "general-assistant", "auto")
	if err != nil {
		return "", err
	}
	return conv.ID, a.Settings.Set(ctx, settingDigestConversation, conv.ID)
}

func oneLineText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max])) + "…"
	}
	return s
}
