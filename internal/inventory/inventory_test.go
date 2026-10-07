package inventory

import (
	"strings"
	"testing"
)

func snapshot() Snapshot {
	return Snapshot{
		Models: []Model{
			{ID: "llama-3.2-1b-q4", Name: "Llama 3.2 1B", ToolCalling: true, MemoryNeeded: 2 << 30, On: []string{"This Mac"}},
			{ID: "qwen2.5-14b-q4", Name: "Qwen 2.5 14B", MemoryNeeded: 12 << 30, On: []string{"Studio"}},
		},
		Nodes: []Node{
			{ID: "here", Name: "This Mac", Local: true, Online: true, MemoryBytes: 8 << 30, Trainer: "MLX"},
			{ID: "studio", Name: "Studio", Online: true, MemoryBytes: 64 << 30},
			{ID: "old", Name: "Old PC", Online: false, MemoryBytes: 16 << 30},
		},
		Tools: []Tool{
			{ID: "internet.search", Name: "Web Search", Source: "builtin", Enabled: true},
			{ID: "terminal", Name: "Terminal", Source: "builtin", Enabled: true},
			{ID: "files.create", Name: "Create File", Source: "builtin", Enabled: true},
			{ID: "git.status", Name: "Git Status", Source: "builtin", Enabled: false},
			{ID: "mail.send", Name: "Send Gmail message", Description: "Send an email from Gmail", Source: "mcp:gmail", Enabled: true},
		},
		Connectors: []Connector{{ID: "github", Name: "GitHub", Connected: true}, {ID: "homeassistant", Name: "Home Assistant"}},
	}
}

func ability(t *testing.T, list []Ability, id string) Ability {
	t.Helper()
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no ability %s", id)
	return Ability{}
}

func TestAbilities(t *testing.T) {
	all := Abilities(snapshot())
	for id, want := range map[string]bool{
		"web": true, "run_code": true, "make_files": true, "git": false, "email": true, "github": true,
		"home": false, "image_generation": false, "vision": false, "training": true, "other_computers": true, "meaning_search": false,
	} {
		if a := ability(t, all, id); a.Available != want {
			t.Errorf("%s = %+v", id, a)
		}
	}
	if a := ability(t, all, "email"); a.Via[0] != "Send Gmail message" {
		t.Errorf("email via = %v", a.Via)
	}
	if a := ability(t, all, "image_generation"); !strings.Contains(a.Note, "tool source") {
		t.Errorf("image note = %q", a.Note)
	}
}

func TestCapabilityQuestions(t *testing.T) {
	s := snapshot()
	for q, want := range map[string]string{
		"Can you generate images?":        "image_generation",
		"Can you execute Python?":         "run_code",
		"Do you have access to my email?": "email",
		"Can you search the web?":         "web",
		"Are you able to use GitHub?":     "github",
	} {
		got := Ask(s, q)
		if len(got) == 0 || got[0].ID != want {
			t.Errorf("%q = %+v, want %s", q, got, want)
		}
	}
	// A question that names something to make asks for it.
	for _, q := range []string{"Generate an image of a cat", "Can you generate an image of a cat?", "Are you able to make a picture of a dog for me?", "Could you draw me a fox?", "What is the capital of France?", "Run the tests"} {
		if got := Ask(s, q); len(got) != 0 {
			t.Errorf("%q treated as a capability question: %+v", q, got)
		}
	}
	facts := Facts(s, "Can you generate an image?")
	if !strings.Contains(facts, "Generate images: no. No image model") || !strings.Contains(facts, "do not claim abilities") {
		t.Errorf("facts = %q", facts)
	}
}

func TestWhichComputerCanRunAModel(t *testing.T) {
	s := snapshot()
	places, ok := NodesFor(s, "qwen2.5-14b-q4")
	if !ok || len(places) != 3 {
		t.Fatalf("places = %+v", places)
	}
	if places[0].Node != "Studio" || places[0].Note != "can run it now" {
		t.Errorf("best = %+v", places[0])
	}
	for _, p := range places {
		if p.Node == "This Mac" && (p.Fits || !strings.Contains(p.Note, "needs about 12.0 GB")) {
			t.Errorf("this mac = %+v", p)
		}
		if p.Node == "Old PC" && p.Note != "offline" {
			t.Errorf("old = %+v", p)
		}
	}
	if _, ok := NodesFor(s, "nope"); ok {
		t.Error("unknown model")
	}
	facts := Facts(s, "Which computer can run Qwen 2.5 14B?")
	if !strings.Contains(facts, "Qwen 2.5 14B on Studio: can run it now") {
		t.Errorf("facts = %q", facts)
	}
}

