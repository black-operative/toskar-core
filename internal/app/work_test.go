package app

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestActiveWorkCountsStreamingTurns(t *testing.T) {
	a := &App{}
	in := make(chan pluginapi.ChatChunk, 1)
	out := countWork(context.Background(), in, a.beginWork("gpu-box"))
	done := a.beginWork("gpu-box")
	if got := a.activeWork()["gpu-box"]; got != 2 {
		t.Fatalf("two turns: %d", got)
	}
	done()
	done() // ending twice counts once
	in <- pluginapi.ChatChunk{Content: "hi"}
	if c := <-out; c.Content != "hi" {
		t.Fatalf("chunk %+v", c)
	}
	close(in)
	for range out {
	}
	deadline := time.Now().Add(time.Second)
	for len(a.activeWork()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.activeWork(); len(w) != 0 {
		t.Fatalf("after the streams ended: %v", w)
	}

	// A turn whose reader went away still ends its count.
	ctx, cancel := context.WithCancel(context.Background())
	stuck := make(chan pluginapi.ChatChunk)
	_ = countWork(ctx, stuck, a.beginWork("studio"))
	go func() { stuck <- pluginapi.ChatChunk{Content: "unread"} }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	deadline = time.Now().Add(time.Second)
	for len(a.activeWork()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if w := a.activeWork(); len(w) != 0 {
		t.Fatalf("after the turn was cancelled: %v", w)
	}
}
