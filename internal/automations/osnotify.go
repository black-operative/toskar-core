package automations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSSender posts a notification with the operating system's usual command.
type OSSender struct {
	// Run starts the platform command. Tests set it. The default starts a process.
	Run func(ctx context.Context, name string, args []string, env []string) error
}

// Notify posts the notice when this operating system has a notifier.
func (s OSSender) Notify(ctx context.Context, notice Notice) error {
	notice = noticeTitle(notice.Title, notice)
	name, args, env, err := notifyCommand(runtime.GOOS, notice)
	if err != nil {
		return err
	}
	run := s.Run
	if run == nil {
		run = execNotify
	}
	return run(ctx, name, args, env)
}

func notifyCommand(goos string, notice Notice) (name string, args []string, env []string, err error) {
	switch goos {
	case "darwin":
		return "osascript", []string{
			"-e", `on run argv
display notification (item 2 of argv) with title (item 1 of argv)
end run`,
			notice.Title, notice.Body,
		}, nil, nil
	case "linux":
		return "notify-send", []string{"--app-name", "Toskar", notice.Title, notice.Body}, nil, nil
	case "windows":
		return "powershell", []string{
				"-NoProfile", "-NonInteractive", "-Command", windowsNotifyScript,
			}, []string{
				"YGG_NOTIFY_TITLE=" + notice.Title,
				"YGG_NOTIFY_BODY=" + notice.Body,
			}, nil
	default:
		return "", nil, nil, fmt.Errorf("notifications are not available on %s", goos)
	}
}

const windowsNotifyScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$icon = New-Object System.Windows.Forms.NotifyIcon
$icon.Icon = [System.Drawing.SystemIcons]::Information
$icon.Visible = $true
$icon.BalloonTipTitle = $env:YGG_NOTIFY_TITLE
$icon.BalloonTipText = $env:YGG_NOTIFY_BODY
$icon.ShowBalloonTip(5000)
Start-Sleep -Milliseconds 1500
$icon.Dispose()
`

func execNotify(ctx context.Context, name string, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}
