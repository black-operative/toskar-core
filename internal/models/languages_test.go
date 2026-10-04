package models

import (
	"slices"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"golang.org/x/text/language"
)

// Every catalog model says which languages it writes and how well, with
// confidence and sources (multilingual spec §13–14, §31), in values clients
// know.
func TestCatalogLanguageCapabilities(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int{"excellent": 0, "good": 1, "fair": 2, "limited": 3}
	for _, e := range c.List() {
		if len(e.Languages) == 0 {
			t.Errorf("%s has no languages", e.ID)
			continue
		}
		seen := map[string]bool{}
		for i, l := range e.Languages {
			if _, err := language.Parse(l.Language); err != nil || seen[l.Language] {
				t.Errorf("%s: language %q is not a tag, or comes twice", e.ID, l.Language)
			}
			seen[l.Language] = true
			if !slices.Contains(contracts.LanguageLevels, l.Level) || !slices.Contains(contracts.LanguageConfidences, l.Confidence) {
				t.Errorf("%s %s: level %q, confidence %q", e.ID, l.Language, l.Level, l.Confidence)
			}
			if len(l.Sources) == 0 {
				t.Errorf("%s %s has no source", e.ID, l.Language)
			}
			for _, s := range l.Sources {
				if !slices.Contains(contracts.LanguageSources, s) {
					t.Errorf("%s %s: source %q", e.ID, l.Language, s)
				}
			}
			if i > 0 && order[l.Level] < order[e.Languages[i-1].Level] {
				t.Errorf("%s: languages are not best first", e.ID)
			}
		}
		if !seen["en"] {
			t.Errorf("%s has no level for English", e.ID)
		}
	}
	// The API's models carry them.
	e, _ := c.Get("qwen2.5-7b-q4")
	m := entryToContract(e, false)
	if len(m.Languages) == 0 || m.Languages[0].Level != "excellent" {
		t.Fatalf("qwen2.5-7b languages = %+v", m.Languages)
	}
}
