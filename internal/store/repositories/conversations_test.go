package repositories_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestConversationDeleteAndModel(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	repo := repositories.NewConversationRepo(db.SQL)
	ctx := context.Background()

	created, err := repo.Create(ctx, "Hello", "general-assistant", "model-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ModelID != "model-a" {
		t.Fatalf("model_id = %q", created.ModelID)
	}

	_, err = repo.AddMessage(ctx, created.ID, "user", "hi")
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	modelB := "model-b"
	updated, err := repo.Update(ctx, created.ID, repositories.ConversationPatch{ModelID: &modelB})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ModelID != "model-b" {
		t.Fatalf("updated model_id = %q", updated.ModelID)
	}
	if updated.ProfileID != "general-assistant" {
		t.Fatalf("profile should be unchanged, got %q", updated.ProfileID)
	}

	msgs, err := repo.ListMessages(ctx, created.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages before delete: %v len=%d", err, len(msgs))
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	msgs, err = repo.ListMessages(ctx, created.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected cascade delete of messages, got %d", len(msgs))
	}
	if _, err := repo.Get(ctx, created.ID); err == nil {
		t.Fatal("expected get after delete to fail")
	}
}

func TestMessageMetaRoundTrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := repositories.NewConversationRepo(db.SQL)
	conv, err := repo.Create(ctx, "t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	meta := &contracts.MessageMeta{
		Sources: []contracts.Citation{{Kind: "web", Title: "PSI guide", URL: "https://example.com/psi"}},
		Steps:   []contracts.ActivityStep{{Kind: "search", Text: "Searched the web for “psi”"}},
	}
	if _, err := repo.AddMessage(ctx, conv.ID, "user", "psi?"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddMessageWithMeta(ctx, conv.ID, "assistant", "Use the door sticker.", meta); err != nil {
		t.Fatal(err)
	}
	msgs, err := repo.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].Meta != nil || msgs[1].Meta == nil || msgs[1].Meta.Sources[0].URL != "https://example.com/psi" || msgs[1].Meta.Steps[0].Kind != "search" {
		t.Fatalf("messages = %+v", msgs)
	}
}
