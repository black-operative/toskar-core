package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/store"
)

func TestTurnInstructionsRetrieveProfileKnowledge(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	kb := mimir.NewStore(db.SQL, t.TempDir())
	src, err := kb.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: "stock.csv",
		Text: "sku,size,price\nMP-22545,225/45R17,189.99\n"})
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(8)
	subID, sub := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	a := &App{Mimir: kb, Bus: bus}

	env := &chatExecEnv{app: a, ctx: ctx,
		profile:      profiles.Profile{KnowledgeSources: []string{src.ID}},
		instructions: "You are Tire Bot."}
	// Trusted instructions and untrusted knowledge are kept apart (§58).
	if got := env.TurnInstructions(ctx, "price for 225/45R17?"); !strings.HasPrefix(got, "You are Tire Bot.") || strings.Contains(got, "189.99") {
		t.Fatalf("instructions = %q", got)
	}
	ref := env.ReferenceMaterial(ctx, "price for 225/45R17?")
	if !strings.Contains(ref, "price: 189.99") || strings.Contains(ref, "Tire Bot") {
		t.Fatalf("reference = %q", ref)
	}
	evt := <-sub
	if evt.Type != "knowledge.retrieved" || evt.Payload["passages"] != 1 {
		t.Fatalf("event = %+v", evt)
	}

	none := &chatExecEnv{app: a, ctx: ctx}
	if got := none.ReferenceMaterial(ctx, "price for 225/45R17?"); got != "" {
		t.Fatalf("a profile without knowledge got %q", got)
	}
}
