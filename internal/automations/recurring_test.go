package automations

import "testing"

func TestRecurring(t *testing.T) {
	for _, text := range []string{
		"Every morning at 8, summarize my email.",
		"check the price every 2 hours",
		"each Monday, list new releases",
		"Fasse jeden Morgen um 8 Uhr meine E-Mails zusammen.",
		"毎朝8時にニュースをまとめて",
		"Todos los días a las 9, revisa el precio.",
	} {
		if !Recurring(text) {
			t.Errorf("%q is recurring", text)
		}
	}
	for _, text := range []string{
		"What's on Monday?",
		"What's the weather tomorrow?",
		"Write a poem about the sea.",
		"",
	} {
		if Recurring(text) {
			t.Errorf("%q isn't recurring", text)
		}
	}
}

func TestRequestLanguage(t *testing.T) {
	for text, want := range map[string]string{
		"Fasse jeden Morgen um 7:30 Uhr die Nachrichten zusammen.": "de",
		"Every morning at 8, summarize my email.":                  "en",
		"毎朝8時にニュースをまとめて":                                           "ja",
		"Write a poem.": "",
	} {
		if got := RequestLanguage(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
