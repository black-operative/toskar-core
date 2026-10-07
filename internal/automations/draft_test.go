package automations_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// An automation made from a chat keeps its conversation, and pressing
// Create on the same draft again returns it instead of a second one (#204).
func TestCreatingADraftTwiceMakesOne(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
	in := automations.CreateInput{
		ModelID: "auto", Name: "News", Prompt: "Summarize the news.", ProfileID: "general-assistant",
		Schedule:       automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		ConversationID: "conv-1", DraftID: "draft-1",
	}
	first, err := repo.Create(ctx, in, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.Create(ctx, in, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("second automation %s for the same draft", again.ID)
	}
	got, err := repo.Get(ctx, first.ID)
	if err != nil || got.ConversationID != "conv-1" || got.DraftID != "draft-1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	list, _ := repo.List(ctx)
	if len(list) != 1 {
		t.Fatalf("%d automations", len(list))
	}
}

// A run that notifies posts its result, without the JSON its condition
// asked for, to the chat the automation came from, and the notice opens
// that chat. A post that fails leaves the notice opening the automation.
func TestResultGoesToTheChatItCameFrom(t *testing.T) {
	for _, postFails := range []bool{false, true} {
		db := openAutomationDB(t)
		repo := repositories.NewAutomationRepo(db.SQL)
		ctx := context.Background()
		createdAt := time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)
		created, err := repo.Create(ctx, automations.CreateInput{
			ModelID: "auto", Name: "Price", Prompt: "Check the price.", ConversationID: "conv-1", DraftID: "d",
			Notification: priceBelow,
			Schedule:     automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		}, createdAt)
		if err != nil {
			t.Fatal(err)
		}
		var posted []string
		notify := &recordingNotifier{}
		clock := createdAt.Add(90 * time.Minute)
		runner := &automations.Runner{
			Store: repo, Exec: &scriptedExec{text: "It's €420 today.\n{\"price\": 420}"}, Notify: notify,
			Now: func() time.Time { return clock }, Lease: time.Hour,
			Post: func(_ context.Context, a automations.Automation, run automations.Run, text string) error {
				if postFails {
					return context.Canceled
				}
				if a.ID != created.ID || run.ID == "" {
					t.Errorf("posted for %s run %q", a.ID, run.ID)
				}
				posted = append(posted, text)
				return nil
			},
		}
		if err := runner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		if len(notify.items) != 1 {
			t.Fatalf("%d notices", len(notify.items))
		}
		if postFails {
			if notify.items[0].ConversationID != "" {
				t.Fatal("notice opens a chat the result never reached")
			}
			continue
		}
		if len(posted) != 1 || posted[0] != "It's €420 today." || notify.items[0].ConversationID != "conv-1" {
			t.Fatalf("posted %q, notice %+v", posted, notify.items[0])
		}
	}
}

// An automation with a save folder also saves each result as a file, and
// the run says where (#204).
func TestResultIsSavedToItsFolder(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(automations.SetHomeDir(home))
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "auto", Name: "Price", Prompt: "Check the price.", SaveFolder: "~/Toskar/Prices", Notification: priceBelow,
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: &scriptedExec{text: "It's €640 today.\n{\"price\": 640}"}, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.History(ctx, created.ID)
	saved := detail.History[0].SavedFile
	if !strings.HasPrefix(saved, filepath.Join(home, "Toskar", "Prices")+string(filepath.Separator)) {
		t.Fatalf("saved file = %q", saved)
	}
	body, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(body), "It's €640 today.") || strings.Contains(string(body), `"price"`) {
		t.Fatalf("body = %q, %v", body, err)
	}
	// Outside the home folder is refused when saved.
	if _, err := repo.Update(ctx, created.ID, automations.Patch{SaveFolder: ptr("/etc")}, clock); err == nil {
		t.Fatal("saved a folder outside the home folder")
	}
}

func ptr[T any](v T) *T { return &v }

type scriptedWatcher struct {
	found []automations.Found
	errs  []error
	calls int
}

func (w *scriptedWatcher) Check(_ context.Context, _ automations.Trigger, _ []byte) (automations.Found, []byte, error) {
	i := w.calls
	w.calls++
	var err error
	if i < len(w.errs) {
		err = w.errs[i]
	}
	if err != nil {
		return automations.Found{}, nil, err
	}
	return w.found[i], []byte(`{"n":` + string(rune('0'+i)) + `}`), nil
}

type noteExec struct{ notes []string }

func (e *noteExec) Execute(ctx context.Context, _ automations.Automation) (automations.Execution, error) {
	e.notes = append(e.notes, automations.ChangeNote(ctx))
	return automations.Execution{Text: "Summarized the change."}, nil
}

