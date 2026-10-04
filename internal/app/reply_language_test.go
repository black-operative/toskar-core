package app

import (
	"context"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"strings"
	"testing"
)

// The answer's language (spec §11): a request wins, then the setting's
// mode decides, and the conversation fills in for short messages.
func TestReplyLanguageInTurnInstructions(t *testing.T) {
	t.Setenv("LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	a, conv := memoryApp(t)
	ctx := context.Background()
	env := &chatExecEnv{app: a, conversationID: conv}
	instructions := func(message string) string {
		env.turnPrompt = message
		return env.TurnInstructions(ctx, "a worker's prompt, written by the planner in English")
	}
	set := func(k, v string) {
		if err := a.Settings.Set(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}

	// Auto, the default: the language the person writes in.
	if got := instructions("Wie hoch ist die Zugspitze eigentlich genau?"); !strings.Contains(got, "Write your reply in German") {
		t.Fatalf("auto, German message:\n%s", got)
	}
	// A short message takes the conversation's language.
	if _, err := a.Conversations.AddMessage(ctx, conv, "user", "Quelle est la plus haute montagne de France ?"); err != nil {
		t.Fatal(err)
	}
	if got := instructions("ok, et après ?"); !strings.Contains(got, "in French") {
		t.Fatalf("auto, short message in a French conversation:\n%s", got)
	}
	// An explicit request wins in every mode.
	set("assistant_language_mode", "language")
	set("assistant_language", "es")
	if got := instructions("What is the tallest mountain in Europe? Answer in Japanese."); !strings.Contains(got, "in Japanese") {
		t.Fatalf("request:\n%s", got)
	}
	// A chosen language is used even when the person writes in another.
	if got := instructions("What is the tallest mountain in Europe?"); !strings.Contains(got, "in Spanish") {
		t.Fatalf("chosen language:\n%s", got)
	}
	// Same as app.
	set("assistant_language_mode", "app")
	set("ui_locale", "ko")
	if got := instructions("What is the tallest mountain in Europe?"); !strings.Contains(got, "in Korean") {
		t.Fatalf("same as app:\n%s", got)
	}
}

func TestAssistantLanguageSettings(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ctx := context.Background()
	view, err := a.settingsView(ctx)
	if err != nil || view.AssistantLanguageMode != "auto" || view.AssistantLanguage != "" {
		t.Fatalf("default view %q %q, %v", view.AssistantLanguageMode, view.AssistantLanguage, err)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"assistant_language_mode": "language", "assistant_language": "pt-BR"}); err != nil {
		t.Fatal(err)
	}
	mode, _ := a.Settings.GetString(ctx, "assistant_language_mode", "")
	lang, _ := a.Settings.GetString(ctx, "assistant_language", "")
	if mode != "language" || lang != "pt-BR" {
		t.Fatalf("saved %q %q", mode, lang)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"assistant_language_mode": "sometimes"}); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"assistant_language": "Portuguese!"}); err == nil {
		t.Fatal("a language that isn't a tag was accepted")
	}
}

func TestSystemLanguage(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	if got := systemLanguage(); got != "de_DE" {
		t.Errorf("LANG=de_DE.UTF-8 → %q", got)
	}
	t.Setenv("LANG", "C.UTF-8")
	if got := systemLanguage(); got != "" {
		t.Errorf("LANG=C.UTF-8 → %q", got)
	}
}

// An automation's results come in its response language (spec §22), which
// stands in for the assistant language setting.
func TestAutomationResponseLanguage(t *testing.T) {
	t.Setenv("LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	a, _ := memoryApp(t)
	ctx := context.Background()
	for k, v := range map[string]string{"assistant_language_mode": "language", "assistant_language": "es", "ui_locale": "ja"} {
		if err := a.Settings.Set(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	run := func(response, prompt string) string {
		env := &chatExecEnv{app: a, turnPrompt: prompt, responseLanguage: response}
		return env.TurnInstructions(ctx, prompt)
	}
	const german = "Prüfe jeden Morgen den Preis von diesem Laptop und sag mir, ob er gefallen ist."
	cases := []struct{ response, prompt, want string }{
		{"", german, "in Spanish"},        // same as account: the assistant language setting
		{"account", german, "in Spanish"}, // the same, said outright
		{"app", german, "in Japanese"},    // the App language
		{"auto", german, "in German"},     // the request's language
		{"fr", german, "in French"},       // a language
		{"fr", "Check the price every morning and answer in Korean.", "in Korean"}, // a request wins
	}
	for _, c := range cases {
		if got := run(c.response, c.prompt); !strings.Contains(got, c.want) {
			t.Errorf("response %q: want %q in\n%s", c.response, c.want, got)
		}
	}
}

// The warning that a model may write a language less well is in the App
// language (multilingual spec §16).
func TestLanguageWeakNotice(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	m := contracts.Model{ID: "gemma", DisplayName: "Gemma 2 9B"}
	if got := a.languageWeakNotice(ctx, m, "ja"); !strings.Contains(got, "Gemma 2 9B may write Japanese less well") {
		t.Fatalf("English notice = %q", got)
	}
	if err := a.Settings.Set(ctx, "ui_locale", "de"); err != nil {
		t.Fatal(err)
	}
	if got := a.languageWeakNotice(ctx, m, "ja"); !strings.Contains(got, "Japanisch") || strings.Contains(got, "may write") {
		t.Fatalf("German notice = %q", got)
	}
}
