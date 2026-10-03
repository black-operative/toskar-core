// Package leakcheck fails a package's tests when goroutines they started are
// still running after the last test (#231). Use it from TestMain:
//
//	func TestMain(m *testing.M) { leakcheck.Main(m) }
//
// A leaked goroutine is usually a stream, subscriber, timer, or connection
// nobody stopped; in a daemon that runs for weeks it grows without bound.
package leakcheck

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// settle is how long Main waits for goroutines to finish after the tests.
const settle = 5 * time.Second

// expected are stacks that belong to the Go runtime or the test framework,
// not to the code under test.
var expected = []string{
	"testing.(*M).",
	"testing.RunTests",
	"testing.(*T).Run",
	"testing.tRunner",
	"runtime.goexit0",
	"runtime.ReadTrace",
	"os/signal.signal_recv",
	"os/signal.loop",
	"runtime.ensureSigM",
	"runtime/trace.Start",
	"internal/poll.runtime_pollWait\nnet/http.(*persistConn).readLoop", // idle keep-alive, closed with the transport
}

// Main runs the tests and, when they pass, fails if goroutines started
// during them are still running after a short wait. ignore names more
// stacks to accept, as substrings, for goroutines a package starts on
// purpose for the life of the process.
func Main(m *testing.M, ignore ...string) {
	code := m.Run()
	if code == 0 {
		if leaks := Find(settle, ignore...); len(leaks) > 0 {
			fmt.Fprintf(os.Stderr, "leakcheck: %d goroutine(s) still running after the tests:\n\n%s\n", len(leaks), strings.Join(leaks, "\n\n"))
			code = 1
		}
	}
	os.Exit(code)
}

// Find waits up to wait for goroutines other than the caller, the runtime,
// and the test framework to finish, and returns the stacks of those that
// are still running.
func Find(wait time.Duration, ignore ...string) []string {
	deadline := time.Now().Add(wait)
	for {
		leaks := running(ignore)
		if len(leaks) == 0 || time.Now().After(deadline) {
			return leaks
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func running(ignore []string) []string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var out []string
	// The first stack is the caller's own goroutine.
	for i, stack := range bytes.Split(buf, []byte("\n\n")) {
		if i == 0 || len(bytes.TrimSpace(stack)) == 0 {
			continue
		}
		s := string(stack)
		if matches(s, expected) || matches(s, ignore) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func matches(stack string, patterns []string) bool {
	for _, p := range patterns {
		if p != "" && strings.Contains(stack, p) {
			return true
		}
	}
	return false
}
