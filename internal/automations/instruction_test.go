package automations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

var priceBelow = automations.Notification{Mode: automations.NotifyOnCondition, Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500, Currency: "EUR"}}

// The server adds a condition's instruction when a run starts, and a saved
// prompt is only the task, even one an older page wrote the instruction
// into (#204).
func TestRunPromptAddsTheInstruction(t *testing.T) {
	a := automations.Automation{Prompt: "Prüfe dieses Produkt.", Notification: priceBelow}
	run := automations.RunPrompt(a)
	if !strings.HasPrefix(run, "Prüfe dieses Produkt.\n\n") || !strings.Contains(run, "numeric price in EUR") {
		t.Fatalf("run prompt = %q", run)
	}
	old := "Check this product.\n\nInclude a JSON object in the result with the numeric price in USD, for example {\"price\": 420}."
	if got := automations.TaskPrompt(old); got != "Check this product." {
		t.Fatalf("task = %q", got)
	}
	// A currency change gives one instruction, in the new currency.
	again := automations.RunPrompt(automations.Automation{Prompt: old, Notification: priceBelow})
	if strings.Count(again, "numeric price") != 1 || !strings.Contains(again, "in EUR") {
		t.Fatalf("run prompt = %q", again)
	}
	if plain := automations.RunPrompt(automations.Automation{Prompt: "Summarize the news.", Notification: automations.Notification{Mode: automations.NotifyAlways}}); plain != "Summarize the news." {
		t.Fatalf("plain = %q", plain)
	}
	stock := automations.Notification{Mode: automations.NotifyOnCondition, Condition: &automations.Condition{Kind: automations.ConditionAvailable}}
	if !strings.Contains(automations.SignalInstruction(stock), `{"available": true}`) {
		t.Fatal("no availability instruction")
	}
}

func TestSavedPromptIsOnlyTheTask(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "m", Name: "Price", Notification: priceBelow,
		Prompt:   "Check this product.\n\nInclude a JSON object in the result with the numeric price in EUR, for example {\"price\": 420}.",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if created.Prompt != "Check this product." {
		t.Fatalf("saved prompt = %q", created.Prompt)
	}
}

// A run records why it did or didn't notify, as the key the apps show
// (#204).
func TestRunRecordsItsDecision(t *testing.T) {
	db := openAutomationDB(t)
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	createdAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	created, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "m", Name: "Price", Prompt: "Check the price.", Notification: priceBelow,
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptedExec{text: "Es kostet 640 €.\n{\"price\": 640}"}
	clock := createdAt.Add(90 * time.Minute)
	runner := &automations.Runner{Store: repo, Exec: exec, Notify: &recordingNotifier{}, Now: func() time.Time { return clock }, Lease: time.Hour}
	if err := runner.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.History(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := detail.History[0]
	if run.NotifyDetail != "notBelow" || run.NotifyValues["price"] != 640.0 || run.NotifyValues["amount"] != 500.0 || run.NotifyValues["currency"] != "EUR" {
		t.Fatalf("run decision = %q %v", run.NotifyDetail, run.NotifyValues)
	}
}

func TestDecisionDetails(t *testing.T) {
	stock := automations.Notification{Mode: automations.NotifyOnCondition, Condition: &automations.Condition{Kind: automations.ConditionAvailable}}
	for name, tc := range map[string]struct {
		d         automations.Decision
		detail    string
		notMet    bool
		notifying bool
	}{
		"stored only":   {automations.Decide(automations.Notification{Mode: automations.NotifyNone}, "x", nil, false), "storesResult", false, false},
		"no price":      {automations.Decide(priceBelow, "No price today.", nil, false), "noPrice", false, false},
		"price matched": {automations.Decide(priceBelow, "{\"price\": 420}", nil, false), "priceBelow", false, true},
		"price missed":  {automations.Decide(priceBelow, "{\"price\": 640}", nil, false), "notBelow", true, false},
		"out of stock":  {automations.Decide(stock, "{\"available\": false}", nil, false), "notAvailable", true, false},
		"in stock":      {automations.Decide(stock, "{\"available\": true}", nil, false), "inStock", false, true},
	} {
		if tc.d.Detail != tc.detail || tc.d.ConditionNotMet != tc.notMet || tc.d.Notify != tc.notifying {
			t.Errorf("%s: %q notMet=%v notify=%v", name, tc.d.Detail, tc.d.ConditionNotMet, tc.d.Notify)
		}
	}
}
