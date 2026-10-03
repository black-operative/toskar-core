//go:build unix

package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Stopping a tool source ends what it started too, even a helper that
// outlives the server's own stdin, such as the node process npx runs
// (#231).
func TestClosingAServerEndsWhatItStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	spec := fakeStdioSpec("Helper", map[string]string{"MCP_FAKE_CHILD_PIDFILE": pidFile})
	tr, err := StartProcess(spec.Command, spec.Args, spec.Env, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var child int
	for deadline := time.Now().Add(5 * time.Second); child == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(pidFile)
		child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	alive := func(pid int) bool {
		err := syscall.Kill(pid, 0)
		return err == nil || errors.Is(err, syscall.EPERM)
	}
	if child == 0 || !alive(child) {
		t.Fatalf("the server's helper should be running (pid %d)", child)
	}
	_ = tr.Close()
	deadline := time.Now().Add(5 * time.Second)
	for alive(child) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the server's helper (pid %d) is still running after the source stopped", child)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