func TestDirectAnswers(t *testing.T) {
	s := snapshot()
	for q, want := range map[string]string{
		"Can you generate images?":             "No, I can't generate images right now. No image model",
		"Can you execute Python?":              "Yes, I can run commands and code, using Terminal.",
		"Which computer can run Qwen 2.5 14B?": "Qwen 2.5 14B:\n- Studio: can run it now",
	} {
		got, ok := Direct(s, q)
		if !ok || !strings.HasPrefix(got, want) {
			t.Errorf("%q = %q, %v", q, got, ok)
		}
	}
	for _, q := range []string{
		"Generate an image of a cat",
		"Can you generate an image of a cat?",
		"Can you search the web and also run Python to chart the results for me?",
		"Can you explain what a capital city is and why it matters for a country's government?",
	} {
		if got, ok := Direct(s, q); ok {
			t.Errorf("%q answered directly: %q", q, got)
		}
	}
}

// Image generation that is on but not set up says how to set it up, and does
// not count as available.
func TestImageGenerationNotSetUp(t *testing.T) {
	s := snapshot()
	s.Tools = append(s.Tools, Tool{ID: "image.generate", Name: "Generate Image", Description: "Make an image from a description",
		Source: "builtin", Enabled: true, Unavailable: "image generation isn't set up yet. Set it up on the Tools page"})
	got, ok := Direct(s, "Can you generate images?")
	if !ok || got != "No, I can't generate images right now. Image generation isn't set up yet. Set it up on the Tools page." {
		t.Fatalf("answer %q", got)
	}
	s.Tools[len(s.Tools)-1].Unavailable = ""
	if a := ability(t, Abilities(s), "image_generation"); !a.Available {
		t.Fatalf("a ready image tool: %+v", a)
	}
}

// A request for a missing ability that can be installed is found; a
// question, an available ability, or one without a setup is not.
func TestNeeds(t *testing.T) {
	s := snapshot()
	s.Setups = []Setup{{Ability: "image_generation", Option: "flux2-klein-4b", Name: "FLUX.2 [klein] 4B", SizeBytes: 5_207_178_964, Tools: []string{"image.generate"}}}
	// A request asked as a question, or to draw something, needs it too.
	for _, msg := range []string{"Make me an image of a Viking tree", "Are you able to make a picture of a dog for me?", "Can you draw a dog?", "Draw me a fox"} {
		a, ok := Needs(s, msg)
		if !ok || a.ID != "image_generation" || a.Setup == nil || a.Setup.Option != "flux2-klein-4b" {
			t.Fatalf("%q needs = %+v %v", msg, a, ok)
		}
	}
	for _, msg := range []string{"Can you generate images?", "What is the capital of France?", "Run the tests"} {
		if a, ok := Needs(s, msg); ok {
			t.Errorf("%q needs %s", msg, a.ID)
		}
	}
	if a := ability(t, Abilities(s), "image_generation"); a.Setup == nil {
		t.Error("the ability does not carry its setup")
	}
	s.Setups = nil
	if _, ok := Needs(s, "Make me an image of a Viking tree"); ok {
		t.Error("offered with nothing to install")
	}
}

// Video generation is its own ability, offered a setup when it can be
// installed.
func TestVideoAbility(t *testing.T) {
	s := snapshot()
	s.Setups = []Setup{{Ability: "video_generation", Option: "wan2.2-ti2v-5b", Name: "Wan 2.2 TI2V 5B", SizeBytes: 8_497_662_272, Tools: []string{"video.generate"}}}
	if a, ok := Needs(s, "Make a short video of waves on a beach"); !ok || a.ID != "video_generation" {
		t.Fatalf("needs %+v %v", a, ok)
	}
	s.Tools = append(s.Tools, Tool{ID: "video.generate", Name: "Generate Video", Enabled: true})
	if a := ability(t, Abilities(s), "video_generation"); !a.Available || a.Setup != nil {
		t.Fatalf("ready %+v", a)
	}
}
