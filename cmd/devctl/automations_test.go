package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestYggctlAutomations(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := repositories.NewAutomationRepo(db.SQL)
	exec := &cliExec{}
	runner := &automations.Runner{Store: repo, Exec: exec}
	now := time.Now()
	srv := api.NewServer(api.Dependencies{
		ListAutomations: repo.List,
		CreateAutomation: func(ctx context.Context, in automations.CreateInput) (automations.Automation, error) {
			return repo.Create(ctx, in, now)
		},
		GetAutomation: repo.History,
		UpdateAutomation: func(ctx context.Context, id string, patch automations.Patch) (automations.Automation, error) {
			return repo.Update(ctx, id, patch, now)
		},
		DeleteAutomation: repo.Delete,
		RunAutomation:    runner.RunNow,
		PauseAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := false
			return repo.Update(ctx, id, automations.Patch{Enabled: &enabled}, now)
		},
		ResumeAutomation: func(ctx context.Context, id string) (automations.Automation, error) {
			enabled := true
			return repo.Update(ctx, id, automations.Patch{Enabled: &enabled}, now)
		},
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	client := daemonClient{base: ts.URL, client: ts.Client()}

	var created bytes.Buffer
	err = runAutomations([]string{
		"create", "--name", "Morning price", "--prompt", "Check the price",
		"--profile", "general-assistant", "--model", "gemma-4-e4b", "--schedule", "daily", "--at", "08:00",
		"--zone", "UTC", "--notify", "none",
	}, client, &created)
	if err != nil {
		t.Fatal(err)
	}
	var automation automations.Automation
	if err := json.Unmarshal(created.Bytes(), &automation); err != nil {
		t.Fatal(err)
	}
	if automation.Name != "Morning price" || !automation.Enabled || automation.Schedule.Hour != 8 || automation.ModelID != "gemma-4-e4b" {
		t.Fatalf("created = %+v", automation)
	}

	var listed bytes.Buffer
	if err := runAutomations([]string{"list"}, client, &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), automation.ID) {
		t.Fatalf("list = %s", listed.String())
	}

	var updated bytes.Buffer
	if err := runAutomations([]string{"update", automation.ID, "--name", "Evening price"}, client, &updated); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.String(), "Evening price") {
		t.Fatalf("update = %s", updated.String())
	}

	var paused bytes.Buffer
	if err := runAutomations([]string{"pause", automation.ID}, client, &paused); err != nil {
		t.Fatal(err)
	}
	var pausedItem automations.Automation
	if err := json.Unmarshal(paused.Bytes(), &pausedItem); err != nil {
		t.Fatal(err)
	}
	if pausedItem.Enabled {
		t.Fatal("pause left the automation enabled")
	}

	var resumed bytes.Buffer
	if err := runAutomations([]string{"resume", automation.ID}, client, &resumed); err != nil {
		t.Fatal(err)
	}
	var resumedItem automations.Automation
	if err := json.Unmarshal(resumed.Bytes(), &resumedItem); err != nil {
		t.Fatal(err)
	}
	if !resumedItem.Enabled {
		t.Fatal("resume left the automation paused")
	}

	// run waits for the run the daemon started to finish.
	runPoll = 10 * time.Millisecond
	var ran bytes.Buffer
	if err := runAutomations([]string{"run", automation.ID}, client, &ran); err != nil {
		t.Fatal(err)
	}
	var run automations.Run
	if err := json.Unmarshal(ran.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != automations.RunSucceeded || run.Result != "price is 12" || exec.count() != 1 {
		t.Fatalf("run = %+v executions = %d", run, exec.count())
	}

	var got bytes.Buffer
	if err := runAutomations([]string{"get", automation.ID}, client, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), "price is 12") {
		t.Fatalf("get = %s", got.String())
	}

	var deleted bytes.Buffer
	if err := runAutomations([]string{"delete", automation.ID}, client, &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.String() != "deleted "+automation.ID+"\n" {
		t.Fatalf("delete = %q", deleted.String())
	}
	if err := runAutomations([]string{"get", automation.ID}, client, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing get = %v", err)
	}
	if err := runAutomations([]string{"update", automation.ID}, client, &bytes.Buffer{}); err == nil {
		t.Fatal("empty update succeeded")
	}
}

type cliExec struct {
	mu    sync.Mutex
	calls int
}

func (e *cliExec) Execute(context.Context, automations.Automation) (automations.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return automations.Execution{Text: "price is 12", ModelID: "model-a", NodeID: "node-a"}, nil
}

func (e *cliExec) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}
