package automations_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

var onChange = automations.Notification{Mode: automations.NotifyOnChange}

// "Notify on change" compares what changed, not the model's wording (#204).
func TestChangeModeComparesWhatChanged(t *testing.T) {
	prev := &automations.Previous{Text: "The listing is $420.\n{\"price\": 420}", SourceHash: "aaa"}
	for name, tc := range map[string]struct {
		cur    automations.Execution
		notify bool
		judge  bool
	}{
		"same values, new wording": {
			cur: automations.Execution{Text: "It still costs $420 today.\n{\"price\": 420}", SourceHash: "bbb"},
		},
		"different value": {
			cur:    automations.Execution{Text: "Now $399.\n{\"price\": 399}", SourceHash: "aaa"},
			notify: true,
		},
		"same sources, new wording": {
			cur: automations.Execution{Text: "Nothing new on the page.", SourceHash: "aaa"},
		},
		"the model sees no meaningful change": {
			cur:   automations.Execution{Text: "Three openings are listed.", SourceHash: "ccc", Change: &automations.Change{Changed: false}},
			judge: true,
		},
		"the model sees a change": {
			cur:    automations.Execution{Text: "Four openings are listed.", SourceHash: "ccc", Change: &automations.Change{Changed: true, What: "A fourth opening, for a designer, was added."}},
			notify: true,
			judge:  true,
		},
		"nothing else can tell": {
			cur:    automations.Execution{Text: "Four openings are listed."},
			notify: true,
			judge:  true,
		},
	} {
		decision := automations.DecideRun(onChange, tc.cur, prev)
		if decision.Notify != tc.notify {
			t.Errorf("%s: notify = %v (%s), want %v", name, decision.Notify, decision.Reason, tc.notify)
		}
		if got := automations.NeedsJudgment(onChange, *prev, tc.cur); got != tc.judge {
			t.Errorf("%s: needs judgment = %v, want %v", name, got, tc.judge)
		}
	}

	// The notice says what changed, as the model put it.
	changed := automations.DecideRun(onChange, automations.Execution{Text: "Four openings.", Change: &automations.Change{Changed: true, What: "A fourth opening was added."}}, prev)
	if changed.Notice.Body != "A fourth opening was added." {
		t.Fatalf("notice body = %q", changed.Notice.Body)
	}
	if first := automations.DecideRun(onChange, automations.Execution{Text: "x"}, nil); first.Notify {
		t.Fatal("the first result is only a baseline")
	}
}

// The in-stock condition asks for a flag, so it works in every language
// (#204).
func TestInStockAsksForAFlag(t *testing.T) {
	schema := automations.ConditionSchema(automations.Notification{
		Mode:      automations.NotifyOnCondition,
		Condition: &automations.Condition{Kind: automations.ConditionAvailable},
	})
	if schema == nil || schema.Properties["available"] == nil || schema.Properties["available"].Type != "boolean" {
		t.Fatalf("schema = %+v", schema)
	}
	in := automations.Notification{Mode: automations.NotifyOnCondition, Condition: &automations.Condition{Kind: automations.ConditionAvailable}}
	if d := automations.Decide(in, "Wieder vorrätig bei Costco.\n{\"available\": true}", nil, false); !d.Notify {
		t.Fatalf("a German in-stock result with its flag: %+v", d)
	}
}

// previousExec records the previous run it was given, and reports a source
// fingerprint.
type previousExec struct {
	got  []automations.Previous
	hash string
	text string
}

func (e *previousExec) Execute(ctx context.Context, _ automations.Automation) (automations.Execution, error) {
	if p, ok := automations.PreviousFrom(ctx); ok {
		e.got = append(e.got, p)
	}
	return automations.Execution{Text: e.text, SourceHash: e.hash}, nil
}

// The runner hands the executor the last successful run, with what its
// tools read, and a run that read the same doesn't notify (#204).
func TestRunnerComparesWithThePreviousRun(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	_, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model", Name: "Job board", Prompt: "List the openings",
		Notification: onChange,
		Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	exec := &previousExec{hash: "page-v1", text: "Three openings are listed."}
	notes := &recordingNotifier{}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: notes, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("the first run had a previous one: %+v", exec.got)
	}

	clock = clock.Add(24 * time.Hour)
	exec.text = "The board lists three openings."
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(exec.got) != 1 || exec.got[0].SourceHash != "page-v1" || exec.got[0].Text != "Three openings are listed." {
		t.Fatalf("previous = %+v", exec.got)
	}
	if notes.count() != 0 {
		t.Fatalf("notified on new wording of the same page: %+v", notes.notices())
	}

}
