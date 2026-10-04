package huginn

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

var tireShop = Specialist{
	ID: "sai:tire-assistant", Name: "Tire Assistant", Goal: "Help tire shop customers find the right tires",
	Questions: []string{
		"What tire size fits a 2018 Honda Civic?",
		"Do you have winter tires in 225/45R17?",
		"How much is a tire rotation and balance?",
		"Which all-season tires are quiet on the highway?",
		"What does the load index on a tire mean?",
		"Can I mix winter and all-season tires?",
	},
}

var recipes = Specialist{
	ID: "sai:kitchen", Name: "Kitchen Helper", Goal: "Suggest recipes from what is in the pantry",
	Questions: []string{"What can I cook with rice and beans?", "A quick recipe with chicken and rice?", "Dessert recipe without eggs?"},
}

func TestChooseSpecialist(t *testing.T) {
	list := []Specialist{recipes, tireShop}
	for _, tc := range []struct {
		message string
		kind    Kind
		want    string
	}{
		{"What winter tires fit a 2019 Civic?", Chat, "sai:tire-assistant"},
		{"Is a tire rotation worth it before winter?", Chat, "sai:tire-assistant"},
		{"Ask the Tire Assistant: hello there", Chat, "sai:tire-assistant"},
		{"Give me a recipe using rice and chicken", Chat, "sai:kitchen"},
		// Not about either.
		{"What is the capital of France?", Chat, ""},
		{"Write a poem about the ocean and the sky", Chat, ""},
		// One topic word in a long, unrelated message is not enough.
		{"Explain how the federal reserve sets interest rates and why a tire company would care about inflation trends", Research, ""},
		// Needs tools, current information, or code: never a specialist.
		{"What are winter tire prices today?", Current, ""},
		{"Write a Python script that lists tire sizes", Coding, ""},
		{"Find tire invoices in my Documents folder", Local, ""},
		{"", Chat, ""},
	} {
		got, reason, ok := ChooseSpecialist(tc.message, tc.kind, list)
		if tc.want == "" {
			if ok {
				t.Errorf("%q routed to %s (%s)", tc.message, got.ID, reason)
			}
			continue
		}
		if !ok || got.ID != tc.want {
			t.Errorf("%q = %q, want %s", tc.message, got.ID, tc.want)
			continue
		}
		if !strings.HasPrefix(reason.Render("en"), "Auto chose "+got.Name) {
			t.Errorf("reason = %q", reason)
		}
	}
	if _, reason, _ := ChooseSpecialist("What winter tires fit a 2019 Civic?", Chat, list); !strings.Contains(reason.Render("en"), "(tire, winter)") {
		t.Errorf("reason = %q", reason)
	}
	if _, _, ok := ChooseSpecialist("What winter tires fit a 2019 Civic?", Chat, nil); ok {
		t.Error("routed with no specialists")
	}
}

func TestSupportingModelsNeverAnswer(t *testing.T) {
	chat := contracts.Model{ID: "llama-3.2-1b-q4", DisplayName: "Llama 3.2 1B", Installed: true, MemoryNeeded: 2 << 30, Tags: []string{"general"}}
	embed := contracts.Model{ID: "nomic-embed-text-v1.5-q4", Installed: true, MemoryNeeded: 4 << 30, Status: "running"}
	rerank := contracts.Model{ID: "custom", DisplayName: "BGE Reranker v2", Installed: true, MemoryNeeded: 3 << 30}
	classifier := contracts.Model{ID: "intent", Installed: true, Purpose: []string{"classifier"}, MemoryNeeded: 3 << 30}
	listed := contracts.Model{ID: "listed", Installed: true, SupportRole: contracts.SupportEmbedding, MemoryNeeded: 5 << 30}
	for m, want := range map[*contracts.Model]string{&chat: "", &embed: "embedding", &rerank: "reranker", &classifier: "classifier", &listed: "embedding"} {
		if got := SupportRoleOf(*m); got != want {
			t.Errorf("%s: role %q, want %q", m.ID, got, want)
		}
	}
	all := []contracts.Model{embed, rerank, classifier, listed, chat}
	for _, k := range []Kind{Chat, Research, Coding, Current} {
		if c, ok := ChooseFor(k, EffortThorough, all, 0); !ok || c.Model.ID != chat.ID {
			t.Errorf("%s chose %q", k, c.Model.ID)
		}
	}
	if m, ok := Fallback("other", append(all, contracts.Model{ID: "other", Installed: true}), 0); !ok || m.ID != chat.ID {
		t.Errorf("fallback = %q", m.ID)
	}
	if _, ok := ChooseFor(Chat, EffortAuto, []contracts.Model{embed}, 0); ok {
		t.Error("an embedding model was chosen to chat")
	}
}

