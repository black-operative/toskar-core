package automations

import (
	"strings"
	"time"
)

// Recurring reports a message that asks for something on a repeating
// schedule, such as "every morning at 8" or "jeden Montag", in any
// language with request words (#204). Chat offers the schedule tool for
// these. A day alone, such as "on Monday", isn't recurring: "what's on
// Monday?" is a question.
func Recurring(text string) bool {
	normalized := normalizeRequest(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	_, order := requestLanguagesAll()
	for _, w := range order {
		t := withDigits(normalized, w)
		if parseInterval(t, w) > 0 || dailyPart(t, w) != "" || everyWeekday(t, w) {
			return true
		}
	}
	return false
}

// RequestLanguage is the language whose request words find a schedule in
// text, such as de for "jeden Morgen um 8 Uhr", or "" when none does. A
// chat request is read in the language it's written in, whatever the
// assistant answers in.
func RequestLanguage(text string) string {
	normalized := normalizeRequest(strings.TrimSpace(text))
	if normalized == "" {
		return ""
	}
	loc := time.UTC
	_, order := requestLanguagesAll()
	// English last, since other languages' requests may carry English
	// words, such as "AM".
	for _, pass := range []bool{false, true} {
		for _, w := range order {
			if (w.code == "en") != pass {
				continue
			}
			if found, err := parseSchedule(withDigits(normalized, w), w, time.Now(), loc, "UTC", w.code); err == nil && found != nil {
				return w.code
			}
		}
	}
	return ""
}

// everyWeekday is a weekday said with "every" or "each", or in a form that
// means every one, such as "montags" or "mondays".
func everyWeekday(text string, w *requestWordList) bool {
	if len(w.Weekly.Days) == 0 {
		return false
	}
	re := w.pattern("everyWeekday", func() string {
		days := []string{}
		for _, day := range w.Weekly.Days {
			days = append(days, phraseSource(day, w.Spaced, true, true))
		}
		names := `(?:` + strings.Join(days, "|") + `)`
		forms := []string{}
		if len(w.Interval.Before) > 0 {
			forms = append(forms, phraseSource(w.Interval.Before, w.Spaced, true, true)+w.gap()+names)
		}
		if len(w.Weekly.After) > 0 {
			forms = append(forms, names+`\s*`+phraseSource(w.Weekly.After, w.Spaced, false, true))
		}
		for _, alone := range w.Weekly.Alone {
			forms = append(forms, phraseSource(alone, w.Spaced, true, true))
		}
		if len(forms) == 0 {
			return ""
		}
		return `(?:` + strings.Join(forms, "|") + `)`
	})
	if re == nil {
		return false
	}
	ok, _ := re.MatchString(text)
	return ok
}
