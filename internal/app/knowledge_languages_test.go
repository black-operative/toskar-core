package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// With an English and a multilingual embedding model installed, knowledge
// and memory use the multilingual one (multilingual spec §19).
func TestMultilingualEmbedderIsPreferred(t *testing.T) {
	lang := func(tags ...string) []contracts.LanguageCapability {
		out := []contracts.LanguageCapability{}
		for _, tag := range tags {
			out = append(out, contracts.LanguageCapability{Language: tag, Level: "good", Confidence: "medium", Sources: []string{"model_card"}})
		}
		return out
	}
	k := &knowledgeModels{installed: func(context.Context) []contracts.Model {
		return []contracts.Model{
			{ID: "a-english-embed", Status: "installed", SupportRole: contracts.SupportEmbedding, Languages: lang("en")},
			{ID: "bge-m3-q8", Status: "installed", SupportRole: contracts.SupportEmbedding, Languages: lang("en", "es", "de", "ja")},
		}
	}}
	m, ok := k.pick(context.Background(), contracts.SupportEmbedding)
	if !ok || m.ID != "bge-m3-q8" {
		t.Fatalf("picked %q", m.ID)
	}
}
