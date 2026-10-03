//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
	"time"
)

// hideWindow puts the server in its own process group, so stopping it also
// stops what it started, such as the node process npx runs.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// reapGroup ends the processes left in the server's group after it exited:
// SIGTERM, then SIGKILL for any still there a second later.
func reapGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	group := -cmd.Process.Pid
	if syscall.Kill(group, syscall.SIGTERM) != nil {
		return // nothing left
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(group, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(group, syscall.SIGKILL)
}
