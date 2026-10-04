package ratings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A rating can say the language the person used the model in (multilingual
// spec §23). The ratings service takes a base tag such as es, and keeps
// Chinese's script, so the ratings of each language collect together.
var languageRe = regexp.MustCompile(`^([a-z]{2,3}|zh-Hans|zh-Hant)$`)

// ErrLanguage is a rating's language that is not a language tag.
var ErrLanguage = errors.New("a rating's language is a language tag such as es or zh-Hant")

// RatingLanguage is the tag a rating gives for a BCP 47 tag: pt-BR is pt,
// zh-TW is zh-Hant. ok is false when tag is not a language tag; "" is no
// language.
func RatingLanguage(tag string) (string, bool) {
	if tag == "" {
		return "", true
	}
	lower := strings.ToLower(tag)
	base, rest, _ := strings.Cut(lower, "-")
	if base == "zh" {
		if strings.Contains(rest, "hant") || rest == "tw" || rest == "hk" || rest == "mo" {
			return "zh-Hant", true
		}
		return "zh-Hans", true
	}
	return base, languageRe.MatchString(base)
}

// LanguageStats are a model configuration's ratings given for one language,
// for everyone who runs it that way: never split by hardware.
type LanguageStats struct {
	Language      string  `json:"language"`
	Ratings       int     `json:"ratings"`
	Average       float64 `json:"average"`
	WeightedScore float64 `json:"weighted_score"`
	Confidence    string  `json:"confidence"`
}

// Languages are community ratings by language, by local model ID, from the
// summary already kept, so asking never sends anything; with community
// ratings off there are none.
func (s *Service) Languages(ctx context.Context) (map[string][]LanguageStats, error) {
	snap, ok, err := s.kept(ctx)
	if !ok || err != nil {
		return nil, err
	}
	inv, err := s.Hardware(ctx)
	if err != nil {
		return nil, err
	}
	backend := Backend(inv)
	list, err := s.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]LanguageStats{}
	for _, m := range list {
		id, err := Identify(m, backend)
		if err != nil {
			continue
		}
		if mc, ok := lookup(snap, id, Hardware{}, false); ok && len(mc.Languages) > 0 {
			out[m.ID] = mc.Languages
		}
	}
	return out, nil
}

// kept is the summary already downloaded, when community ratings are on.
func (s *Service) kept(ctx context.Context) (Snapshot, bool, error) {
	if on, err := s.Settings.GetBool(ctx, SettingShow, false); err != nil || !on {
		return Snapshot{}, false, err
	}
	var body string
	err := s.DB.QueryRowContext(ctx, `SELECT body FROM ratings_summary WHERE id = 1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		return Snapshot{}, false, nil
	}
	return snap, true, nil
}

// Language levels, weakest first, as contracts.LanguageCapability names them.
var levels = []string{"limited", "fair", "good", "excellent"}

// communityLevel is the level a language's weighted score stands for.
func communityLevel(score float64) int {
	switch {
	case score >= 4.3:
		return 3
	case score >= 3.7:
		return 2
	case score >= 3.0:
		return 1
	}
	return 0
}

// WithLanguageRatings is m with its language levels moved by what the
// community says it is like in each language, so Huginn's language-aware
// choice counts them (§23). A community's ratings (10 or more) set the
// level; early ones (3 to 9) move it one step at most. A level the
// community moved names it among its sources.
func WithLanguageRatings(m contracts.Model, stats []LanguageStats) contracts.Model {
	if len(stats) == 0 {
		return m
	}
	langs := append([]contracts.LanguageCapability(nil), m.Languages...)
	for _, st := range stats {
		if st.Confidence != "early" && st.Confidence != "community" {
			continue
		}
		i := matchLanguage(langs, st.Language)
		// A model that lists no languages is treated as fair in each; one
		// that leaves this language out, as limited.
		from := 1
		if i >= 0 {
			from = levelIndex(langs[i].Level)
		} else if len(m.Languages) > 0 {
			from = 0
		}
		to := communityLevel(st.WeightedScore)
		if st.Confidence == "early" {
			to = max(from-1, min(from+1, to))
		}
		if to == from && i >= 0 {
			continue
		}
		if i < 0 {
			langs = append(langs, contracts.LanguageCapability{Language: st.Language})
			i = len(langs) - 1
		}
		langs[i].Level = levels[to]
		if !containsString(langs[i].Sources, "community") {
			langs[i].Sources = append(append([]string(nil), langs[i].Sources...), "community")
		}
	}
	m.Languages = langs
	return m
}

func levelIndex(level string) int {
	for i, l := range levels {
		if l == level {
			return i
		}
	}
	return 1
}

// matchLanguage finds a rating language among a model's levels: the same
// language in any region, and Chinese only in the same script.
func matchLanguage(langs []contracts.LanguageCapability, tag string) int {
	for i, l := range langs {
		if t, ok := RatingLanguage(l.Language); ok && t == tag {
			return i
		}
	}
	return -1
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
