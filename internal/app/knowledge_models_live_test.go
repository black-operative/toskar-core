package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// TestSemanticSearchWithARealEmbeddingModel runs knowledge search through a
// real llama-server and embedding model. It is skipped unless
// YGG_TEST_EMBED_MODEL names a GGUF file (for example
// nomic-embed-text-v1.5.Q8_0.gguf) and YGG_TEST_RUNTIMES_DIR is a runtimes
// directory with llamacpp/llama-server in it.
func TestSemanticSearchWithARealEmbeddingModel(t *testing.T) {
	modelPath, runtimesDir := os.Getenv("YGG_TEST_EMBED_MODEL"), os.Getenv("YGG_TEST_RUNTIMES_DIR")
	if modelPath == "" || runtimesDir == "" {
		t.Skip("set YGG_TEST_EMBED_MODEL and YGG_TEST_RUNTIMES_DIR to run against a real embedding model")
	}
	ctx := context.Background()
	rt := llamacpp.New(runtimesDir, t.TempDir())
	t.Cleanup(func() {
		running, _ := rt.ListRunning(ctx)
		for _, r := range running {
			_ = rt.StopModel(ctx, r.ID)
		}
	})

	k, _ := testKnowledgeModels(&App{}, nomic)
	k.start = func(ctx context.Context, id, mode string) (string, error) {
		r, err := rt.StartModel(ctx, pluginapi.ModelStartConfig{ModelID: id, ModelPath: modelPath, Mode: mode})
		return r.Endpoint, err
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	kb := mimir.NewStore(db.SQL, t.TempDir())
	kb.SetModels(k)
	// Separate files, so each policy is its own passage.
	var ids []string
	for name, text := range map[string]string{
		"guarantee.md": "# Guarantee\n\nEvery tyre we sell is covered for five years against defects.",
		"returns.md":   "# Returns\n\nUnused items can be brought back within 30 days for a refund.",
		"hours.md":     "# Opening hours\n\nWe open at 9 and close at 5 on weekdays.",
		"shipping.md":  "# Shipping\n\nOrders over $50 ship free. Most orders arrive in 3 to 5 business days.",
	} {
		src, err := kb.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: name, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, src.ID)
	}
	stock, err := kb.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: "inventory.csv", Text: `sku,brand,model,size,price,in_stock
MP-22545,Michelin,Pilot Sport 4,225/45R17,189.99,12
BW-20555,Bridgestone,Blizzak WS90,205/55R16,142.50,0
CT-21555,Continental,ExtremeContact,215/55R17,165.00,6
GY-19565,Goodyear,Assurance,195/65R15,99.00,20
`})
	if err != nil {
		t.Fatal(err)
	}
	if err := kb.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ids = append(ids, stock.ID)

	search := func(q string) []mimir.Hit {
		t.Helper()
		hits, err := kb.Search(ctx, mimir.SearchInput{Query: q, SourceIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			t.Logf("%q → %s (%s, %.4f)", q, h.Title, h.Match, h.Score)
		}
		return hits
	}
	if hits := search("What warranty do you offer?"); len(hits) != 1 || !strings.Contains(hits[0].Body, "five years") {
		t.Errorf("want only the guarantee passage, got %d hits", len(hits))
	}
	if hits := search("Can I get my money back?"); len(hits) == 0 || !strings.Contains(hits[0].Body, "refund") {
		t.Errorf("want the returns passage first, got %d hits", len(hits))
	}
	if hits := search("How long does delivery take?"); len(hits) != 1 || !strings.Contains(hits[0].Body, "business days") {
		t.Errorf("want only the shipping passage, got %d hits", len(hits))
	}
	if hits := search("price of MP-22545"); len(hits) != 1 || !strings.Contains(hits[0].Body, "Michelin") {
		t.Errorf("want only the named row, got %d hits", len(hits))
	}
	for _, q := range []string{"What's the capital of France?", "How do I reset my router password?", "hello"} {
		if hits := search(q); len(hits) != 0 {
			t.Errorf("%q should find nothing, got %d hits", q, len(hits))
		}
	}
}
