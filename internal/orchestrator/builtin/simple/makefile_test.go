package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestAskedForFile(t *testing.T) {
	cases := map[string]fileRequest{
		"Create a spreadsheet file named tires.xlsx with columns item and price": {Name: "tires.xlsx", Table: true},
		"Can you make me a spreadsheet of these prices?":                         {Name: "spreadsheet.xlsx", Table: true},
		"Export this as a CSV":                               {Name: "data.csv", Table: true},
		"Write a short report on the trip":                   {Name: "document.md"},
		"Put the summary in a markdown file called notes.md": {Name: "notes.md"},
	}
	for msg, want := range cases {
		got, ok := askedForFile(msg)
		if !ok || got != want {
			t.Errorf("askedForFile(%q) = %+v %v, want %+v", msg, got, ok, want)
		}
	}
	for _, msg := range []string{"What is a CSV file?", "How do I open a spreadsheet?", "Summarize this file."} {
		if _, ok := askedForFile(msg); ok {
			t.Errorf("%q is not a request for a file", msg)
		}
	}
}

func TestFileBody(t *testing.T) {
	if got := fileBody("Sure! Here it is:\n```csv\nitem,price\nTire,189.99\n```\nLet me know!", true); got != "item,price\nTire,189.99" {
		t.Fatalf("fenced = %q", got)
	}
	if got := fileBody("Here is your data:\nitem,price\nTire,1", true); got != "item,price\nTire,1" {
		t.Fatalf("prose before the table = %q", got)
	}
	if got := fileBody("# Trip\n\nIt went well.", false); got != "# Trip\n\nIt went well." {
		t.Fatalf("plain = %q", got)
	}
}

type fileEnv struct {
	scriptedEnv
	created map[string]any
}

func (e *fileEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.tools++
	e.created = args
	return map[string]any{"name": args["name"]}, nil
}

func TestFileRequestsAreMadeByYggdrasil(t *testing.T) {
	env := &fileEnv{scriptedEnv: scriptedEnv{replies: []string{"```csv\nitem,price\nTire,189.99\nWiper,12\n```"}}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "Create a spreadsheet named tires.xlsx of the prices"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "files.create", Policy: "allow"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	if env.tools != 1 || env.created["name"] != "tires.xlsx" || env.created["content"] != "item,price\nTire,189.99\nWiper,12" {
		t.Fatalf("created %+v", env.created)
	}
	if text != "Here is **tires.xlsx**, with 2 rows. It's attached below." {
		t.Fatalf("reply = %q", text)
	}
	last := env.seen[0][len(env.seen[0])-1]
	if last.Role != "user" || !strings.Contains(last.Content, "as CSV: a header row") {
		t.Fatalf("the instruction must ride on the user turn: %+v", last)
	}
	for i := 1; i < len(env.seen[0]); i++ {
		if env.seen[0][i].Role == env.seen[0][i-1].Role && env.seen[0][i].Role != "system" {
			t.Fatal("roles must alternate")
		}
	}
	_ = pluginapi.ChatMessage{}
}

func TestFileRequestNeedsPermission(t *testing.T) {
	env := &fileEnv{scriptedEnv: scriptedEnv{replies: []string{"ok"}}}
	events, _ := New().Run(context.Background(), contracts.Task{Prompt: "Create a spreadsheet of the prices"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "files.create", Policy: "ask"}},
	}, env)
	for range events {
	}
	if env.tools != 0 {
		t.Fatal("a file was made without permission")
	}
}
