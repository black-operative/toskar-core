package huginn

import (
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Language ranks: how well a model writes the language an answer is in
// (multilingual spec §13–16). A model without language levels is treated as
// fair, so missing metadata never makes a model unusable; one whose levels
// leave the language out, or call it limited, is the weakest choice.
const (
	rankWeak      = 1 // limited, or not among the model's languages
	rankFair      = 2 // fair, or a model with no language levels
	rankGood      = 3
	rankExcellent = 4
)

// LanguageLevel is a model's level for a language: the same tag, or the same
// language in another region (pt-BR for pt). Chinese matches only in the
// same script.
func LanguageLevel(m contracts.Model, tag string) (contracts.LanguageCapability, bool) {
	tag = strings.ToLower(tag)
	for _, l := range m.Languages {
		if strings.ToLower(l.Language) == tag {
			return l, true
		}
	}
	base, _, _ := strings.Cut(tag, "-")
	if base == "zh" {
		script := "zh-hans"
		if strings.Contains(tag, "hant") || strings.HasSuffix(tag, "-tw") || strings.HasSuffix(tag, "-hk") || strings.HasSuffix(tag, "-mo") {
			script = "zh-hant"
		}
		for _, l := range m.Languages {
			if strings.ToLower(l.Language) == script {
				return l, true
			}
		}
		return contracts.LanguageCapability{}, false
	}
	for _, l := range m.Languages {
		if b, _, _ := strings.Cut(strings.ToLower(l.Language), "-"); b == base {
			return l, true
		}
	}
	return contracts.LanguageCapability{}, false
}

// languageRank is how well m writes lang; every model ranks the same when
// the language is not known.
func languageRank(m contracts.Model, lang string) int {
	if lang == "" {
		return rankFair
	}
	if len(m.Languages) == 0 {
		return rankFair
	}
	l, ok := LanguageLevel(m, lang)
	if !ok {
		return rankWeak
	}
	switch l.Level {
	case "excellent":
		return rankExcellent
	case "good":
		return rankGood
	case "fair":
		return rankFair
	}
	return rankWeak
}

// WeakIn reports a model expected to write lang materially worse: its
// levels leave the language out, or call it limited. A model without
// language levels is not weak, since nothing says so.
func WeakIn(m contracts.Model, lang string) bool {
	return lang != "" && languageRank(m, lang) == rankWeak
}
