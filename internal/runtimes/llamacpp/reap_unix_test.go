//go:build unix

package llamacpp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitGone(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("%s (pid %d) is still running after the model stopped", what, pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stopping a model ends llama-server and every process it started, so a
// daemon that loads and unloads models for weeks leaves nothing behind
// (#231). StopAll at shutdown stops each model the same way.
func TestStoppingAModelLeavesNoProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "llamacpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.Args[0], filepath.Join(root, "llamacpp", "llama-server")); err != nil {
		t.Fatal(err)
	}
	if bundledLlamaServer() != "" {
		t.Skip("a bundled llama-server takes precedence on this machine")
	}
	model := filepath.Join(root, "model.gguf")
	_ = os.WriteFile(model, []byte("GGUF"), 0o600)
	r := New(root, t.TempDir())

	for i := range 2 {
		pidFile := filepath.Join(root, fmt.Sprintf("child-%d.pid", i))
		t.Setenv(fakeLlamaEnv, pidFile)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		running, err := r.StartModel(ctx, pluginapi.ModelStartConfig{ModelID: fmt.Sprintf("m%d", i), ModelPath: model, Mode: pluginapi.ModeEmbedding})
		cancel()
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		r.sup.mu.Lock()
		server := r.sup.procs[running.ID].cmd.Process.Pid
		r.sup.mu.Unlock()
		var child int
		for deadline := time.Now().Add(5 * time.Second); child == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			b, _ := os.ReadFile(pidFile)
			child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if child == 0 || !alive(server) || !alive(child) {
			t.Fatalf("fake llama-server %d and child %d should be running", server, child)
		}

		if err := r.StopModel(context.Background(), running.ID); err != nil {
			t.Fatal(err)
		}
		waitGone(t, server, "llama-server")
		waitGone(t, child, "llama-server's child")
	}
	if list, _ := r.ListRunning(context.Background()); len(list) != 0 {
		t.Fatalf("still listed as running: %v", list)
	}
}
