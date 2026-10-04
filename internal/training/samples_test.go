package training

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/models"
)

func TestCreateExampleTeachesBothConcepts(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	var catalog []models.CatalogEntry
	for _, e := range testCatalog {
		catalog = append(catalog, e)
	}
	ai, err := h.svc.CreateExample(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !ai.Example || ai.BaseModelID != "small-q4" || ai.Preset != PresetQuick {
		t.Fatalf("example AI = %+v", ai)
	}
	view, err := h.svc.View(ctx, ai.ID)
	if err != nil {
		t.Fatal(err)
	}
	uses := map[string]Use{}
	for _, m := range view.Materials {
		uses[m.Filename] = m.Use
	}
	want := map[string]Use{"tire-chats.jsonl": UseTraining, "inventory.csv": UseKnowledge, "returns-and-services.md": UseKnowledge}
	for f, u := range want {
		if uses[f] != u {
			t.Errorf("%s recommended %q, want %q", f, uses[f], u)
		}
	}
	if len(view.Knowledge) != 2 {
		t.Fatalf("knowledge sources = %v", view.Knowledge)
	}
	// The planted flaws show up for the review step.
	st := view.Dataset
	if st.Flagged[FlagDuplicate] != 1 || st.Flagged[FlagNoAnswer] != 1 || st.Flagged[FlagVolatile] != 1 {
		t.Fatalf("flags = %v", st.Flagged)
	}
	if st.Usable < RecommendedExamples-5 {
		t.Fatalf("usable examples = %d", st.Usable)
	}
	plan, err := h.svc.Plan(ctx, ai.ID)
	if err != nil || !plan.Ready {
		t.Fatalf("example is not ready to train: %+v %v", plan, err)
	}

	again, err := h.svc.CreateExample(ctx, catalog)
	if err != nil || again.ID != ai.ID {
		t.Fatalf("second call made another example: %v %v", again.ID, err)
	}
	// The knowledge answers price questions from the inventory.
	hits, err := h.kb.Search(ctx, searchInput("price of the Pilot Sport 4 in 225/45R17", view.Knowledge))
	if err != nil || len(hits) == 0 || hits[0].Title != "inventory.csv row 1" {
		t.Fatalf("hits = %+v %v", hits, err)
	}
}
