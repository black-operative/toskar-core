package tools

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
)

func TestImplausibleReadDoesNotPrompt(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"filesystem.read": stubTool{id: "filesystem.read"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	_, err := r.Execute(context.Background(), "filesystem.read", map[string]any{
		"path": "weather data for Juneau AK",
	}, PolicyAsk, "chat requested tool", nil)
	if err == nil {
		t.Fatal("expected rejection")
	}

	select {
	case evt := <-ch:
		if evt.Type == events.ToolRequested {
			t.Fatal("nonsense path opened the approval dialog")
		}
	case <-time.After(40 * time.Millisecond):
	}
}

func TestRealReadStillPrompts(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"filesystem.read": stubTool{id: "filesystem.read"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	errCh := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), "filesystem.read", map[string]any{"path": "README.md"}, PolicyAsk, "test", nil)
		errCh <- err
	}()

	var requestID string
	deadline := time.After(2 * time.Second)
	for requestID == "" {
		select {
		case evt := <-ch:
			if evt.Type == events.ToolRequested {
				requestID, _ = evt.Payload["request_id"].(string)
			}
		case <-deadline:
			t.Fatal("real file read did not ask for approval")
		}
	}
	if err := r.Decide(requestID, false, false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected denial")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestPlausibleArguments(t *testing.T) {
	real := []struct {
		id   string
		args map[string]any
	}{
		{"filesystem.read", map[string]any{"path": "README.md"}},
		{"filesystem.read", map[string]any{"path": "docs/macos-signing.md"}},
		{"filesystem.read", map[string]any{"path": "My Notes.txt"}},
		{"filesystem.write", map[string]any{"path": "src/main.go"}},
		{"terminal", map[string]any{"command": "echo hi"}},
		{"terminal", map[string]any{"command": "curl -s https://example.com | grep weather"}},
		{"git.status", nil},
		{"git.commit", map[string]any{"message": "Save the weather notes"}},
	}
	for _, tc := range real {
		if err := implausibleCall(tc.id, tc.args); err != nil {
			t.Errorf("%s %v: %v", tc.id, tc.args, err)
		}
	}

	fake := []struct {
		id   string
		args map[string]any
	}{
		{"filesystem.read", map[string]any{"path": "weather data for Juneau AK"}},
		{"filesystem.write", map[string]any{"path": "what is the weather"}},
		{"terminal", map[string]any{"command": "what is the current weather in Juneau AK"}},
		{"terminal", map[string]any{"command": "weather data for Juneau AK"}},
	}
	for _, tc := range fake {
		if err := implausibleCall(tc.id, tc.args); err == nil {
			t.Errorf("%s %v was treated as a real request", tc.id, tc.args)
		}
	}
}
