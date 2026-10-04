package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestAutomationHTTP(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := repositories.NewAutomationRepo(db.SQL)
	exec := &countingExec{}
	runner := &automations.Runner{Store: repo, Exec: exec}
	now := time.Now()
	srv := NewServer(Dependencies{
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

	var created automations.Automation
	postJSON(t, ts.Client(), ts.URL+"/api/v1/automations", map[string]any{
		"name":   "Morning price",
		"prompt": "Check the price",
		"schedule": map[string]any{
			"kind": "daily", "time_zone": "UTC", "hour": 8,
		},
		"profile_id":   "general-assistant",
		"model_id":     "gemma-4-e4b",
		"notification": map[string]any{"mode": "none"},
	}, &created)
	if created.Name != "Morning price" || !created.Enabled || created.NextRunAt == nil || created.ModelID != "gemma-4-e4b" {
		t.Fatalf("created = %+v", created)
	}

	var listed []automations.Automation
	getJSON(t, ts.Client(), ts.URL+"/api/v1/automations", &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list = %+v", listed)
	}

	var detail automations.Detail
	getJSON(t, ts.Client(), ts.URL+"/api/v1/automations/"+created.ID, &detail)
	if detail.ID != created.ID || len(detail.History) != 0 {
		t.Fatalf("detail = %+v", detail)
	}

	var renamed automations.Automation
	sendJSON(t, ts.Client(), http.MethodPatch, ts.URL+"/api/v1/automations/"+created.ID, map[string]any{"name": "Evening price"}, &renamed)
	if renamed.Name != "Evening price" {
		t.Fatalf("renamed = %+v", renamed)
	}

	var paused automations.Automation
	sendJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/automations/"+created.ID+"/pause", nil, &paused)
	if paused.Enabled {
		t.Fatal("pause left the automation enabled")
	}
	var resumed automations.Automation
	sendJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/automations/"+created.ID+"/resume", nil, &resumed)
	if !resumed.Enabled {
		t.Fatal("resume left the automation paused")
	}

	var run automations.Run
	sendJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/automations/"+created.ID+"/run", nil, &run)
	if run.Status != automations.RunSucceeded || run.Result != "price is 12" {
		t.Fatalf("run = %+v", run)
	}
	if exec.count() != 1 {
		t.Fatalf("executions = %d", exec.count())
	}

	if status := doStatus(t, ts.Client(), http.MethodDelete, ts.URL+"/api/v1/automations/"+created.ID, nil); status != http.StatusNoContent {
		t.Fatalf("delete status = %d", status)
	}
	if status := doStatus(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/automations/"+created.ID, nil); status != http.StatusNotFound {
		t.Fatalf("missing status = %d", status)
	}
	if status := doStatus(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/automations", []byte(`{"name":"x"}`)); status != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d", status)
	}
}

type countingExec struct {
	mu    sync.Mutex
	calls int
}

func (e *countingExec) Execute(context.Context, automations.Automation) (automations.Execution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return automations.Execution{Text: "price is 12", ModelID: "model-a", NodeID: "node-a"}, nil
}

func (e *countingExec) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func getJSON(t *testing.T, client *http.Client, url string, dest any) {
	t.Helper()
	sendJSON(t, client, http.MethodGet, url, nil, dest)
}

func postJSON(t *testing.T, client *http.Client, url string, body, dest any) {
	t.Helper()
	sendJSON(t, client, http.MethodPost, url, body, dest)
}

func sendJSON(t *testing.T, client *http.Client, method, url string, body, dest any) {
	t.Helper()
	resp := doRequest(t, client, method, url, body)
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s status = %d body = %s", method, url, resp.StatusCode, data)
	}
	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			t.Fatal(err)
		}
	}
}

func doStatus(t *testing.T, client *http.Client, method, url string, body any) int {
	t.Helper()
	resp := doRequest(t, client, method, url, body)
	resp.Body.Close()
	return resp.StatusCode
}

func doRequest(t *testing.T, client *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var payload io.Reader
	switch v := body.(type) {
	case nil:
	case []byte:
		payload = bytes.NewReader(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
