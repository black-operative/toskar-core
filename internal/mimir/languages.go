package mimir

import (
	"encoding/json"
	"sort"

	"github.com/yeixio/toskar-core/internal/replylang"
)

// SourceLanguage is a language a source is written in, and how many of its
// passages are in it.
type SourceLanguage struct {
	Language string `json:"language"`
	Passages int    `json:"passages"`
}

// passageLanguages detects the language of each passage on this computer
// (multilingual spec §19), most passages first. Passages too short to tell,
// such as a table row of numbers, are not counted.
func passageLanguages(chunks []chunk) []SourceLanguage {
	counts := map[string]int{}
	for _, c := range chunks {
		if tag, ok := replylang.Detect(c.Title + "\n" + c.Body); ok {
			counts[tag]++
		}
	}
	out := make([]SourceLanguage, 0, len(counts))
	for tag, n := range counts {
		out = append(out, SourceLanguage{Language: tag, Passages: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Passages != out[j].Passages {
			return out[i].Passages > out[j].Passages
		}
		return out[i].Language < out[j].Language
	})
	return out
}

// languagesJSON is the languages as stored, or NULL when none was detected.
func languagesJSON(langs []SourceLanguage) any {
	if len(langs) == 0 {
		return nil
	}
	raw, err := json.Marshal(langs)
	if err != nil {
		return nil
	}
	return string(raw)
}
