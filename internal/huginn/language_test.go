package huginn

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func speaker(id string, memGB uint64, langs map[string]string) contracts.Model {
	m := contracts.Model{ID: id, DisplayName: id, Installed: true, MemoryNeeded: memGB * gb, Purpose: []string{"general"}}
	for tag, level := range langs {
		m.Languages = append(m.Languages, contracts.LanguageCapability{Language: tag, Level: level, Confidence: "medium", Sources: []string{"model_card"}})
	}
	return m
}

// The spec's example (§15): asked in Spanish, the model that writes Spanish
// well beats a faster, larger one that writes it fairly.
func TestLanguageIsARoutingSignal(t *testing.T) {
	a := speaker("a", 5, map[string]string{"en": "excellent", "es": "excellent"})
	b := speaker("b", 9, map[string]string{"en": "excellent", "es": "fair"})
	b.Status = "running"
	installed := []contracts.Model{a, b}

	c, ok := ChooseIn(Chat, EffortAuto, "es", installed, 32*gb)
	if !ok || c.Model.ID != "a" || c.LanguageWeak {
		t.Fatalf("Spanish: %+v", c)
	}
	if !strings.Contains(c.Reason("en"), "in Spanish") {
		t.Errorf("reason = %q", c.Reason("en"))
	}
	// In English, both write it well: the loaded model answers a quick question.
	if c, _ := ChooseIn(Chat, EffortAuto, "en", installed, 32*gb); c.Model.ID != "b" {
		t.Fatalf("English picked %s", c.Model.ID)
	}
	// Without a language, nothing changes.
	if c, _ := ChooseFor(Chat, EffortAuto, installed, 32*gb); c.Model.ID != "b" {
		t.Fatalf("no language picked %s", c.Model.ID)
	}
}

// Missing metadata never makes a model unusable, and a warning comes only
// when the pick writes the language materially worse and nothing that fits
// does better (§16).
func TestLanguageFallback(t *testing.T) {
	englishOnly := speaker("english-only", 5, map[string]string{"en": "excellent"})
	unrated := speaker("unrated", 4, nil)

	// An unrated model beats one whose levels leave Japanese out.
	c, _ := ChooseIn(Chat, EffortAuto, "ja", []contracts.Model{englishOnly, unrated}, 32*gb)
	if c.Model.ID != "unrated" || c.LanguageWeak {
		t.Fatalf("unrated vs English-only: %+v", c)
	}
	// Only the English model: it still answers, with a warning.
	c, ok := ChooseIn(Chat, EffortAuto, "ja", []contracts.Model{englishOnly}, 32*gb)
	if !ok || c.Model.ID != "english-only" || !c.LanguageWeak {
		t.Fatalf("English-only for Japanese: %+v %v", c, ok)
	}
	// A better model that doesn't fit this computer is no reason to warn less.
	tooBig := speaker("too-big", 64, map[string]string{"ja": "excellent"})
	if c, _ := ChooseIn(Chat, EffortAuto, "ja", []contracts.Model{englishOnly, tooBig}, 16*gb); c.Model.ID != "english-only" || !c.LanguageWeak {
		t.Fatalf("too big to run: %+v", c)
	}
}

func TestLanguageLevelMatching(t *testing.T) {
	m := speaker("m", 5, map[string]string{"pt": "good", "zh-Hans": "excellent"})
	if l, ok := LanguageLevel(m, "pt-BR"); !ok || l.Level != "good" {
		t.Errorf("pt-BR → %+v %v", l, ok)
	}
	if _, ok := LanguageLevel(m, "zh-Hant"); ok {
		t.Error("Traditional Chinese took the Simplified level")
	}
	if _, ok := LanguageLevel(m, "zh-CN"); !ok {
		t.Error("zh-CN did not match zh-Hans")
	}
	if !WeakIn(m, "de") || WeakIn(m, "") || WeakIn(speaker("x", 1, nil), "de") {
		t.Error("WeakIn")
	}
}
