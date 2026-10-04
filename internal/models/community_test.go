package models_test

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func communityFixture(t *testing.T) (*models.Catalog, []models.PurposePreset, contracts.HardwareInventory, []string) {
	t.Helper()
	catalog, err := models.NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	presets, err := models.LoadPresetsEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	const gb = 1 << 30
	hw := contracts.HardwareInventory{
		Memory:       contracts.MemoryInfo{TotalBytes: 64 * gb, AvailableBytes: 56 * gb},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", UnifiedMemory: 64 * gb, Backends: []string{"metal"}}},
	}
	var coding []string
	for _, p := range presets {
		if p.ID == "coding" {
			coding = p.PreferredModels
		}
	}
	return catalog, presets, hw, coding
}

func winner(resp contracts.ModelsFitResponse, category string) contracts.CategoryWinner {
	for _, w := range resp.Winners {
		if w.Category == category {
			return w
		}
	}
	return contracts.CategoryWinner{}
}

func TestCommunityRatingsAreOneSignalInWinners(t *testing.T) {
	catalog, presets, hw, coding := communityFixture(t)
	base := winner(models.BuildFitResponse(catalog, hw, "local", "Mac", presets, models.FitOptions{}), "coding")
	if base.ModelID == "" || base.CommunityChosen {
		t.Fatalf("without ratings: %+v", base)
	}
	// The curated choice's position, and an eligible model after it.
	pos := -1
	for i, id := range coding {
		if id == base.ModelID {
			pos = i
		}
	}
	if pos < 0 || pos+1 >= len(coding) {
		t.Skip("the coding preset has no second eligible model on this hardware")
	}
	next := coding[pos+1]

	// A small lead is not enough to pass the curated order.
	slight := map[string]models.CommunitySignal{next: {Signal: 0.2, Score: 3.7, Ratings: 12, Similar: true}}
	if w := winner(models.BuildFitResponse(catalog, hw, "local", "Mac", presets, models.FitOptions{Community: slight}), "coding"); w.ModelID != base.ModelID {
		t.Fatalf("a 0.2 lead moved the pick to %s", w.ModelID)
	}
	// A clear lead from hardware like this is.
	clear := map[string]models.CommunitySignal{next: {Signal: 0.8, Score: 4.3, Ratings: 40, Similar: true}}
	w := winner(models.BuildFitResponse(catalog, hw, "local", "Mac", presets, models.FitOptions{Community: clear}), "coding")
	if w.ModelID != next || !w.CommunityChosen {
		t.Fatalf("a 0.8 lead: %+v, want %s chosen by the community", w, next)
	}
	// So is a poorly rated curated choice.
	poor := map[string]models.CommunitySignal{base.ModelID: {Signal: -1.2, Score: 2.3, Ratings: 30, Similar: true}}
	if w := winner(models.BuildFitResponse(catalog, hw, "local", "Mac", presets, models.FitOptions{Community: poor}), "coding"); w.ModelID == base.ModelID {
		t.Fatal("a poorly rated curated choice stayed the pick")
	}
	// Ratings never make a model that cannot run here a winner.
	huge := map[string]models.CommunitySignal{}
	for _, f := range models.ScoreFits(catalog, hw, "local", "Mac", models.FitOptions{}) {
		if f.Label == contracts.FitTooLarge {
			huge[f.ModelID] = models.CommunitySignal{Signal: 2, Score: 5, Ratings: 500}
		}
	}
	for _, w := range models.BuildFitResponse(catalog, hw, "local", "Mac", presets, models.FitOptions{Community: huge}).Winners {
		if _, ok := huge[w.ModelID]; ok {
			t.Fatalf("%s is too large but won %s", w.ModelID, w.Category)
		}
	}
}

func TestCommunityRatingsInTheFirstRecommendation(t *testing.T) {
	catalog, presets, hw, _ := communityFixture(t)
	base, err := models.RecommendWithPresets(catalog, presets, models.RecommendInput{Purpose: "general", Hardware: hw})
	if err != nil || len(base.Models) == 0 || base.CommunityChosen {
		t.Fatalf("without ratings: %+v, %v", base, err)
	}
	primary := base.Roles[0].ModelID
	poor := map[string]models.CommunitySignal{primary: {Signal: -1.5, Score: 2, Ratings: 25, Similar: true}}
	rec, err := models.RecommendWithPresets(catalog, presets, models.RecommendInput{Purpose: "general", Hardware: hw, Community: poor})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Roles[0].ModelID == primary || !rec.CommunityChosen {
		t.Fatalf("a poorly rated first choice stayed: %+v", rec.Roles)
	}
}