// A trigger's check that finds nothing new needs no run; one that finds a
// change runs, told what changed; one that can't look fails the run (#204).
func TestTriggerChecksBeforeRunning(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "auto", Name: "Careers page", Prompt: "Say what's new.",
		Trigger:  &automations.Trigger{Kind: automations.TriggerPage, URL: "https://example.com/careers"},
		Schedule: automations.Schedule{Kind: automations.KindInterval, TimeZone: "UTC", EverySeconds: 3600},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	watch := &scriptedWatcher{
		found: []automations.Found{{}, {Changed: true, Summary: "+ Product manager"}, {}},
		errs:  []error{nil, nil, errors.New("503 Service Unavailable")},
	}
	exec := &noteExec{}
	clock := createdAt
	runner := &automations.Runner{Store: repo, Exec: exec, Watch: watch, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	tick := func() {
		clock = clock.Add(time.Hour)
		if err := runner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}

	tick() // nothing new
	detail, _ := repo.History(ctx, created.ID)
	if len(detail.History) != 0 || detail.LastCheckedAt == nil || detail.NextRunAt == nil || !detail.NextRunAt.After(clock) {
		t.Fatalf("after a quiet check: %d runs, checked %v, next %v", len(detail.History), detail.LastCheckedAt, detail.NextRunAt)
	}
	tick() // changed
	detail, _ = repo.History(ctx, created.ID)
	if len(detail.History) != 1 || detail.History[0].Status != automations.RunSucceeded || len(exec.notes) != 1 || !strings.Contains(exec.notes[0], "+ Product manager") {
		t.Fatalf("after a change: %+v, notes %q", detail.History, exec.notes)
	}
	if got, _ := repo.Get(ctx, created.ID); string(got.WatchState) != `{"n":1}` {
		t.Fatalf("watch state = %s", got.WatchState)
	}
	tick() // can't look: a failed run, without the model
	detail, _ = repo.History(ctx, created.ID)
	if len(detail.History) != 2 || len(exec.notes) != 1 {
		t.Fatalf("after a failed check: %d runs, %d model runs", len(detail.History), len(exec.notes))
	}
	if run := detail.History[0]; run.Status == automations.RunSucceeded || !strings.Contains(run.Error, "could not check https://example.com/careers") {
		t.Fatalf("failed check run = %+v", run)
	}
}

type chainExec struct {
	mu    sync.Mutex
	order []string
	notes map[string]string
}

func (e *chainExec) Execute(ctx context.Context, a automations.Automation) (automations.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.order = append(e.order, a.Name)
	if e.notes == nil {
		e.notes = map[string]string{}
	}
	e.notes[a.Name] = automations.ChangeNote(ctx)
	return automations.Execution{Text: "Result of " + a.Name + "."}, nil
}

// One automation finishing starts those that follow it, with its result;
// one that follows only notices waits for a notice; a chain can't loop and
// stops after MaxChain (#204).
func TestAutomationsRunAfterOthers(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)
	add := func(name string, after *automations.Automation, when string) automations.Automation {
		in := automations.CreateInput{ModelID: "auto", Name: name, Prompt: "Do " + name + ".",
			Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8}}
		if after != nil {
			in.Schedule = automations.Schedule{Kind: automations.KindManual, TimeZone: "UTC"}
			in.Trigger = &automations.Trigger{Kind: automations.TriggerAfter, AutomationID: after.ID, When: when}
		}
		created, err := repo.Create(ctx, in, createdAt)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	research := add("Research", nil, "")
	draft := add("Draft", &research, "")
	review := add("Review", &draft, automations.AfterNotified)

	exec := &chainExec{}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if strings.Join(exec.order, ",") != "Research,Draft,Review" {
		t.Fatalf("order = %v", exec.order)
	}
	if note := exec.notes["Draft"]; !strings.Contains(note, `follows the automation "Research"`) || !strings.Contains(note, "Result of Research.") || !strings.Contains(note, "not instructions") {
		t.Fatalf("draft note = %q", note)
	}

	// Review follows only notices: Draft not notifying leaves it be.
	off := automations.Notification{Mode: automations.NotifyNone}
	if _, err := repo.Update(ctx, draft.ID, automations.Patch{Notification: &off}, clock); err != nil {
		t.Fatal(err)
	}
	exec.order = nil
	clock = clock.Add(time.Minute)
	if _, err := runner.RunNow(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if strings.Join(exec.order, ",") != "Draft" {
		t.Fatalf("after a quiet draft: %v", exec.order)
	}

	// No loops, not even through others, and no following a missing one.
	loop := &automations.Trigger{Kind: automations.TriggerAfter, AutomationID: review.ID}
	if _, err := repo.Update(ctx, research.ID, automations.Patch{Trigger: loop}, clock); err == nil {
		t.Fatal("made a loop")
	}
	self := &automations.Trigger{Kind: automations.TriggerAfter, AutomationID: research.ID}
	if _, err := repo.Update(ctx, research.ID, automations.Patch{Trigger: self}, clock); err == nil {
		t.Fatal("followed itself")
	}
	if _, err := repo.Create(ctx, automations.CreateInput{ModelID: "auto", Name: "Orphan", Prompt: "x",
		Trigger:  &automations.Trigger{Kind: automations.TriggerAfter, AutomationID: "gone"},
		Schedule: automations.Schedule{Kind: automations.KindManual, TimeZone: "UTC"}}, clock); err == nil {
		t.Fatal("followed a missing automation")
	}
}

func TestChainStopsAfterMax(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 10, 7, 7, 0, 0, 0, time.UTC)
	prev, err := repo.Create(ctx, automations.CreateInput{ModelID: "auto", Name: "Step 0", Prompt: "x",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8}}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= automations.MaxChain+2; i++ {
		prev, err = repo.Create(ctx, automations.CreateInput{ModelID: "auto", Name: "Step " + string(rune('0'+i)), Prompt: "x",
			Trigger:  &automations.Trigger{Kind: automations.TriggerAfter, AutomationID: prev.ID},
			Schedule: automations.Schedule{Kind: automations.KindManual, TimeZone: "UTC"}}, createdAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := &chainExec{}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	runner.Wait()
	if len(exec.order) != automations.MaxChain+1 {
		t.Fatalf("ran %d steps: %v", len(exec.order), exec.order)
	}
}
