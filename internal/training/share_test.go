package training

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/share"
)

// Training unloads models only once no chat is using this computer, and
// says what it is waiting for meanwhile (§60).
func TestTrainingWaitsForChatBeforeUnloading(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	gate := share.New(time.Millisecond)
	chat, err := gate.Enter(ctx, share.Interactive, "chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	var unloaded atomic.Int32
	var heldDuringUnload atomic.Bool
	h.svc.d.UnloadLocalModels = func(context.Context) int {
		unloaded.Add(1)
		_, ok := gate.Running(share.Training)
		heldDuringUnload.Store(ok)
		return 1
	}
	h.svc.d.Admit = func(ctx context.Context, name string, waiting func(string)) (Hold, error) {
		return gate.Enter(ctx, share.Training, name, waiting)
	}
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Tire Bot", BaseModelID: "small-q4"})
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(12)}); err != nil {
		t.Fatal(err)
	}
	job, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		j, _ := h.svc.Job(ctx, job.ID)
		if strings.HasPrefix(j.Progress.Detail, "Waiting for your chat to finish") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("training did not wait for chat: %+v", j.Progress)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if unloaded.Load() != 0 {
		t.Fatal("models were unloaded during a chat")
	}
	chat.Done()
	job = waitJob(t, h, job.ID)
	if unloaded.Load() != 1 || !heldDuringUnload.Load() {
		t.Fatalf("unloaded=%d held=%v", unloaded.Load(), heldDuringUnload.Load())
	}
	if _, ok := gate.Running(share.Training); ok {
		t.Fatal("training still holds the computer after finishing")
	}
	if job.State == StateFailed {
		t.Fatalf("job failed: %s", job.Error)
	}
}
