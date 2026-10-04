package huginn

import (
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestClassify(t *testing.T) {
	cases := map[string]Kind{
		"hi there":                                      Chat,
		"What is DNS?":                                  Chat,
		"What's my zip code format in Canada?":          Chat,
		"I take a yoga class on Mondays, any tips?":     Chat,
		"What's the weather in Denver?":                 Current,
		"Who won the game last night?":                  Current,
		"What is the latest news about the Mars rover?": Current,
		"Summarize https://example.com/post":            Current,
		"Fix the bug in my Python script":               Coding,
		"Why does `x := foo()` fail to compile?":        Coding,
		"Write a function that reverses a string":       Coding,
		"What's wrong with my C++ loop":                 Coding,
		"Compare the pros and cons of three NAS drives": Research,
		"Make a plan for moving to a new city":          Research,
		"Run the tests in this repo":                    Local,
		"What files are in my Downloads folder?":        Local,
		"List my files in this folder":                  Local,
		"How do I use underscore in names?":             Chat,
		"":                                              Chat,
	}
	for msg, want := range cases {
		if got := Classify(msg); got != want {
			t.Errorf("Classify(%q) = %s, want %s", msg, got, want)
		}
	}
	if got := Classify(strings.Repeat("Tell me a story about a fox. ", 30)); got != Research {
		t.Errorf("a long request = %s, want research", got)
	}
}

const gb = 1_000_000_000

func model(id string, mem uint64, tags, purpose []string, caps contracts.ModelCapabilities) contracts.Model {
	return contracts.Model{ID: id, DisplayName: id, Installed: true, MemoryNeeded: mem, Tags: tags, Purpose: purpose, Capabilities: caps}
}

var (
	tiny    = model("llama-1b", 1.6*gb, []string{"general", "fast"}, []string{"assistant", "general"}, contracts.ModelCapabilities{ToolCalling: true})
	mid     = model("qwen-7b", 6.4*gb, []string{"general"}, []string{"assistant", "general"}, contracts.ModelCapabilities{ToolCalling: true})
	coder   = model("coder-7b", 6.4*gb, []string{"coding"}, []string{"coding"}, contracts.ModelCapabilities{ToolCalling: true, Coding: true})
	big     = model("qwen-14b", 12*gb, []string{"general", "large", "reasoning"}, []string{"assistant", "general", "research"}, contracts.ModelCapabilities{ToolCalling: true})
	gemma   = model("gemma-9b", 8*gb, []string{"general", "large"}, []string{"assistant", "general"}, contracts.ModelCapabilities{})
	r1      = model("r1-7b", 6.4*gb, []string{"reasoning"}, []string{"research"}, contracts.ModelCapabilities{})
	vision  = model("llava-7b", 7*gb, []string{"vision", "general"}, []string{"assistant", "general"}, contracts.ModelCapabilities{})
	library = []contracts.Model{tiny, mid, coder, big, gemma, r1, vision}
)

func TestChooseMatchesTheRequest(t *testing.T) {
	const ram = 24 * gb // quick ≤ 9.6 GB, full ≤ 14.4 GB
	cases := []struct {
		kind Kind
		want string
	}{
		{Chat, "gemma-9b"},     // largest conversational model that loads fast
		{Coding, "coder-7b"},   // the coding model
		{Research, "qwen-14b"}, // largest that fits
		{Current, "qwen-14b"},  // must call tools; gemma cannot
		{Local, "qwen-14b"},
	}
	for _, c := range cases {
		got, ok := Choose(c.kind, library, ram)
		if !ok || got.Model.ID != c.want {
			t.Errorf("Choose(%s) = %s, want %s", c.kind, got.Model.ID, c.want)
		}
		if !strings.Contains(got.Reason("en"), c.kind.Describe("en")) || !strings.Contains(got.Reason("en"), got.Model.ID) {
			t.Errorf("reason = %q", got.Reason("en"))
		}
	}
}

func TestChooseQuickQuestionUsesTheLoadedModel(t *testing.T) {
	loaded := mid
	loaded.Status = "running"
	got, _ := Choose(Chat, []contracts.Model{tiny, loaded, gemma}, 24*gb)
	if got.Model.ID != "qwen-7b" {
		t.Fatalf("got %s, want the running qwen-7b", got.Model.ID)
	}
}

func TestChooseRespectsMemory(t *testing.T) {
	got, _ := Choose(Research, library, 8*gb) // full ≤ 4.8 GB
	if got.Model.ID != "llama-1b" {
		t.Fatalf("got %s on an 8 GB computer", got.Model.ID)
	}
	// Nothing fits: the smallest model is still better than no answer.
	got, ok := Choose(Chat, []contracts.Model{big, mid}, 4*gb)
	if !ok || got.Model.ID != "qwen-7b" {
		t.Fatalf("got %s %v", got.Model.ID, ok)
	}
	if _, ok := Choose(Chat, nil, 4*gb); ok {
		t.Fatal("no installed model must report false")
	}
}

func TestFallback(t *testing.T) {
	got, ok := Fallback("qwen-14b", library, 24*gb)
	if !ok || got.ID != "gemma-9b" {
		t.Fatalf("fallback from 14b = %s", got.ID)
	}
	if !Smaller(big, got) {
		t.Fatal("9b after 14b should be called out")
	}
	got, ok = Fallback("llama-1b", []contracts.Model{tiny, mid}, 24*gb)
	if !ok || got.ID != "qwen-7b" {
		t.Fatalf("fallback from the smallest = %s", got.ID)
	}
	if _, ok := Fallback("llama-1b", []contracts.Model{tiny}, 24*gb); ok {
		t.Fatal("no other model must report false")
	}
}

func TestSmallAndLarger(t *testing.T) {
	for p, want := range map[string]float64{"1B": 1, "3.8B": 3.8, "500M": 0.5, "14b": 14} {
		if got, ok := Billions(contracts.Model{Parameters: p}); !ok || got != want {
			t.Errorf("Billions(%q) = %v %v", p, got, ok)
		}
	}
	if _, ok := Billions(contracts.Model{Parameters: "large"}); ok {
		t.Error("an unreadable size is unknown")
	}
	a, b := tiny, mid
	a.Parameters, b.Parameters = "1B", "7B"
	if !Small(a) || Small(b) || Small(contracts.Model{}) {
		t.Fatal("small")
	}
	if got, ok := Larger([]contracts.Model{a, b}, 24*gb); !ok || got.ID != "qwen-7b" {
		t.Fatalf("larger = %v %v", got.ID, ok)
	}
	if _, ok := Larger([]contracts.Model{a}, 24*gb); ok {
		t.Fatal("nothing larger")
	}
}

func TestBudgets(t *testing.T) {
	if b := BudgetFor(EffortAuto, Chat); b.Effort != EffortFast || b.Plan || b.Corrections != 0 {
		t.Fatalf("auto chat = %+v", b)
	}
	if b := BudgetFor(EffortAuto, Research); b.Effort != EffortThorough || b.Pages != 2 || b.Corrections != 2 {
		t.Fatalf("auto research = %+v", b)
	}
	if b := BudgetFor(EffortAuto, Current); b.Effort != EffortBalanced {
		t.Fatalf("auto current = %+v", b)
	}
	if b := BudgetFor(EffortBalanced, Chat); b.Effort != EffortBalanced || !b.Plan {
		t.Fatalf("a chosen effort wins = %+v", b)
	}
	if ParseEffort(" Thorough ") != EffortThorough || ParseEffort("max") != EffortAuto {
		t.Fatal("parse")
	}
}

func TestChooseFollowsEffort(t *testing.T) {
	// Thorough takes the largest model even for a quick question.
	if got, _ := ChooseFor(Chat, EffortThorough, library, 24*gb); got.Model.ID != "qwen-14b" || !strings.Contains(got.Reason("en"), "Thorough effort") {
		t.Fatalf("thorough = %+v", got)
	}
	// Fast keeps research on a quick model that is already loaded.
	loaded := mid
	loaded.Status = "running"
	if got, _ := ChooseFor(Research, EffortFast, []contracts.Model{loaded, big}, 24*gb); got.Model.ID != "qwen-7b" {
		t.Fatalf("fast = %+v", got)
	}
}

func TestToolsFor(t *testing.T) {
	all := []string{"internet.search", "internet.open", "filesystem.search", "filesystem.read", "filesystem.write", "files.create", "terminal", "git.status", "git.diff", "git.commit", "git.push"}
	has := func(list []string, ids ...string) bool {
		for _, id := range ids {
			if !slices.Contains(list, id) {
				return false
			}
		}
		return true
	}
	// A plain question is answered directly, without tools (§64).
	if got := ToolsFor(Chat, "What is DNS?", all); len(got) != 0 {
		t.Fatalf("plain question offered %v", got)
	}
	if got := ToolsFor(Chat, "Search the web for how DNS works", all); !has(got, "internet.search") || has(got, "terminal") {
		t.Fatalf("web cue = %v", got)
	}
	// A request to find something offers the web, so the answer comes from
	// it and not from memory or "search for it yourself" (§21).
	for _, msg := range []string{
		"Can you recommend a good bike for someone up to 500lbs?",
		"Where can I buy a used kayak in Juneau?",
		"What's the best espresso machine under $200?",
		"Can you check for me please?",
		"How much does it cost?",
		"Is there a new Go release?",
	} {
		if got := ToolsFor(Chat, msg, all); !has(got, "internet.search") {
			t.Fatalf("%q offered %v", msg, got)
		}
	}
	for _, msg := range []string{"Suggest a name for my gray cat", "Write a haiku about autumn rain", "What is 17 times 23?"} {
		if got := ToolsFor(Chat, msg, all); len(got) != 0 {
			t.Fatalf("%q offered %v", msg, got)
		}
	}
	if got := ToolsFor(Chat, "Make a spreadsheet of these prices", all); !has(got, "files.create") || has(got, "internet.search") {
		t.Fatalf("file cue = %v", got)
	}
	if got := ToolsFor(Chat, "What's in notes.md?", all); !has(got, "filesystem.read") || has(got, "filesystem.write") {
		t.Fatalf("a file name offers reading = %v", got)
	}
	if got := ToolsFor(Coding, "Fix the bug in main.go", all); !has(got, "filesystem.write", "terminal", "git.diff") || has(got, "git.push") {
		t.Fatalf("coding = %v", got)
	}
	if got := ToolsFor(Local, "commit and push my changes", all); !has(got, "git.commit", "git.push") {
		t.Fatalf("git cues = %v", got)
	}
	images := []string{"image.generate", "image.edit", "files.create"}
	for msg, want := range map[string]string{
		"Draw a fox in the snow":                  "image.generate",
		"Can you make a picture of a lighthouse?": "image.generate",
		"Generate a logo for my bakery":           "image.generate",
		"Remove the background from photo.png":    "image.edit",
		"Change the sky in this image to sunset":  "image.edit",
	} {
		if got := ToolsFor(Chat, msg, images); !has(got, want) {
			t.Errorf("%q offered %v, want %s", msg, got, want)
		}
	}
	browse := []string{"browser.open", "browser.click", "browser.type", "internet.search"}
	for _, msg := range []string{"Go to example.com and click the pricing link", "Fill out the contact form on their website", "Take a screenshot of the page"} {
		if got := ToolsFor(Chat, msg, browse); !has(got, "browser.open", "browser.click") {
			t.Errorf("%q offered %v", msg, got)
		}
	}
	film := []string{"video.generate", "image.generate"}
	for _, msg := range []string{"Make a short video of waves on a beach", "Animate this photo", "Bring this picture to life"} {
		if got := ToolsFor(Chat, msg, film); !has(got, "video.generate") {
			t.Errorf("%q offered %v", msg, got)
		}
	}
	mailCal := []string{"email.search", "email.read", "email.send", "calendar.search", "calendar.availability", "calendar.create"}
	for msg, want := range map[string]string{
		"Anything new in my inbox from Sam?":    "email.search",
		"Reply to Alice's message":              "email.send",
		"Am I free tomorrow afternoon?":         "calendar.availability",
		"Schedule a meeting with Sam on Friday": "calendar.create",
	} {
		if got := ToolsFor(Chat, msg, mailCal); !has(got, want) {
			t.Errorf("%q offered %v, want %s", msg, got, want)
		}
	}
	if got := ToolsFor(Chat, "What is a prime number?", mailCal); len(got) != 0 {
		t.Errorf("plain question offered %v", got)
	}
	maps := []string{"places.search", "places.details", "maps.route", "maps.distance"}
	for _, msg := range []string{"Find coffee near me in Juneau", "Directions from the airport to downtown", "How far is Anchorage from Juneau?", "Is the pharmacy open now?"} {
		if got := ToolsFor(Chat, msg, maps); !has(got, "places.search", "maps.route") {
			t.Errorf("%q offered %v", msg, got)
		}
	}
	for _, msg := range []string{"Where is the bug in this function?", "Add a route to the API"} {
		if got := ToolsFor(Chat, msg, maps); len(got) != 0 {
			t.Errorf("%q offered %v", msg, got)
		}
	}
	if got := ToolsFor(Chat, "What is a pixel?", images); len(got) != 0 {
		t.Errorf("plain question offered %v", got)
	}
	// Only tools the profile has are offered.
	if got := ToolsFor(Local, "run the tests", []string{"terminal"}); len(got) != 1 {
		t.Fatalf("limited profile = %v", got)
	}
}

func TestDeflects(t *testing.T) {
	for _, answer := range []string{
		"I'm a text-based AI model, I don't have direct access to real-time location-based data.",
		`You can search for "Juneau weather" on a search engine like Google.`,
		"I'm not sure, the user needs to check a bike website for that.",
		"I'll search the web for you. [Searching on iPhone's Safari]",
		"You can find these by visiting specialty bike shops or online retailers. Websites like REI list them.",
	} {
		if !Deflects(answer) {
			t.Errorf("not caught: %q", answer)
		}
	}
	for _, answer := range []string{
		"Zize bikes are rated for riders up to 550 lbs: https://zizebikes.com/",
		"It is 48°F with light rain in Juneau.",
		"Paris is the capital of France.",
	} {
		if Deflects(answer) {
			t.Errorf("an answer was caught: %q", answer)
		}
	}
}

func TestSmallTalk(t *testing.T) {
	for _, message := range []string{"Hi! How are you?", "hello", "Thanks so much!", "Good morning, how's it going?", "ok"} {
		if !SmallTalk(message) {
			t.Errorf("not small talk: %q", message)
		}
	}
	for _, message := range []string{"", "Hi! What's the weather in Juneau?", "How are you supposed to season cast iron?", "Thanks. Can you check for me please?"} {
		if SmallTalk(message) {
			t.Errorf("small talk: %q", message)
		}
	}
}
