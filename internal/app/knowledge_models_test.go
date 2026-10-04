package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func testKnowledgeModels(a *App, installed ...contracts.Model) (*knowledgeModels, *[]string) {
	k := newKnowledgeModels(a)
	k.installed = func(context.Context) []contracts.Model { return installed }
	var started []string
	k.start = func(_ context.Context, id, mode string) (string, error) {
		started = append(started, id+"/"+mode)
		return "http://127.0.0.1:1", nil
	}
	return k, &started
}

var (
	nomic = contracts.Model{ID: "nomic-embed-text-v1.5-q8", DisplayName: "Nomic Embed Text v1.5", Status: "installed",
		SupportRole: contracts.SupportEmbedding}
	hfEmbed = contracts.Model{ID: "hf-bge-small-en-embed", Status: "installed", Dynamic: true}
	llama   = contracts.Model{ID: "llama-3.2-1b-q4", Status: "installed"}
)

func TestNoEmbeddingModelMeansKeywordSearch(t *testing.T) {
	k, started := testKnowledgeModels(&App{}, llama)
	emb, err := k.Embedder(context.Background())
	if emb != nil || err != nil || len(*started) != 0 {
		t.Fatalf("got %v, %v; started %v", emb, err, *started)
	}
	rr, err := k.Reranker(context.Background())
	if rr != nil || err != nil {
		t.Fatalf("got %v, %v", rr, err)
	}
}

func TestEmbeddingModelIsStartedInEmbeddingMode(t *testing.T) {
	k, started := testKnowledgeModels(&App{Share: share.New(time.Millisecond)}, llama, hfEmbed, nomic)
	emb, err := k.Embedder(context.Background())
	if err != nil || emb == nil {
		t.Fatalf("got %v, %v", emb, err)
	}
	// The catalog model wins over one installed by URL.
	if emb.ModelID() != nomic.ID || len(*started) != 1 || (*started)[0] != nomic.ID+"/"+pluginapi.ModeEmbedding {
		t.Fatalf("model %s, started %v", emb.ModelID(), *started)
	}
	le := emb.(*llamaEmbedder)
	if le.docPrefix != "search_document: " || le.queryPrefix != "search_query: " {
		t.Fatalf("nomic prefixes: %q %q", le.docPrefix, le.queryPrefix)
	}
}

func TestNothingIsLoadedWhileTraining(t *testing.T) {
	g := share.New(time.Millisecond)
	a := &App{Share: g}
	k, started := testKnowledgeModels(a, nomic)
	w, err := g.Enter(context.Background(), share.Training, "Tire shop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Embedder(context.Background()); !errors.Is(err, mimir.ErrNotNow) || len(*started) != 0 {
		t.Fatalf("got %v; started %v", err, *started)
	}
	if _, err := a.admitIndexing(context.Background()); !errors.Is(err, mimir.ErrNotNow) {
		t.Fatalf("background indexing should wait for training, got %v", err)
	}
	w.Done()
	if emb, err := k.Embedder(context.Background()); err != nil || emb == nil {
		t.Fatalf("after training: %v, %v", emb, err)
	}
	release, err := a.admitIndexing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestAFailedStartIsNotRetriedOnEverySearch(t *testing.T) {
	k, _ := testKnowledgeModels(&App{}, nomic)
	calls := 0
	k.start = func(context.Context, string, string) (string, error) {
		calls++
		return "", errors.New("llama-server stopped while loading the model")
	}
	for range 3 {
		if _, err := k.Embedder(context.Background()); !errors.Is(err, mimir.ErrNotNow) {
			t.Fatalf("got %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("started %d times", calls)
	}
}

func TestStubInferenceKeepsSearchKeywordOnly(t *testing.T) {
	k, started := testKnowledgeModels(&App{stubInference: true}, nomic)
	if emb, err := k.Embedder(context.Background()); emb != nil || err != nil || len(*started) != 0 {
		t.Fatalf("got %v, %v", emb, err)
	}
}

func TestEmbedPrefixes(t *testing.T) {
	cases := map[string][2]string{
		"multilingual-e5-small":  {"passage: ", "query: "},
		"bge-small-en-v1.5":      {"", "Represent this sentence for searching relevant passages: "},
		"bge-m3":                 {"", ""},
		"all-minilm-l6-v2-embed": {"", ""},
	}
	for id, want := range cases {
		doc, q := embedPrefixes(contracts.Model{ID: id})
		if doc != want[0] || q != want[1] {
			t.Errorf("%s: %q %q", id, doc, q)
		}
	}
}
