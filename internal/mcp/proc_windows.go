//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console window from opening for the server.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

func terminate(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// reapGroup has nothing to do on Windows: there are no process groups to
// signal. Ending a server's helpers there would need a job object.
func reapGroup(*exec.Cmd) {}
