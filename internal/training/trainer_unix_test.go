//go:build unix

package training

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancelStopsTheWholeProcessGroup(t *testing.T) {
	spec := fakeSpec(t)
	pidFile := filepath.Join(spec.WorkDir, "child.pid")
	// The trainer starts a worker, as data loaders do, then waits.
	script := writeScript(t, spec.WorkDir, `
sleep 60 &
echo $! > `+pidFile+`
echo '@@ygg {"event":"stage","stage":"training","detail":"Training"}'
wait
`)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := runScript(ctx, spec, []string{script}, func(u Update) {
			if u.State == StateTraining {
				started <- struct{}{}
			}
		})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("trainer did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the trainer")
	}
	raw, _ := os.ReadFile(pidFile)
	var pid int
	for _, c := range strings.TrimSpace(string(raw)) {
		pid = pid*10 + int(c-'0')
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = exec.Command("kill", "-9", strings.TrimSpace(string(raw))).Run()
	t.Fatal("the trainer's child process survived cancel")
}
