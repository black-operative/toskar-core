package automations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// These scenarios drive the stored schedule, the runner, and the notifier together.
// Each one uses the v1 contract: one weekday, edge-triggered availability, and failure
// notices that start on the second consecutive terminal failure.

func TestScenarioPriceThreshold(t *testing.T) {
	loc := zone(t, "America/Juneau")
	created := time.Date(2026, 9, 23, 9, 0, 0, 0, loc)
	scene := newScene(t, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Price below $500",
		Prompt:  "Check the product price.",
		Notification: automations.Notification{
			Mode: automations.NotifyOnCondition,
			Condition: &automations.Condition{
				Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500,
			},
		},
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: loc.String(), Hour: 8},
	}, created)

	scene.at(time.Date(2026, 9, 24, 7, 30, 0, 0, loc))
	scene.tick()
	if scene.exec.count() != 0 {
		t.Fatalf("ran before 8:00 AM, executions = %d", scene.exec.count())
	}

	scene.at(time.Date(2026, 9, 24, 8, 5, 0, 0, loc))
	scene.exec.texts = []string{`The listing is $640. {"price": 640}`}
	scene.tick()
	scene.tick()
	if scene.exec.count() != 1 || scene.notes.count() != 0 {
		t.Fatalf("640 exec=%d notices=%d", scene.exec.count(), scene.notes.count())
	}
	high := scene.runAt(time.Date(2026, 9, 24, 8, 0, 0, 0, loc))
	if high.Status != automations.RunSucceeded || high.NotificationSent || !strings.Contains(high.Result, "640") {
		t.Fatalf("high run = %+v", high)
	}
	scene.expectNext(time.Date(2026, 9, 25, 8, 0, 0, 0, loc))

	scene.at(time.Date(2026, 9, 25, 8, 5, 0, 0, loc))
	scene.exec.texts = []string{`The listing is $420. {"price": 420}`}
	scene.tick()
	if scene.exec.count() != 2 || scene.notes.count() != 1 {
		t.Fatalf("420 exec=%d notices=%d", scene.exec.count(), scene.notes.count())
	}
	if scene.notes.last().Title != "Price below $500" || !strings.Contains(scene.notes.last().Body, "$420") {
		t.Fatalf("notice = %+v", scene.notes.last())
	}
	low := scene.runAt(time.Date(2026, 9, 25, 8, 0, 0, 0, loc))
	if low.Status != automations.RunSucceeded || !low.NotificationSent {
		t.Fatalf("low run = %+v", low)
	}
	kept := scene.runAt(time.Date(2026, 9, 24, 8, 0, 0, 0, loc))
	if kept.Result != high.Result || kept.NotificationSent {
		t.Fatalf("stored high-price run changed: %+v", kept)
	}
	scene.expectNext(time.Date(2026, 9, 26, 8, 0, 0, 0, loc))
}

