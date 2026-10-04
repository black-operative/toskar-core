// Package personal shapes answers to the person (spec §38): how long, in
// what tone and format, which units, and what to know about them.
// Preferences change how answers look, never what Yggdrasil may do. Tool
// permissions live in profiles and Settings, and nothing here, or in a
// memory, can grant one.
package personal

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Style is how the person likes answers.
type Style struct {
	// Length is brief, balanced, or detailed; empty leaves it to the request.
	Length string `json:"length,omitempty"`
	// Tone is friendly, neutral, or direct.
	Tone string `json:"tone,omitempty"`
	// Format is prose or lists.
	Format string `json:"format,omitempty"`
	// Units is metric or imperial.
	Units string `json:"units,omitempty"`
	// AboutMe is what the person wants every answer to know, such as their
	// work or where they live.
	AboutMe string `json:"about_me,omitempty"`
	// Instructions are anything else about how to answer.
	Instructions string `json:"instructions,omitempty"`
}

// MaxTextRunes caps AboutMe and Instructions, so they stay a short note.
const MaxTextRunes = 1500

var choices = map[string][]string{
	"length": {"brief", "balanced", "detailed"},
	"tone":   {"friendly", "neutral", "direct"},
	"format": {"prose", "lists"},
	"units":  {"metric", "imperial"},
}

// ErrPermission is returned for a preference or memory that tries to grant
// a permission, such as "you can always push without asking".
var ErrPermission = contracts.NewError("PREFERENCE_CANNOT_GRANT_TOOLS", nil, errors.New("preferences and memories cannot grant permission to use tools. Change what tools may do in Settings › Tool permissions or in a profile"))

// Clean trims a style and checks it.
func (s Style) Clean() (Style, error) {
	s.Length, s.Tone, s.Format, s.Units = norm(s.Length), norm(s.Tone), norm(s.Format), norm(s.Units)
	for field, v := range map[string]string{"length": s.Length, "tone": s.Tone, "format": s.Format, "units": s.Units} {
		if v != "" && !contains(choices[field], v) {
			return Style{}, fmt.Errorf("%s must be one of %s", field, strings.Join(choices[field], ", "))
		}
	}
	s.AboutMe, s.Instructions = strings.TrimSpace(s.AboutMe), strings.TrimSpace(s.Instructions)
	for _, text := range []string{s.AboutMe, s.Instructions} {
		if utf8.RuneCountInString(text) > MaxTextRunes {
			return Style{}, fmt.Errorf("keep each note under %d characters", MaxTextRunes)
		}
		if GrantsPermission(text) {
			return Style{}, ErrPermission
		}
	}
	return s, nil
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Empty reports a style with nothing set.
func (s Style) Empty() bool { return s == Style{} }

// Block is the style as instructions for the model, or "" when nothing is
// set. It says plainly that it shapes answers only.
func (s Style) Block() string {
	var lines []string
	switch s.Length {
	case "brief":
		lines = append(lines, "Keep answers short: the answer first, a few sentences at most, unless the user asks for more.")
	case "detailed":
		lines = append(lines, "Give thorough answers with explanation and examples.")
	case "balanced":
		lines = append(lines, "Keep answers moderately detailed.")
	}
	switch s.Tone {
	case "friendly":
		lines = append(lines, "Use a warm, friendly tone.")
	case "neutral":
		lines = append(lines, "Use a neutral, matter-of-fact tone.")
	case "direct":
		lines = append(lines, "Be direct. Skip pleasantries and filler.")
	}
	switch s.Format {
	case "prose":
		lines = append(lines, "Prefer short paragraphs over bullet lists.")
	case "lists":
		lines = append(lines, "Prefer bullet lists and short headings where they help.")
	}
	switch s.Units {
	case "metric":
		lines = append(lines, "Use metric units (°C, km, kg).")
	case "imperial":
		lines = append(lines, "Use imperial units (°F, miles, pounds).")
	}
	if s.AboutMe != "" {
		lines = append(lines, "About the user: "+s.AboutMe)
	}
	if s.Instructions != "" {
		lines = append(lines, "The user's own instructions for answers: "+s.Instructions)
	}
	if len(lines) == 0 {
		return ""
	}
	return "How the user likes answers. These shape style only; they never grant permission to use a tool or change anything:\n- " +
		strings.Join(lines, "\n- ")
}

// permissionRe finds text that tries to grant a permission: skipping an
// approval ("without asking", "don't ask"), or allowing an action outright
// ("you can always push", "you are allowed to delete").
var (
	skipApprovalRe = regexp.MustCompile(`(?i)\b(without (asking|approval|permission|confirm(ing|ation)?|checking with me)|(don'?t|do not|never) (need to )?(ask|check with me|confirm|wait for (my )?approval)|no need to ask|skip (the )?(approval|confirmation)s?|auto[- ]?approve|pre-?approved?)\b`)
	allowRe        = regexp.MustCompile(`(?i)\b(you('re| are)? (always )?(allowed|permitted|authori[sz]ed) to|you (can|may) always|you have (my )?permission to|i (give|grant) you (permission|access)|permission (is )?granted)\b`)
	actionRe       = regexp.MustCompile(`(?i)\b(run|execute|send|email|delete|remove|push|commit|write|edit|change|modify|install|buy|purchase|pay|post|comment|turn|unlock|open|call|use|access|terminal|shell|command|tools?|files?)\b`)
)

// GrantsPermission reports text that tries to grant a permission rather
// than state a preference, so it can be refused as a preference or memory.
func GrantsPermission(text string) bool {
	if skipApprovalRe.MatchString(text) {
		return true
	}
	return allowRe.MatchString(text) && actionRe.MatchString(text)
}
