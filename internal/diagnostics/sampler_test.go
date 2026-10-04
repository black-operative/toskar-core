package diagnostics

import (
	"context"
	"testing"
	"time"
)

// The sampler keeps the last Keep samples and stops with its context.
func TestSamplerKeepsADayAndStops(t *testing.T) {
	s := &Sampler{Interval: 10 * time.Millisecond, Keep: 3}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done
	h := s.History()
	if len(h.Samples) != 3 {
		t.Fatalf("kept %d samples, want 3", len(h.Samples))
	}
	if h.Now.Goroutines < 1 || h.Now.HeapBytes == 0 || h.StartedAt.IsZero() || h.Interval != 0 {
		t.Fatalf("history = %+v", h)
	}
	if !h.Samples[0].At.Before(h.Samples[2].At) {
		t.Fatal("samples are not oldest first")
	}
}