func TestScenarioStockBecomesAvailable(t *testing.T) {
	created := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	scene := newScene(t, automations.CreateInput{
		ModelID: "model-a",
		Name:    "Switch stock",
		Prompt:  "Check whether the item is in stock.",
		Notification: automations.Notification{
			Mode:      automations.NotifyOnCondition,
			Condition: &automations.Condition{Kind: automations.ConditionAvailable},
		},
		Schedule: automations.Schedule{Kind: automations.KindInterval, TimeZone: "UTC", EverySeconds: 2 * 60 * 60},
	}, created)

	steps := []struct {
		result string
		notify bool
	}{
		{result: "The item is out of stock.", notify: false},
		{result: "The item is out of stock.", notify: false},
		{result: "The item is in stock.", notify: true},
		{result: "The item is in stock.", notify: false},
		{result: "The item is out of stock.", notify: false},
		{result: "The item is in stock.", notify: true},
	}
	for i, step := range steps {
		if i == 3 {
			scene.restart()
		}
		scene.exec.texts = []string{step.result}
		scene.at(created.Add(time.Duration(i+1) * 2 * time.Hour))
		before := scene.notes.count()
		scene.tick()
		scene.tick()
		if scene.exec.count() != i+1 {
			t.Fatalf("run %d executions = %d", i+1, scene.exec.count())
		}
		if step.notify {
			if scene.notes.count() != before+1 || !strings.Contains(scene.notes.last().Body, "in stock") {
				t.Fatalf("run %d notices = %+v", i+1, scene.notes.notices())
			}
		} else if scene.notes.count() != before {
			t.Fatalf("run %d sent an unexpected notice %+v", i+1, scene.notes.last())
		}
		run := scene.runAt(scene.clock)
		if run.Status != automations.RunSucceeded || run.NotificationSent != step.notify || run.Result != step.result {
			t.Fatalf("run %d = %+v", i+1, run)
		}
	}
	if scene.notes.count() != 2 {
		t.Fatalf("notices = %+v", scene.notes.notices())
	}
	scene.expectNext(created.Add(7 * 2 * time.Hour))
}

func TestScenarioWeeklyReportAlwaysNotifies(t *testing.T) {
	loc := zone(t, "America/Juneau")
	friday := int(time.Friday)
	created := time.Date(2026, 9, 24, 10, 0, 0, 0, loc)
	scene := newScene(t, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Friday releases",
		Prompt:       "Summarize today's releases.",
		Notification: automations.Notification{Mode: automations.NotifyAlways},
		Schedule: automations.Schedule{
			Kind: automations.KindWeekly, TimeZone: loc.String(), Hour: 17, Minute: 0, Weekday: &friday,
		},
	}, created)

	scene.at(time.Date(2026, 9, 25, 16, 0, 0, 0, loc))
	scene.tick()
	if scene.exec.count() != 0 {
		t.Fatalf("ran before Friday 5:00 PM, executions = %d", scene.exec.count())
	}

	scene.at(time.Date(2026, 9, 25, 17, 5, 0, 0, loc))
	scene.exec.texts = []string{"No releases today."}
	scene.tick()
	if scene.exec.count() != 1 || scene.notes.count() != 1 || scene.notes.last().Body != "No releases today." {
		t.Fatalf("friday exec=%d notices=%+v", scene.exec.count(), scene.notes.notices())
	}
	scene.expectNext(time.Date(2026, 10, 2, 17, 0, 0, 0, loc))

	for _, day := range []int{26, 27, 28} {
		scene.at(time.Date(2026, 9, day, 17, 5, 0, 0, loc))
		scene.tick()
		if scene.exec.count() != 1 {
			t.Fatalf("Sep %d ran a non-Friday occurrence, executions = %d", day, scene.exec.count())
		}
	}

	scene.restart()
	scene.at(time.Date(2026, 10, 2, 17, 5, 0, 0, loc))
	scene.exec.texts = []string{"Released today: scheduler scenarios."}
	scene.tick()
	if scene.exec.count() != 2 || scene.notes.count() != 2 || !strings.Contains(scene.notes.last().Body, "scheduler scenarios") {
		t.Fatalf("next friday exec=%d notices=%+v", scene.exec.count(), scene.notes.notices())
	}
	scene.expectNext(time.Date(2026, 10, 9, 17, 0, 0, 0, loc))
}

