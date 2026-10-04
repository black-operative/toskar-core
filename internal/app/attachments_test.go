package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
)

func TestAttachmentsReachTheTurnAsData(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()
	a.Artifacts = artifacts.NewStore(a.DB.SQL, t.TempDir())

	// Uploaded before the chat existed, then sent with a message.
	f, err := a.Artifacts.Save(ctx, artifacts.Input{Name: "warranty.md", Data: []byte("# Warranty\n\nTread wear is covered for 60,000 miles.")})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := a.resolveAttachments(ctx, conv, []string{f.ID})
	if err != nil || len(attached) != 1 || attached[0].ConversationID != conv {
		t.Fatalf("resolve = %+v %v", attached, err)
	}
	env := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, conversationID: conv, trace: &turnTrace{}, attachments: attached}
	block := env.attachmentBlock(ctx, "Summarize this")
	if !strings.Contains(block, "File attached to this message by the user: warranty.md") || !strings.Contains(block, "60,000 miles") {
		t.Fatalf("block = %q", block)
	}
	meta := env.trace.meta()
	if meta == nil || meta.Steps[0].Text != "Read warranty.md" || meta.Sources[0].Kind != "file" || !env.trace.sawUntrusted() {
		t.Fatalf("meta = %+v", meta)
	}

	// On a later turn the file is still there, but adds only what matches.
	later := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, conversationID: conv, trace: &turnTrace{}}
	if got := later.attachmentBlock(ctx, "What does the warranty cover?"); !strings.Contains(got, "attached earlier in this chat") {
		t.Fatalf("later = %q", got)
	}

	// A file from another chat is refused.
	other, _ := a.Conversations.Create(ctx, "other", "", "")
	if _, err := a.resolveAttachments(ctx, other.ID, []string{f.ID}); err == nil {
		t.Fatal("a file from another chat was accepted")
	}
	if _, err := a.resolveAttachments(ctx, conv, []string{"missing"}); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestCreatedFilesAreListedWithTheAnswer(t *testing.T) {
	tr := &turnTrace{}
	tr.tool("files.create", map[string]any{"name": "Budget.xlsx"}, map[string]any{
		"id": "f1", "name": "Budget.xlsx", "kind": "spreadsheet", "mime_type": "application/x", "size_bytes": int64(5120),
	})
	meta := tr.meta()
	if meta == nil || len(meta.Files) != 1 || meta.Files[0].Producer != "assistant" || meta.Files[0].Size != 5120 || meta.Steps[0].Text != "Created Budget.xlsx" {
		t.Fatalf("meta = %+v", meta)
	}
	if !tr.hasSideEffects() {
		t.Fatal("a turn that created a file must not be retried")
	}
}

type readyTool struct {
	id    string
	ready bool
}

func (r readyTool) ID() string                { return r.id }
func (r readyTool) DisplayName() string       { return r.id }
func (r readyTool) Description() string       { return r.id }
func (r readyTool) Available() (bool, string) { return r.ready, "not set up" }
func (r readyTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return nil, nil
}

// An image is not read as text. Its note offers image.edit only when images
// can be edited here, so the model is never pointed at a tool that cannot run.
func TestImageAttachmentNote(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()
	a.Artifacts = artifacts.NewStore(a.DB.SQL, t.TempDir())
	a.Tools = tools.NewRegistry(t.TempDir(), a.Bus)
	f, err := a.Artifacts.Save(ctx, artifacts.Input{ConversationID: conv, Name: "beach.jpg", Producer: artifacts.ProducerUser, Data: []byte("\xff\xd8\xff")})
	if err != nil {
		t.Fatal(err)
	}
	env := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, conversationID: conv, trace: &turnTrace{}, attachments: []artifacts.Artifact{f}}
	block := env.attachmentBlock(ctx, "Make it sunset")
	if !strings.Contains(block, "Image attached to this message by the user: beach.jpg. You cannot see it.") || strings.Contains(block, "image.edit") {
		t.Fatalf("not set up: %q", block)
	}
	a.Tools.Register(readyTool{id: ImageEditToolID})
	if block := env.attachmentBlock(ctx, "Make it sunset"); strings.Contains(block, "image.edit") {
		t.Fatalf("unavailable tool offered: %q", block)
	}
	a.Tools.Register(readyTool{id: ImageEditToolID, ready: true})
	if block := env.attachmentBlock(ctx, "Make it sunset"); !strings.Contains(block, `To change it, call image.edit with {"file": "beach.jpg"}.`) {
		t.Fatalf("ready: %q", block)
	}
}
