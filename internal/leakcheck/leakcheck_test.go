package leakcheck

import (
	"strings"
	"testing"
	"time"
)

func blockedForever(stop chan struct{}) { <-stop }

func TestFindsAGoroutineThatOutlivesItsTest(t *testing.T) {
	stop := make(chan struct{})
	go blockedForever(stop)
	leaks := Find(100 * time.Millisecond)
	if len(leaks) != 1 || !strings.Contains(leaks[0], "blockedForever") {
		t.Fatalf("leaks = %q", leaks)
	}
	if got := Find(100*time.Millisecond, "blockedForever"); len(got) != 0 {
		t.Fatalf("an ignored stack was reported: %q", got)
	}
	close(stop)
	if got := Find(time.Second); len(got) != 0 {
		t.Fatalf("a finished goroutine was reported: %q", got)
	}
}

func TestMain(m *testing.M) { Main(m) }
