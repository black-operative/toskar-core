package app

import (
	"context"
	"os"
	"strings"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/replylang"
)

// replyLanguage is the language a turn's answer is written in (multilingual
// spec §11–12): a language the message asks for, then, by the assistant
// language setting, the language the person writes in (the message's, or
// the conversation's), a chosen language, or the App language; then this
// computer's language; then English. It is worked out here, never sent
// anywhere to find out.
//
// response is an automation's response language (§22), which stands in for
// the assistant language setting: "" or "account" keeps the setting, "app"
// the App language, "auto" the request's language, or a language tag.
func (a *App) replyLanguage(ctx context.Context, conversationID, message, response string) replylang.Decision {
	in := replylang.Input{Mode: replylang.ModeAuto, Message: message, System: systemLanguage()}
	if a.Settings != nil {
		in.Mode, _ = a.Settings.GetString(ctx, "assistant_language_mode", replylang.ModeAuto)
		in.Setting, _ = a.Settings.GetString(ctx, "assistant_language", "")
		in.App, _ = a.Settings.GetString(ctx, "ui_locale", "")
	}
	switch response {
	case "", automations.ResponseAccount:
	case automations.ResponseApp:
		in.Mode = replylang.ModeApp
	case automations.ResponseAuto:
		in.Mode = replylang.ModeAuto
	default:
		in.Mode, in.Setting = replylang.ModeLanguage, response
	}
	if conversationID != "" && a.Conversations != nil {
		if stored, err := a.Conversations.ListMessages(ctx, conversationID); err == nil {
			for _, m := range stored {
				if m.Role == "user" && strings.TrimSpace(m.Content) != strings.TrimSpace(message) {
					in.History = append(in.History, m.Content)
				}
			}
		}
	}
	return replylang.Resolve(in)
}

// systemLanguage is this computer's language from the environment, such as
// "de_DE" from LANG=de_DE.UTF-8, or "" when it doesn't say.
func systemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(key)
		if v == "" || v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
			continue
		}
		v, _, _ = strings.Cut(v, ".")
		v, _, _ = strings.Cut(v, "@")
		return v
	}
	return ""
}
