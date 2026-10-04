package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// runningRuntime reports a fixed list of running models.
type runningRuntime struct {
	pluginapi.Runtime
	running []pluginapi.RunningModel
}

func (r *runningRuntime) ID() string { return "llamacpp" }
func (r *runningRuntime) ListRunning(context.Context) ([]pluginapi.RunningModel, error) {
	return r.running, nil
}

func tokenApp(running ...pluginapi.RunningModel) (*App, *[]string) {
	reg := runtimes.NewRegistry()
	reg.Register(&runningRuntime{running: running})
	var calls []string
	a := &App{
		Runtimes:    runtimes.NewManager(reg, nil, nil, nil),
		tokenCounts: cache.New[int](tokenCountPolicy),
		tokenize: func(_ context.Context, endpoint, text string) (int, error) {
			calls = append(calls, endpoint)
			if endpoint == "http://broken" {
				return 0, errors.New("down")
			}
			return len(strings.Fields(text)), nil
		},
	}
	return a, &calls
}

func TestTokenCounterUsesTheRunningModel(t *testing.T) {
	a, calls := tokenApp(
		pluginapi.RunningModel{ModelID: "llama", Endpoint: "http://embed", Status: "running", Mode: pluginapi.ModeEmbedding},
		pluginapi.RunningModel{ModelID: "llama", Endpoint: "http://chat", Status: "running"},
	)
	count := a.tokenCounter(context.Background(), "llama")
	if n, exact := count("four words right here"); n != 4 || !exact {
		t.Fatalf("n=%d exact=%v", n, exact)
	}
	// The same text is counted once.
	if n, exact := a.tokenCounter(context.Background(), "llama")("four words right here"); n != 4 || !exact {
		t.Fatalf("cached n=%d exact=%v", n, exact)
	}
	if len(*calls) != 1 || (*calls)[0] != "http://chat" {
		t.Fatalf("calls %v", *calls)
	}
}

func TestTokenCounterEstimatesWhenTheModelIsNotRunning(t *testing.T) {
	a, calls := tokenApp(pluginapi.RunningModel{ModelID: "other", Endpoint: "http://other", Status: "running"})
	n, exact := a.tokenCounter(context.Background(), "llama")("12345678")
	if n != 2 || exact || len(*calls) != 0 {
		t.Fatalf("n=%d exact=%v calls=%v", n, exact, *calls)
	}
}

func TestTokenCounterStopsAskingAfterAFailure(t *testing.T) {
	a, calls := tokenApp(pluginapi.RunningModel{ModelID: "llama", Endpoint: "http://broken", Status: "running"})
	count := a.tokenCounter(context.Background(), "llama")
	for _, text := range []string{"12345678", "abcdefgh"} {
		if n, exact := count(text); n != 2 || exact {
			t.Fatalf("n=%d exact=%v", n, exact)
		}
	}
	if len(*calls) != 1 {
		t.Fatalf("asked %d times", len(*calls))
	}
}

func TestStubInferenceEstimates(t *testing.T) {
	a, _ := tokenApp()
	a.stubInference = true
	if a.tokenCounter(context.Background(), "llama") != nil {
		t.Fatal("stub inference has no tokenizer")
	}
}

// The gauge's limit is the window the running llama-server reports, read
// once per server; a model that isn't running here falls back (#230).
func TestLocalContextReadsTheRunningWindow(t *testing.T) {
	a, _ := tokenApp(pluginapi.RunningModel{ModelID: "llama", Endpoint: "http://chat", Status: "running"})
	reads := 0
	a.window = func(_ context.Context, endpoint string) (int, error) {
		reads++
		if endpoint != "http://chat" {
			t.Fatalf("endpoint %q", endpoint)
		}
		return 16384, nil
	}
	for range 2 {
		window, memory, ok := a.localContext(context.Background(), "llama")
		if !ok || window != 16384 || memory != 0 {
			t.Fatalf("window=%d memory=%d ok=%v", window, memory, ok)
		}
	}
	if reads != 1 {
		t.Fatalf("read the window %d times, want once", reads)
	}
	if _, _, ok := a.localContext(context.Background(), "qwen"); ok {
		t.Fatal("a model not running here has no local window")
	}
	env := &chatExecEnv{app: a, ctx: context.Background(), modelOverride: "llama"}
	if got := env.ContextLimit(); got != 16384 {
		t.Fatalf("ContextLimit = %d, want the running window", got)
	}
}
