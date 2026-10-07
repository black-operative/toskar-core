package automations_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// An automation made from a chat keeps its conversation, and pressing
// Create on the same draft again returns it instead of a second one (#204).
func TestCreatingADraftTwiceMakesOne(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
	in := automations.CreateInput{
		ModelID: "auto", Name: "News", Prompt: "Summarize the news.", ProfileID: "general-assistant",
		Schedule:       automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		ConversationID: "conv-1", DraftID: "draft-1",
	}
	first, err := repo.Create(ctx, in, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Create(ctx, in, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("second automation %s for the same draft", again.ID)
	}
	got, err := repo.Get(ctx, first.ID)
	if err != nil || got.ConversationID != "conv-1" || got.DraftID != "draft-1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	list, _ := repo.List(ctx)
	if len(list) != 1 {
		t.Fatalf("%d automations", len(list))
	}
}
