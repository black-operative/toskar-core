package automations_test

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
)

type previewExec struct {
	text string
	err  error
}

func (e previewExec) Execute(context.Context, automations.Automation) (automations.Execution, error) {
	if e.err != nil {
		return automations.Execution{}, e.err
	}
	return automations.Execution{Text: e.text, ModelID: "gemma-4-e4b", NodeID: "local"}, nil
}

func TestPreviewReportsWhetherTheConditionMatches(t *testing.T) {
	runner := &automations.Runner{Exec: previewExec{text: "The listing is $420.\n{\"price\": 420}"}}
	preview, err := runner.Preview(context.Background(), automations.CreateInput{
		Name:     "Morning price",
		Prompt:   "Check the price",
		ModelID:  "gemma-4-e4b",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		Notification: automations.Notification{
			Mode:      automations.NotifyOnCondition,
			Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.WouldNotify || preview.Result == "" || preview.ModelID != "gemma-4-e4b" {
		t.Fatalf("preview = %+v", preview)
	}

	miss, err := (&automations.Runner{Exec: previewExec{text: "The listing is $640.\n{\"price\": 640}"}}).Preview(context.Background(), automations.CreateInput{
		Prompt:   "Check the price",
		ModelID:  "gemma-4-e4b",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		Notification: automations.Notification{
			Mode:      automations.NotifyOnCondition,
			Condition: &automations.Condition{Kind: automations.ConditionThreshold, Op: automations.OpBelow, Value: 500},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if miss.WouldNotify {
		t.Fatalf("preview notified on a miss: %+v", miss)
	}
}

func TestPreviewRequiresAModel(t *testing.T) {
	_, err := (&automations.Runner{Exec: previewExec{text: "hi"}}).Preview(context.Background(), automations.CreateInput{
		Prompt:   "Check the price",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	})
	if err == nil {
		t.Fatal("expected missing model to fail")
	}
}

type seenExec struct{ got automations.Automation }

func (e *seenExec) Execute(_ context.Context, a automations.Automation) (automations.Execution, error) {
	e.got = a
	return automations.Execution{Text: "Der Preis ist 420 $."}, nil
}

// A preview runs as the saved automation will: in its response language,
// with its schedule's time zone (#204).
func TestPreviewKeepsTheResponseLanguage(t *testing.T) {
	exec := &seenExec{}
	_, err := (&automations.Runner{Exec: exec}).Preview(context.Background(), automations.CreateInput{
		Prompt:           "Check the price",
		ModelID:          "gemma-4-e4b",
		ResponseLanguage: "de",
		Schedule:         automations.Schedule{Kind: automations.KindDaily, TimeZone: "Europe/Berlin", Hour: 8},
		Notification:     automations.Notification{Mode: automations.NotifyAlways},
	})
	if err != nil {
		t.Fatal(err)
	}
	if exec.got.ResponseLanguage != "de" || exec.got.Schedule.TimeZone != "Europe/Berlin" {
		t.Fatalf("preview ran %+v", exec.got)
	}
}