func TestToolsForConnectedServices(t *testing.T) {
	available := []string{"internet.search", "github.search", "github.issue", "homeassistant.states", "homeassistant.call"}
	tools.SetConnected("homeassistant", []tools.Definition{
		{ID: "homeassistant.states", Risk: tools.RiskRead},
		{ID: "homeassistant.call", Risk: tools.RiskWrite},
	})
	t.Cleanup(func() { tools.SetConnected("homeassistant", nil) })
	has := func(list []string, id string) bool {
		for _, v := range list {
			if v == id {
				return true
			}
		}
		return false
	}
	gh := ToolsFor(Chat, "What open issues are there in yeixio/toskar-core?", available)
	if !has(gh, "github.search") || has(gh, "homeassistant.states") {
		t.Errorf("issues question offered %v", gh)
	}
	ha := ToolsFor(Local, "Turn off the kitchen lights", available)
	if !has(ha, "homeassistant.call") || has(ha, "github.search") {
		t.Errorf("lights request offered %v", ha)
	}
	// A question is never offered a way to change something.
	q := ToolsFor(Current, "Which lights are on right now?", available)
	if !has(q, "homeassistant.states") || has(q, "homeassistant.call") {
		t.Errorf("lights question offered %v", q)
	}
	// Words the service taught, such as device names.
	tools.SetConnected("homeassistant", []tools.Definition{
		{ID: "homeassistant.states", Risk: tools.RiskRead, Cues: []string{"porch temperature"}},
		{ID: "homeassistant.call", Risk: tools.RiskWrite, Cues: []string{"porch temperature"}},
	})
	if got := ToolsFor(Chat, "What is the porch temperature?", available); !has(got, "homeassistant.states") || has(got, "homeassistant.call") {
		t.Errorf("device question offered %v", got)
	}
	if got := ToolsFor(Chat, "What temperature should I bake bread at?", available); has(got, "homeassistant.states") {
		t.Errorf("unrelated question offered %v", got)
	}
	if got := ToolsFor(Chat, "Ask Home Assistant what the porch temperature is", available); !has(got, "homeassistant.states") {
		t.Errorf("named service offered %v", got)
	}
	if got := ToolsFor(Chat, "What is the capital of France?", available); has(got, "github.search") || has(got, "homeassistant.states") {
		t.Errorf("unrelated question offered %v", got)
	}
}

func TestToolsForMCPSources(t *testing.T) {
	available := []string{"internet.search", "linear.list_issues", "linear.create_issue", "time.get_current_time"}
	tools.SetConnected("mcp:linear", []tools.Definition{
		{ID: "linear.list_issues", Risk: tools.RiskRead, Cues: []string{"linear", "ticket", "tickets"}},
		{ID: "linear.create_issue", Risk: tools.RiskWrite, Cues: []string{"linear", "ticket", "tickets"}},
	})
	tools.SetConnected("mcp:time", []tools.Definition{{ID: "time.get_current_time", Risk: tools.RiskRead, Always: true}})
	t.Cleanup(func() {
		tools.SetConnected("mcp:linear", nil)
		tools.SetConnected("mcp:time", nil)
	})
	has := func(list []string, id string) bool {
		for _, v := range list {
			if v == id {
				return true
			}
		}
		return false
	}
	if got := ToolsFor(Chat, "Which tickets are assigned to me?", available); !has(got, "linear.list_issues") || has(got, "linear.create_issue") {
		t.Errorf("ticket question offered %v", got)
	}
	if got := ToolsFor(Chat, "Add a ticket in Linear for the login bug", available); !has(got, "linear.create_issue") {
		t.Errorf("ticket request offered %v", got)
	}
	// A source set to always be offered comes with every request.
	got := ToolsFor(Chat, "What is the capital of France?", available)
	if !has(got, "time.get_current_time") || has(got, "linear.list_issues") {
		t.Errorf("unrelated question offered %v", got)
	}
}

func TestClaimsAction(t *testing.T) {
	for _, a := range []string{"Done! Your Downloads folder is now empty.", "I've deleted the files.", "The email has been sent.", "I successfully pushed the commit."} {
		if !ClaimsAction(a) {
			t.Errorf("missed %q", a)
		}
	}
	for _, a := range []string{"I did not delete anything.", "You can delete them with rm.", "Deleted files go to the Trash.", "The capital of France is Paris."} {
		if ClaimsAction(a) {
			t.Errorf("false alarm %q", a)
		}
	}
}
