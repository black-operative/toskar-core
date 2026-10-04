// Package guide finds the parts of the user guide a question about
// Yggdrasil needs, so the assistant answers from Yggdrasil's own
// documentation instead of guessing.
package guide

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	userguide "github.com/yeixio/toskar-core/docs/user-guide"
)

// Passage is one paragraph or list of steps from a section.
type Passage struct {
	Section string
	Text    string
}

type indexed struct {
	Passage
	terms map[string]int
	n     int
}

var (
	loadOnce sync.Once
	passages []indexed
	idf      map[string]float64
	avgLen   float64
)

func load() {
	var g struct {
		Sections []struct {
			Title      string   `json:"title"`
			Paragraphs []string `json:"paragraphs"`
			Steps      []string `json:"steps"`
		} `json:"sections"`
	}
	if json.Unmarshal(userguide.GuideJSON, &g) != nil {
		return
	}
	add := func(section, text string) {
		// The section title counts toward every passage in it.
		terms := map[string]int{}
		n := 0
		for _, t := range tokens(section + " " + section + " " + text) {
			terms[t]++
			n++
		}
		passages = append(passages, indexed{Passage: Passage{Section: section, Text: text}, terms: terms, n: n})
	}
	for _, s := range g.Sections {
		for _, p := range s.Paragraphs {
			add(s.Title, p)
		}
		if len(s.Steps) > 0 {
			var b strings.Builder
			for i, st := range s.Steps {
				b.WriteString(strconv.Itoa(i+1) + ". " + st + "\n")
			}
			add(s.Title, strings.TrimSpace(b.String()))
		}
	}
	df := map[string]int{}
	total := 0
	for _, p := range passages {
		total += p.n
		for t := range p.terms {
			df[t]++
		}
	}
	idf = map[string]float64{}
	for t, d := range df {
		idf[t] = math.Log(1 + (float64(len(passages))-float64(d)+0.5)/(float64(d)+0.5))
	}
	if len(passages) > 0 {
		avgLen = float64(total) / float64(len(passages))
	}
}

var stop = map[string]bool{}

func init() {
	// The last line is the product's name, and the one from before the
	// rename (#237): it says a question is about the app (selfRe), not which
	// part of the guide answers it.
	for _, w := range strings.Fields(`a an the and or but if then of to in on at for from by with without about into over
		is are was were be been being am do does did done can could would should will shall may might must
		i me my mine you your yours we our us it its this that these those there here what which who whom whose
		how why where when not no yes so as than too very just also only own same such get got make made
		have has had having please tell show explain want need like use using used way thing things
		toskar yggdrasil`) {
		stop[w] = true
	}
}

// tokens are a text's lower-case words, without common words, with a
// plural or -ing ending trimmed so "automations" finds "automation".
func tokens(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) < 2 || stop[w] {
			continue
		}
		switch {
		case strings.HasSuffix(w, "ies") && len(w) > 4:
			w = w[:len(w)-3] + "y"
		case strings.HasSuffix(w, "ing") && len(w) > 5:
			w = w[:len(w)-3]
		case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
			w = w[:len(w)-1]
		}
		out = append(out, w)
	}
	return out
}

// Search returns up to max passages for a question, best first, that score
// at least min (see About).
func Search(question string, max int, min float64) []Passage {
	loadOnce.Do(load)
	q := map[string]bool{}
	for _, t := range tokens(question) {
		q[t] = true
	}
	type scored struct {
		p     indexed
		score float64
	}
	var all []scored
	const k1, b = 1.2, 0.75
	for _, p := range passages {
		s := 0.0
		for t := range q {
			f := float64(p.terms[t])
			if f == 0 {
				continue
			}
			s += idf[t] * f * (k1 + 1) / (f + k1*(1-b+b*float64(p.n)/avgLen))
		}
		if s >= min {
			all = append(all, scored{p, s})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]Passage, 0, max)
	for i := 0; i < len(all) && i < max; i++ {
		out = append(out, all[i].p.Passage)
	}
	return out
}

var (
	// questionRe is a message asking something.
	questionRe = regexp.MustCompile(`(?i)\?|^\s*(how|what|where|why|when|which|can|could|does|do|is|are|will|explain|tell me|show me|help)\b`)
	// selfRe is a message about Toskar itself, under its name or the one
	// from before the rename (#237).
	selfRe = regexp.MustCompile(`(?i)\b(toskar|yggdrasil|this app|the app|your (own )?(features?|docs?|documentation|settings|capabilit\w*|guide)|you (support|offer|have|do)|in the (app|ui|settings)|what left this computer|this computer only|tool sources?|connected services?|specialized ais?|join (token|command)s?|paired computers?|team profile` +
		// Privacy questions are about this app: "are my chats private?",
		// "who can see my data?", "do I need encryption?".
		`|my (data|chats?|conversations?|files|memories|messages|information) (is |are )?(private|encrypted|safe|secure|stored)` +
		`|(see|read|access) my (data|chats?|conversations?|files|memories|messages)|do i need encryption|is encryption (required|needed))\b`)
)

// Thresholds: a question naming Yggdrasil needs a modest match; one that
// doesn't, a strong match on the guide's own words.
const (
	minNamed  = 2.0
	minStrong = 6.0
	maxChars  = 3000
)

// About returns the guide passages for a question about Yggdrasil, or
// none for anything else.
func About(message string) []Passage {
	m := strings.TrimSpace(message)
	if !questionRe.MatchString(m) || len(strings.Fields(m)) > 60 {
		return nil
	}
	min := minStrong
	if selfRe.MatchString(m) {
		min = minNamed
	}
	found := Search(m, 4, min)
	var out []Passage
	size := 0
	for _, p := range found {
		if size+len(p.Text) > maxChars && len(out) > 0 {
			break
		}
		out = append(out, p)
		size += len(p.Text)
	}
	return out
}

// Block is passages as instructions for the model.
func Block(ps []Passage) string {
	if len(ps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("From Toskar's user guide, for a question about Toskar itself. Answer from these excerpts and name screens and buttons as they say; if they don't cover the question, say so instead of guessing:\n")
	for _, p := range ps {
		b.WriteString("\n[" + p.Section + "]\n" + p.Text + "\n")
	}
	return strings.TrimSpace(b.String())
}
