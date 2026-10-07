package simple

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestAskedForMedia(t *testing.T) {
	edit := `Image attached to this message by the user: dog.png. You cannot see it. To change it, call image.edit with {"file": "dog.png"}.`
	for msg, want := range map[string]struct{ ref, tool, file string }{
		"Draw an image of a dog for me please.":                              {"", "image.generate", ""},
		"we installed the tool, please use it to generate an image of a dog": {"", "image.generate", ""},
		"Are you able to make a picture of a dog for me?":                    {"", "image.generate", ""},
		"Paint a lighthouse at dusk":                                         {"", "image.generate", ""},
		"Make a logo for my tire shop":                                       {"", "image.generate", ""},
		"Make a short video of waves on a beach":                             {"", "video.generate", ""},
		"Bring this photo to life":                                           {edit, "video.generate", "dog.png"},
		"Make it night":                                                      {edit, "image.edit", "dog.png"},
		"Remove the background":                                              {edit, "image.edit", "dog.png"},
		"Draw a cat instead":                                                 {edit, "image.generate", ""},
	} {
		got, ok := askedForMedia(msg, want.ref)
		if !ok || got.Tool != want.tool || got.File != want.file {
			t.Errorf("%q = %+v %v, want %s %q", msg, got, ok, want.tool, want.file)
		}
	}
	for _, msg := range []string{
		"Can you make images?",
		"Are you able to generate pictures?",
		"Find pictures of golden retrievers online",
		"Where can I download stock photos of dogs?",
		"How do I draw a dog?",
		"What's in this picture?",
		"Describe this image",
		"Write a poem about a dog",
	} {
		if got, ok := askedForMedia(msg, ""); ok {
			t.Errorf("%q is not a request to make one: %+v", msg, got)
		}
	}
}

type mediaEnv struct {
	scriptedEnv
	toolID string
	args   map[string]any
	fail   error
}

func (e *mediaEnv) ExecuteTool(_ context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.tools++
	e.toolID, e.args = toolID, args
	if e.fail != nil {
		return nil, e.fail
	}
	return map[string]any{"id": "art-1", "name": "dog.png", "kind": "image"}, nil
}

var mediaProfile = contracts.AIProfile{
	Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
	Tools: []contracts.ToolPolicy{{ToolID: "image.generate", Policy: "allow"}, {ToolID: "video.generate", Policy: "allow"}, {ToolID: "internet.search", Policy: "allow"}},
}

func runText(t *testing.T, env *mediaEnv, prompt string, profile contracts.AIProfile) string {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, profile, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	return text
}

// "Draw a dog" makes the picture, though the model would rather say it
// can't: it only writes what to draw, and Toskar calls the tool.
func TestPictureRequestsAreMadeByToskar(t *testing.T) {
	env := &mediaEnv{scriptedEnv: scriptedEnv{replies: []string{
		"Prompt: \"A golden retriever puppy on a sunny lawn, soft natural light, photo style.\"",
		"Here's your dog picture, below.",
	}}}
	text := runText(t, env, "Draw an image of a dog for me please.", mediaProfile)
	if env.tools != 1 || env.toolID != "image.generate" || env.args["prompt"] != "A golden retriever puppy on a sunny lawn, soft natural light, photo style." {
		t.Fatalf("called %s %+v", env.toolID, env.args)
	}
	if text != "Here's your dog picture, below." {
		t.Fatalf("reply = %q", text)
	}
	if last := env.seen[0][len(env.seen[0])-1]; last.Role != "user" || !strings.Contains(last.Content, "description, in English, of the picture") {
		t.Fatalf("the description is asked on the user turn: %+v", last)
	}
}

// A model that writes nothing usable still makes the picture, from the
// person's own words; a failure is said plainly.
func TestPictureFallsBackAndReportsFailure(t *testing.T) {
	env := &mediaEnv{scriptedEnv: scriptedEnv{replies: []string{"", ""}}}
	text := runText(t, env, "Could you please paint a lighthouse at dusk?", mediaProfile)
	if env.args["prompt"] != "paint a lighthouse at dusk" || !strings.Contains(text, "attached below") {
		t.Fatalf("prompt %q, reply %q", env.args["prompt"], text)
	}
	failing := &mediaEnv{scriptedEnv: scriptedEnv{replies: []string{"a lighthouse", ""}}, fail: errors.New("the image took longer than 20 minutes and was stopped")}
	if text := runText(t, failing, "Paint a lighthouse", mediaProfile); !strings.Contains(text, "couldn't make it: the image took longer than 20 minutes") {
		t.Fatalf("failure reply = %q", text)
	}
}

// A clip is made the same way; a profile that denies the tool leaves the
// request to the model.
func TestClipRequestsAndDeniedTools(t *testing.T) {
	env := &mediaEnv{scriptedEnv: scriptedEnv{replies: []string{"Waves rolling onto a beach at sunset, slow pan.", "It's below."}}}
	runText(t, env, "Make a short video of waves on a beach", mediaProfile)
	if env.toolID != "video.generate" || env.args["prompt"] != "Waves rolling onto a beach at sunset, slow pan." {
		t.Fatalf("called %s %+v", env.toolID, env.args)
	}
	denied := &mediaEnv{scriptedEnv: scriptedEnv{replies: []string{"I can describe one instead."}}}
	runText(t, denied, "Draw a dog", contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "image.generate", Policy: "deny"}},
	})
	if denied.tools != 0 {
		t.Fatal("made a picture the profile denies")
	}
}
