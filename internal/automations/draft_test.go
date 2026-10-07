package automations_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A run that notifies posts its result, without the JSON its condition
// asked for, to the chat the automation came from, and the notice opens
// that chat. A post that fails leaves the notice opening the automation.
func TestResultGoesToTheChatItCameFrom(t *testing.T) {
	for _, postFails := range []bool{false, true} {
		db := openAutomationDB(t)
		repo := repositories.NewAutomationRepo(db.SQL)
		ctx := context.Background()
		createdAt := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
		created, err := repo.Create(ctx, automations.CreateInput{
			ModelID: "auto", Name: "Price", Prompt: "Check the price.", ConversationID: "conv-1", DraftID: "d",
			Notification: priceBelow,
			Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		}, createdAt)
		if err != nil {
			t.Fatal(err)
		}
		var posted []string
		notify := &recordingNotifier{}
		clock := createdAt.Add(90 * time.Minute)
		runner := &automations.Runner{
			Store: repo, Exec: &scriptedExec{text: "It's €420 today.\n{\"price\": 420}"}, Notify: notify,
			Now: func() time.Time { return clock }, Lease: time.Hour,
			Post: func(_ context.Context, a automations.Automation, run automations.Run, text string) error {
				if postFails {
					return context.Canceled
				}
				if a.ID != created.ID || run.ID == "" {
					t.Errorf("posted for %s run %q", a.ID, run.ID)
				}
				posted = append(posted, text)
				return nil
			},
		}
		if err := runner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		if len(notify.items) != 1 {
			t.Fatalf("%d notices", len(notify.items))
		}
		if postFails {
			if notify.items[0].ConversationID != "" {
				t.Fatal("notice opens a chat the result never reached")
			}
			continue
		}
		if len(posted) != 1 || posted[0] != "It's €420 today." || notify.items[0].ConversationID != "conv-1" {
			t.Fatalf("posted %q, notice %+v", posted, notify.items[0])
		}
	}
}

// An automation with a save folder also saves each result as a file, and
// the run says where (#204).
func TestResultIsSavedToItsFolder(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(automations.SetHomeDir(home))
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "auto", Name: "Price", Prompt: "Check the price.", SaveFolder: "~/Toskar/Prices", Notification: priceBelow,
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: &scriptedExec{text: "It's €640 today.\n{\"price\": 640}"}, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.History(ctx, created.ID)
	saved := detail.History[0].SavedFile
	if !strings.HasPrefix(saved, filepath.Join(home, "Toskar", "Prices")+string(filepath.Separator)) {
		t.Fatalf("saved file = %q", saved)
	}
	body, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(body), "It's €640 today.") || strings.Contains(string(body), `"price"`) {
		t.Fatalf("body = %q, %v", body, err)
	}
	// Outside the home folder is refused when saved.
	if _, err := repo.Update(ctx, created.ID, automations.Patch{SaveFolder: ptr("/etc")}, clock); err == nil {
		t.Fatal("saved a folder outside the home folder")
	}
}

func ptr[T any](v T) *T { return &v }
