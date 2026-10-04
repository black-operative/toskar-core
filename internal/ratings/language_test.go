package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestRatingLanguage(t *testing.T) {
	for in, want := range map[string]string{"": "", "es": "es", "pt-BR": "pt", "EN-us": "en", "zh": "zh-Hans", "zh-TW": "zh-Hant", "zh-Hant-HK": "zh-Hant", "fil": "fil"} {
		if got, ok := RatingLanguage(in); !ok || got != want {
			t.Errorf("RatingLanguage(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"english", "e", "1x", "es_ES!"} {
		if _, ok := RatingLanguage(bad); ok {
			t.Errorf("RatingLanguage(%q) ok", bad)
		}
	}
}

// A rating keeps its language here, and sends it only when shared.
func TestRateInLanguage(t *testing.T) {
	fake := &fakeService{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, _, records := newService(t, srv)
	ctx := context.Background()

	v, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 5, Language: "pt-BR"})
	if err != nil || v.Language != "pt" || len(fake.posts) != 0 {
		t.Fatalf("private rating in Portuguese = %+v, %v, %v", v, err, fake.posts)
	}
	if _, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 5, Language: "Portuguese"}); !errors.Is(err, ErrLanguage) {
		t.Fatalf("bad language err = %v", err)
	}
	if _, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 4, Language: "es", Share: true}); err != nil {
		t.Fatal(err)
	}
	if p := fake.posts[0]; p.Language != "es" {
		t.Fatalf("posted %+v", p)
	}
	if !strings.HasSuffix((*records)[0], ", used in es") {
		t.Fatalf("records = %v", *records)
	}
	// Not saying a language clears it.
	if v, _ := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 4, Share: true}); v.Language != "" || fake.posts[1].Language != "" {
		t.Fatalf("language kept: %+v %+v", v, fake.posts[1])
	}
}

func TestCommunityLanguages(t *testing.T) {
	fake := &fakeService{}
	if err := json.Unmarshal([]byte(`{"schema_version":1,"generated_at":"2026-10-01T04:17:00Z","prior":3.5,"weight":5,"min_ratings":3,"models":[
		{"model":"qwen2.5-coder-7b-instruct","format":"gguf","quantization":"Q4_K_M","runtime":"llamacpp","backend":"metal",
		 "cohorts":[{"tier":"global","ratings":40,"average":4.1,"weighted_score":4.0,"confidence":"community"}],
		 "languages":[{"language":"es","ratings":14,"average":4.8,"weighted_score":4.6,"confidence":"community"},
		              {"language":"ja","ratings":4,"average":2,"weighted_score":2.9,"confidence":"early"}]}]}`), &fake.snap); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, settings, _ := newService(t, srv)
	ctx := context.Background()

	if l, err := s.Languages(ctx); err != nil || l != nil {
		t.Fatalf("languages with ratings off = %v, %v", l, err)
	}
	_ = settings.SetBool(ctx, SettingShow, true)
	c, err := s.Community(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l := c.Models["qwen2.5-coder-7b-q4"].Languages; len(l) != 2 || l[0].Language != "es" || l[0].Ratings != 14 {
		t.Fatalf("community languages = %+v", l)
	}
	l, err := s.Languages(ctx)
	if err != nil || len(l["qwen2.5-coder-7b-q4"]) != 2 {
		t.Fatalf("Languages = %v, %v", l, err)
	}
}

func TestWithLanguageRatings(t *testing.T) {
	m := contracts.Model{ID: "m", Languages: []contracts.LanguageCapability{
		{Language: "es", Level: "fair", Sources: []string{"model_card"}},
		{Language: "ja", Level: "excellent", Sources: []string{"model_card"}},
		{Language: "pt-BR", Level: "good", Sources: []string{"model_card"}},
	}}
	got := WithLanguageRatings(m, []LanguageStats{
		// A community's ratings set the level.
		{Language: "es", Ratings: 14, WeightedScore: 4.6, Confidence: "community"},
		// Early ones move it one step at most.
		{Language: "ja", Ratings: 4, WeightedScore: 2.1, Confidence: "early"},
		// The same as the catalog already says: unchanged.
		{Language: "pt", Ratings: 12, WeightedScore: 3.9, Confidence: "community"},
		// A language the model's levels leave out is limited, and moves from there.
		{Language: "de", Ratings: 5, WeightedScore: 4.5, Confidence: "early"},
	})
	want := map[string]string{"es": "excellent", "ja": "good", "pt-BR": "good", "de": "fair"}
	for _, l := range got.Languages {
		if l.Level != want[l.Language] {
			t.Errorf("%s = %s, want %s", l.Language, l.Level, want[l.Language])
		}
		community := strings.Contains(strings.Join(l.Sources, ","), "community")
		if community == (l.Language == "pt-BR") {
			t.Errorf("%s sources %v", l.Language, l.Sources)
		}
	}
	if len(got.Languages) != 4 || m.Languages[0].Level != "fair" || len(m.Languages[0].Sources) != 1 {
		t.Fatalf("got %+v; the original changed: %+v", got.Languages, m.Languages)
	}
	// A model with no levels is fair in each language.
	if got := WithLanguageRatings(contracts.Model{}, []LanguageStats{{Language: "ko", WeightedScore: 2, Confidence: "early"}}); got.Languages[0].Level != "limited" {
		t.Fatalf("no levels: %+v", got.Languages)
	}
}
