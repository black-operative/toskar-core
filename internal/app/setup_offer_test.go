package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A request for an image before image generation is set up is answered with
// the setup: what, how large, and where, and the request to finish after
// (Gungnir §29). A profile that denies image tools is not offered it.
func TestOfferSetupForMissingAbility(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ctx := context.Background()
	if !a.Images.Status().Supported {
		t.Skip("stable-diffusion.cpp has no build for this platform")
	}
	var want imagegen.ModelStatus
	for _, m := range a.Images.Status().Models {
		if m.Recommended {
			want = m
		}
	}
	created, err := a.Conversations.Create(ctx, "t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	conv := created.ID
	allow := profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "image.generate", Policy: "allow"}, {ToolID: "image.edit", Policy: "allow"}}}
	ch, ok := a.offerSetup(ctx, allow, conv, "Make me an image of a Viking tree")
	if !ok {
		t.Fatal("no setup offered")
	}
	var reply string
	for c := range ch {
		reply += c.Content
	}
	if !strings.Contains(reply, "I can set it up on this computer (") || !strings.Contains(reply, want.Name+", ") || !strings.Contains(reply, " GB to download.") || !strings.Contains(reply, "finish this once it's ready") {
		t.Fatalf("reply %q", reply)
	}
	msgs, err := a.Conversations.ListMessages(ctx, conv)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages %d %v", len(msgs), err)
	}
	offer := msgs[1].Meta.Setup
	if offer == nil || offer.Ability != "image_generation" || offer.Option != want.ID || offer.NodeName == "" || offer.Request != "Make me an image of a Viking tree" {
		t.Fatalf("offer %+v", offer)
	}

	if _, ok := a.offerSetup(ctx, profiles.Profile{}, conv, "Make me an image of a Viking tree"); ok {
		t.Error("offered to a profile that denies image tools")
	}
	if _, ok := a.offerSetup(ctx, allow, "", "Make me an image of a Viking tree"); ok {
		t.Error("offered outside a chat")
	}
	if _, ok := a.offerSetup(ctx, allow, conv, "What is the capital of France?"); ok {
		t.Error("offered for an ordinary question")
	}

	// "Can you generate images?" says so and carries the offer too.
	ch, ok = a.answerCapabilityQuestion(ctx, conv, "Can you generate an image?")
	if !ok {
		t.Fatal("no direct answer")
	}
	for range ch {
	}
	msgs, _ = a.Conversations.ListMessages(ctx, conv)
	if last := msgs[len(msgs)-1]; last.Meta == nil || last.Meta.Setup == nil || last.Meta.Setup.Request != "" {
		t.Fatalf("question meta %+v", last.Meta)
	}
}
