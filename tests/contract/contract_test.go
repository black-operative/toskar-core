// Package contract guards the client contract (spec §68). Clients such as
// the desktop and mobile apps read these types; a field may be added in a
// minor version, but never removed or renamed within a major version.
//
// When this test says a field was added: bump the minor version in
// pkg/contracts/contract.go, then run
//
//	UPDATE_CONTRACT=1 go test ./tests/contract
//
// to record it.
package contract

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const snapshotFile = "testdata/contract.json"

// covered are the types clients read: events, run traces, answers and
// their citations, steps, and files, artifacts, notifications, what left
// the computer, and errors.
var covered = map[string]any{
	"Event":           events.Event{},
	"Run":             runlog.Run{},
	"ModelUse":        runlog.ModelUse{},
	"ToolUse":         runlog.ToolUse{},
	"Message":         contracts.Message{},
	"MessageMeta":     contracts.MessageMeta{},
	"Citation":        contracts.Citation{},
	"ActivityStep":    contracts.ActivityStep{},
	"FileRef":         contracts.FileRef{},
	"SetupOffer":      contracts.SetupOffer{},
	"AutomationDraft": contracts.AutomationDraft{},
	"ContextUsage":    contracts.ContextUsage{},
	"Conversation":    contracts.Conversation{},
	"Artifact":        artifacts.Artifact{},
	"Notification":    gjallarhorn.Notification{},
	"EgressRecord":    egress.Record{},
	"ContractInfo":    contracts.ContractInfo{},
	"VersionResponse": contracts.VersionResponse{},
	"ErrorBody":       contracts.ErrorBody{},
}

type snapshot struct {
	Version string              `json:"version"`
	Types   map[string][]string `json:"types"`
}

// fields lists a type's JSON field names, including those of embedded
// structs, as clients see them.
func fields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			out = append(out, fields(f.Type)...)
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func current() map[string][]string {
	out := map[string][]string{}
	for name, v := range covered {
		out[name] = fields(reflect.TypeOf(v))
	}
	return out
}

func TestClientContractIsCompatible(t *testing.T) {
	raw, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	var snap snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	now := current()

	// Removing or renaming a field breaks clients; only a major version may.
	snapMajor, _ := contracts.ContractMajor(snap.Version)
	nowMajor, _ := contracts.ContractMajor(contracts.ContractVersion)
	for name, want := range snap.Types {
		got, ok := now[name]
		if !ok && snapMajor == nowMajor {
			t.Errorf("%s left the contract; that needs a new major version", name)
			continue
		}
		for _, f := range want {
			if !slices.Contains(got, f) && snapMajor == nowMajor {
				t.Errorf("%s.%s was removed or renamed; clients read it, so that needs a new major version", name, f)
			}
		}
	}

	var added []string
	for name, got := range now {
		for _, f := range got {
			if !slices.Contains(snap.Types[name], f) {
				added = append(added, name+"."+f)
			}
		}
	}
	sort.Strings(added)
	if os.Getenv("UPDATE_CONTRACT") != "" {
		if len(added) > 0 && contracts.ContractVersion == snap.Version {
			t.Fatalf("fields were added (%s); bump the minor version in pkg/contracts/contract.go first", strings.Join(added, ", "))
		}
		out, _ := json.MarshalIndent(snapshot{Version: contracts.ContractVersion, Types: now}, "", "  ")
		if err := os.WriteFile(snapshotFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if len(added) > 0 {
		t.Errorf("fields were added to the client contract: %s. Bump the minor version in pkg/contracts/contract.go and run UPDATE_CONTRACT=1 go test ./tests/contract", strings.Join(added, ", "))
	}
	if snap.Version != contracts.ContractVersion {
		t.Errorf("the recorded contract is %s but the code says %s; run UPDATE_CONTRACT=1 go test ./tests/contract", snap.Version, contracts.ContractVersion)
	}
}
