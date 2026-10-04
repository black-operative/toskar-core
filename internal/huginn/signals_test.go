package huginn

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestCommunityRatingsInAuto(t *testing.T) {
	const ram = 24 * gb
	// Research: qwen-14b is the largest that fits. r1-7b and qwen-7b also suit.
	plain, _ := ChooseWith(Research, EffortAuto, "", library, ram, Signals{})
	if plain.Model.ID != "qwen-14b" || plain.Rated {
		t.Fatalf("without ratings: %s rated=%v", plain.Model.ID, plain.Rated)
	}
	// A small lead leaves size to decide.
	slight := Signals{Rating: map[string]float64{"qwen-7b": 0.3}}
	if got, _ := ChooseWith(Research, EffortAuto, "", library, ram, slight); got.Model.ID != "qwen-14b" || got.Rated {
		t.Fatalf("a 0.3 lead: %s rated=%v", got.Model.ID, got.Rated)
	}
	// A clear lead wins, and says so.
	clear := Signals{Rating: map[string]float64{"qwen-7b": 0.8, "qwen-14b": 0.1}}
	got, _ := ChooseWith(Research, EffortAuto, "", library, ram, clear)
	if got.Model.ID != "qwen-7b" || !got.Rated {
		t.Fatalf("a clear lead: %s rated=%v", got.Model.ID, got.Rated)
	}
	// A poorly rated pick gives way.
	poor := Signals{Rating: map[string]float64{"qwen-14b": -1}}
	if got, _ := ChooseWith(Research, EffortAuto, "", library, ram, poor); got.Model.ID == "qwen-14b" || !got.Rated {
		t.Fatalf("poorly rated: %s rated=%v", got.Model.ID, got.Rated)
	}
	// Ratings never make a model that doesn't fit, or doesn't suit, the choice.
	huge := model("huge-70b", 40*gb, []string{"general"}, []string{"general", "research"}, contracts.ModelCapabilities{ToolCalling: true})
	rated := Signals{Rating: map[string]float64{"huge-70b": 2, "coder-7b": 2}}
	if got, _ := ChooseWith(Research, EffortAuto, "", append(library, huge), ram, rated); got.Model.ID == "huge-70b" {
		t.Fatal("a model that doesn't fit was chosen for its rating")
	}
	if got, _ := ChooseWith(Current, EffortAuto, "", library, ram, Signals{Rating: map[string]float64{"gemma-9b": 2}}); got.Model.ID == "gemma-9b" {
		t.Fatal("a model without tools was chosen for a request that needs them")
	}
}

func TestBusyModelsInAuto(t *testing.T) {
	const ram = 24 * gb
	loadedBig, loadedMid := big, mid
	loadedBig.Status, loadedMid.Status = "running", "running"
	models := []contracts.Model{tiny, loadedMid, coder, loadedBig, gemma}

	// The best model is busy: a loaded, idle one at least half its size answers.
	got, _ := ChooseWith(Research, EffortAuto, "", models, ram, Signals{Busy: map[string]bool{"qwen-14b": true}})
	if got.Model.ID != "qwen-7b" || !got.AvoidedBusy {
		t.Fatalf("busy best: %s avoided=%v", got.Model.ID, got.AvoidedBusy)
	}
	// Idle: the best model, as always.
	if got, _ := ChooseWith(Research, EffortAuto, "", models, ram, Signals{}); got.Model.ID != "qwen-14b" || got.AvoidedBusy {
		t.Fatalf("idle: %s", got.Model.ID)
	}
	// No idle loaded alternative: wait for the best one.
	onlyBig := []contracts.Model{tiny, mid, loadedBig}
	if got, _ := ChooseWith(Research, EffortAuto, "", onlyBig, ram, Signals{Busy: map[string]bool{"qwen-14b": true}}); got.Model.ID != "qwen-14b" || got.AvoidedBusy {
		t.Fatalf("no alternative: %s avoided=%v", got.Model.ID, got.AvoidedBusy)
	}
	// A quick question skips a busy loaded model for an idle loaded one.
	loadedTiny := tiny
	loadedTiny.Status = "running"
	quick := []contracts.Model{loadedTiny, loadedMid, gemma}
	if got, _ := ChooseWith(Chat, EffortAuto, "", quick, ram, Signals{Busy: map[string]bool{"qwen-7b": true}}); got.Model.ID != "llama-1b" {
		t.Fatalf("quick with a busy model: %s", got.Model.ID)
	}
}

func TestAutoSaysWhyRatingsOrLoadDecided(t *testing.T) {
	rated := Choice{Model: mid, Kind: Research, Rated: true}
	if got := rated.Reason("en"); got != "Auto chose qwen-7b for "+Research.Describe("en")+", rated higher on computers like yours" {
		t.Fatalf("rated reason = %q", got)
	}
	busy := Choice{Model: mid, Kind: Research, AvoidedBusy: true, Busy: big}
	if got := busy.Reason("en"); got != "Auto chose qwen-7b for "+Research.Describe("en")+", since qwen-14b was busy" {
		t.Fatalf("busy reason = %q", got)
	}
}
