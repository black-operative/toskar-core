package benchmark_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/benchmark"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A benchmark unloads other models before loading each one, so it waits for
// a chat on this computer instead of evicting the chat's model (§60).
func TestBenchmarkWaitsForChatBeforeUnloading(t *testing.T) {
	gate := share.New(time.Millisecond)
	chat, err := gate.Enter(context.Background(), share.Interactive, "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	stopped := 0
	r := benchmark.NewRunner()
	r.Admit = func(ctx context.Context, waiting func(string)) (func(), error) {
		w, err := gate.Enter(ctx, share.Benchmark, "benchmark", waiting)
		return w.Done, err
	}
	r.ModelPath = func(ctx context.Context, modelID string) (string, error) { return "/tmp/" + modelID, nil }
	r.ListRunning = func(ctx context.Context) ([]pluginapi.RunningModel, error) {
		return []pluginapi.RunningModel{{ID: "chat-model", ModelID: "chat-model"}}, nil
	}
	r.StopModel = func(ctx context.Context, instanceID string) error {
		mu.Lock()
		stopped++
		mu.Unlock()
		return nil
	}
	r.StartModel = func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error) {
		return pluginapi.RunningModel{ID: "inst-" + modelID, ModelID: modelID, Endpoint: "http://127.0.0.1:9"}, nil
	}
	r.Chat = func(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
		ch := make(chan pluginapi.ChatChunk, 1)
		ch <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{CompletionTokens: 1, TotalMs: 1}}
		close(ch)
		return ch, nil
	}
	job, err := r.Start(context.Background(), contracts.BenchmarkRequest{ModelIDs: []string{"model-a"}, WorkloadIDs: []string{"chat"}, Runs: 1})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := r.Get(job.ID)
		if got.Progress.Phase == "waiting" {
			if got.Progress.Message != "Waiting for your chat to finish" {
				t.Fatalf("message = %q", got.Progress.Message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("benchmark did not wait: %+v", got.Progress)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if stopped != 0 {
		t.Fatal("benchmark unloaded the chat's model while the chat was running")
	}
	mu.Unlock()

	chat.Done()
	for {
		got, _ := r.Get(job.ID)
		if got.Status == contracts.BenchmarkCompleted {
			break
		}
		if got.Status == contracts.BenchmarkFailed || time.Now().After(deadline) {
			t.Fatalf("benchmark did not finish: %+v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