func TestScenarioOneTimeRunsOnceAfterAMiss(t *testing.T) {
	loc := zone(t, "America/Juneau")
	at := time.Date(2026, 9, 29, 14, 15, 0, 0, loc)
	created := time.Date(2026, 9, 28, 9, 0, 0, 0, loc)
	scene := newScene(t, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Store reminder",
		Prompt:       "Remind me to verify screenshots and age rating.",
		Notification: automations.Notification{Mode: automations.NotifyAlways},
		Schedule:     automations.Schedule{Kind: automations.KindOnce, TimeZone: loc.String(), At: &at},
	}, created)

	scene.at(time.Date(2026, 9, 29, 14, 10, 0, 0, loc))
	scene.tick()
	if scene.exec.count() != 0 {
		t.Fatalf("ran before 2:15 PM, executions = %d", scene.exec.count())
	}

	scene.at(time.Date(2026, 9, 29, 14, 25, 0, 0, loc))
	scene.exec.text = "Reminder: verify screenshots and age rating."
	scene.tick()
	scene.tick()
	if scene.exec.count() != 1 || scene.notes.count() != 1 {
		t.Fatalf("missed run exec=%d notices=%d", scene.exec.count(), scene.notes.count())
	}
	if !strings.Contains(scene.notes.last().Body, "verify screenshots and age rating") {
		t.Fatalf("notice = %+v", scene.notes.last())
	}
	run := scene.runAt(at)
	if run.Status != automations.RunSucceeded || !run.NotificationSent || run.OccurrenceAt.UTC().Truncate(time.Second) != at.UTC().Truncate(time.Second) {
		t.Fatalf("run = %+v", run)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(scene.clock.UTC().Truncate(time.Second)) {
		t.Fatalf("finished = %v, want the late clock %s", run.FinishedAt, scene.clock)
	}
	loaded, err := scene.repo.Get(scene.ctx, scene.id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NextRunAt != nil {
		t.Fatalf("one-time next = %v", loaded.NextRunAt)
	}

	scene.restart()
	scene.tick()
	if scene.exec.count() != 1 || scene.notes.count() != 1 {
		t.Fatalf("restart reran exec=%d notices=%d", scene.exec.count(), scene.notes.count())
	}
}

func TestScenarioFailuresAdvanceAndASuccessClearsThem(t *testing.T) {
	created := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	failed := errExec(`tool "internet.open" could not load the page`)
	scene := newScene(t, automations.CreateInput{
		ModelID:      "model-a",
		Name:         "Ready check",
		Prompt:       "Tell me whether the page contains Yggdrasil Ready.",
		Notification: automations.Notification{Mode: automations.NotifyNone},
		Schedule:     automations.Schedule{Kind: automations.KindInterval, TimeZone: "UTC", EverySeconds: 60 * 60},
	}, created)
	scene.exec.errs = []error{failed, failed, failed, nil, nil, failed}
	scene.exec.texts = []string{
		"The page does not contain Yggdrasil Ready.",
		"The page contains Yggdrasil Ready.",
	}

	scene.at(created.Add(time.Hour))
	scene.tick()
	scene.expectQuietFailure(1, created.Add(2*time.Hour))
	scene.tick()
	if scene.exec.count() != 1 {
		t.Fatalf("same occurrence ran again, executions = %d", scene.exec.count())
	}

	scene.at(created.Add(2 * time.Hour))
	scene.tick()
	scene.expectNotifiedFailure(2, created.Add(3*time.Hour))

	scene.at(created.Add(3 * time.Hour))
	scene.tick()
	scene.expectNotifiedFailure(3, created.Add(4*time.Hour))

	scene.at(created.Add(4 * time.Hour))
	scene.tick()
	scene.expectRecovery(created.Add(5*time.Hour), "does not contain")

	scene.at(created.Add(5 * time.Hour))
	scene.tick()
	scene.expectRecovery(created.Add(6*time.Hour), "contains Yggdrasil Ready")

	scene.at(created.Add(6 * time.Hour))
	scene.tick()
	scene.expectQuietFailure(1, created.Add(7*time.Hour))
	if scene.notes.count() != 2 {
		t.Fatalf("notices = %+v", scene.notes.notices())
	}
	history, err := scene.repo.ListRuns(scene.ctx, scene.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 6 {
		t.Fatalf("history len = %d", len(history))
	}
}

type scene struct {
	t      *testing.T
	ctx    context.Context
	repo   *repositories.AutomationRepo
	exec   *scriptedExec
	notes  *recordingNotifier
	clock  time.Time
	runner *automations.Runner
	id     string
}

func newScene(t *testing.T, input automations.CreateInput, created time.Time) *scene {
	t.Helper()
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdItem, err := repo.Create(ctx, input, created)
	if err != nil {
		t.Fatal(err)
	}
	out := &scene{
		t: t, ctx: ctx, repo: repo, exec: &scriptedExec{modelID: "model-a"}, notes: &recordingNotifier{},
		clock: created, id: createdItem.ID,
	}
	out.runner = out.newRunner()
	return out
}

func (s *scene) newRunner() *automations.Runner {
	return &automations.Runner{
		Store:  s.repo,
		Exec:   s.exec,
		Notify: s.notes,
		Now:    func() time.Time { return s.clock },
		Lease:  time.Hour,
	}
}

func (s *scene) restart() {
	s.runner = s.newRunner()
}

func (s *scene) at(when time.Time) {
	s.clock = when.UTC().Truncate(time.Second)
}

func (s *scene) tick() {
	s.t.Helper()
	if err := s.runner.Tick(s.ctx); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scene) runAt(when time.Time) automations.Run {
	s.t.Helper()
	run, err := s.repo.RunFor(s.ctx, s.id, when.UTC().Truncate(time.Second))
	if err != nil {
		s.t.Fatal(err)
	}
	return run
}

func (s *scene) expectNext(want time.Time) {
	s.t.Helper()
	loaded, err := s.repo.Get(s.ctx, s.id)
	if err != nil {
		s.t.Fatal(err)
	}
	want = want.UTC().Truncate(time.Second)
	if loaded.NextRunAt == nil || !loaded.NextRunAt.Equal(want) {
		s.t.Fatalf("next = %v, want %s", loaded.NextRunAt, want.Format(time.RFC3339))
	}
}

func (s *scene) expectQuietFailure(failures int, next time.Time) {
	s.t.Helper()
	loaded, err := s.repo.Get(s.ctx, s.id)
	if err != nil {
		s.t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != failures {
		s.t.Fatalf("consecutive failures = %d, want %d", loaded.ConsecutiveFailures, failures)
	}
	run := s.runAt(s.clock)
	if run.Status != automations.RunFailed || run.NotificationSent {
		s.t.Fatalf("quiet failure = %+v", run)
	}
	s.expectNext(next)
}

func (s *scene) expectNotifiedFailure(failures int, next time.Time) {
	s.t.Helper()
	loaded, err := s.repo.Get(s.ctx, s.id)
	if err != nil {
		s.t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != failures {
		s.t.Fatalf("consecutive failures = %d, want %d", loaded.ConsecutiveFailures, failures)
	}
	run := s.runAt(s.clock)
	if run.Status != automations.RunFailed || !run.NotificationSent {
		s.t.Fatalf("notified failure = %+v", run)
	}
	if !strings.Contains(s.notes.last().Body, "failures in a row") {
		s.t.Fatalf("notice = %+v", s.notes.last())
	}
	if s.notes.count() != failures-1 {
		s.t.Fatalf("notice count = %d, want %d", s.notes.count(), failures-1)
	}
	s.expectNext(next)
}

func (s *scene) expectRecovery(next time.Time, result string) {
	s.t.Helper()
	loaded, err := s.repo.Get(s.ctx, s.id)
	if err != nil {
		s.t.Fatal(err)
	}
	if loaded.ConsecutiveFailures != 0 || loaded.LastError != "" {
		s.t.Fatalf("after success failures=%d error=%q", loaded.ConsecutiveFailures, loaded.LastError)
	}
	run := s.runAt(s.clock)
	if run.Status != automations.RunSucceeded || run.NotificationSent || !strings.Contains(run.Result, result) {
		s.t.Fatalf("success = %+v", run)
	}
	s.expectNext(next)
}

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
