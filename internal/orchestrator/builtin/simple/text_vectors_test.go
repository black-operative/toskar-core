package simple

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/tools"
)

// textVectors is tests/quality/text.json: inputs and core's outputs for the
// text helpers the iPhone app keeps a copy of. The phone's quality run checks
// the same file against its copy, so a change here that the phone has not
// followed fails there.
type textVectors struct {
	About         string         `json:"about"`
	NeedsLiveWeb  []boolVector   `json:"needs_live_web"`
	Classify      []stringVector `json:"classify"`
	SmallTalk     []boolVector   `json:"small_talk"`
	LookupQuery   []stringVector `json:"lookup_query"`
	Deflects      []boolVector   `json:"deflects"`
	CleanQuery    []stringVector `json:"clean_query"`
	MemoryCommand []memoryVector `json:"memory_command"`
}

type boolVector struct {
	In   string `json:"in"`
	Want bool   `json:"want"`
}

type stringVector struct {
	In   string `json:"in"`
	Want string `json:"want"`
}

type memoryVector struct {
	In   string `json:"in"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

const textVectorsPath = "../../../../tests/quality/text.json"

// TestTextVectors holds core to tests/quality/text.json. With
// TOSKAR_UPDATE_TEXT_VECTORS=1 it writes core's current outputs instead.
func TestTextVectors(t *testing.T) {
	raw, err := os.ReadFile(textVectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v textVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	update := os.Getenv("TOSKAR_UPDATE_TEXT_VECTORS") == "1"

	checkBool := func(name string, list []boolVector, f func(string) bool) {
		for i := range list {
			got := f(list[i].In)
			if update {
				list[i].Want = got
			} else if got != list[i].Want {
				t.Errorf("%s(%q) = %v, want %v", name, list[i].In, got, list[i].Want)
			}
		}
	}
	checkString := func(name string, list []stringVector, f func(string) string) {
		for i := range list {
			got := f(list[i].In)
			if update {
				list[i].Want = got
			} else if got != list[i].Want {
				t.Errorf("%s(%q) = %q, want %q", name, list[i].In, got, list[i].Want)
			}
		}
	}

	checkBool("MessageNeedsLiveWeb", v.NeedsLiveWeb, tools.MessageNeedsLiveWeb)
	checkBool("SmallTalk", v.SmallTalk, huginn.SmallTalk)
	checkBool("Deflects", v.Deflects, huginn.Deflects)
	checkString("lookupQuery", v.LookupQuery, lookupQuery)
	checkString("cleanQuery", v.CleanQuery, cleanQuery)

	// The phone has no local kind: it has no files or commands to act on.
	var classify []stringVector
	for _, c := range v.Classify {
		if !update || huginn.Classify(c.In) != huginn.Local {
			classify = append(classify, c)
		}
	}
	checkString("Classify", classify, func(s string) string { return string(huginn.Classify(s)) })
	v.Classify = classify

	for i := range v.MemoryCommand {
		cmd := muninn.ParseCommand(v.MemoryCommand[i].In)
		kind := string(cmd.Kind)
		if kind == "" {
			kind = "none"
		}
		if update {
			v.MemoryCommand[i].Kind, v.MemoryCommand[i].Text = kind, cmd.Text
		} else if kind != v.MemoryCommand[i].Kind || cmd.Text != v.MemoryCommand[i].Text {
			t.Errorf("ParseCommand(%q) = %s %q, want %s %q", v.MemoryCommand[i].In, kind, cmd.Text, v.MemoryCommand[i].Kind, v.MemoryCommand[i].Text)
		}
	}

	if update {
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Clean(textVectorsPath), append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", textVectorsPath)
	}
}
