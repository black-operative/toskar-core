package tools

import (
	"context"
	"errors"
	"strings"
	"time"
)

// aliases map capability names that models and clients often use to the
// built-in tool ids (spec §18). Ids stay stable for stored profiles; the
// aliases let "web.search" or "shell.run" reach the same tool.
var aliases = map[string]string{
	"web.search":    "internet.search",
	"web.open":      "internet.open",
	"web.fetch":     "internet.open",
	"files.read":    "filesystem.read",
	"files.write":   "filesystem.write",
	"files.search":  "filesystem.search",
	"file.read":     "filesystem.read",
	"file.write":    "filesystem.write",
	"shell.run":     "terminal",
	"shell.execute": "terminal",
	"terminal.run":  "terminal",
	"file.create":   "files.create",
	// Document and spreadsheet capabilities (Gungnir §21–22) are files.create
	// with a format; see formatFor.
	"document.create":    "files.create",
	"pdf.create":         "files.create",
	"spreadsheet.create": "files.create",
}

// capabilityFormats are the file formats capability names ask files.create for.
var capabilityFormats = map[string]string{"document.create": "docx", "pdf.create": "pdf", "spreadsheet.create": "xlsx"}

// formatFor is the format a capability name asks for, if any.
func formatFor(id string) string { return capabilityFormats[strings.ToLower(strings.TrimSpace(id))] }

// Canonical returns the built-in id for a tool id or one of its capability
// aliases. Unknown ids are returned unchanged.
func Canonical(id string) string {
	id = strings.TrimSpace(id)
	if c, ok := aliases[strings.ToLower(id)]; ok {
		return c
	}
	return id
}

// timeouts cap how long one call may run (spec §19). Cancelling the turn
// still stops a call sooner.
var timeouts = map[string]time.Duration{
	CapInternet: 45 * time.Second,
	CapFiles:    30 * time.Second,
	CapShell:    2 * time.Minute,
	CapGit:      90 * time.Second,
	// The first call installs the code environment; each run is held to
	// its own, shorter limit.
	CapCode: 10 * time.Minute,
	// The first call installs speech and downloads a model.
	CapSpeech: 15 * time.Minute,
	// An image on a slow computer; the image's own limit stops it sooner.
	CapImage: 25 * time.Minute,
	// A page action waits for the page to settle; its own limit is 45 seconds.
	CapBrowser: 90 * time.Second,
	// A clip on a slow computer; its own limit stops it sooner.
	CapVideo: 80 * time.Minute,
	// A request the words can't read is read by a model.
	CapAutomations: 2 * time.Minute,
}

const defaultTimeout = time.Minute

// mcpTimeout is longer: a tool source on this computer may need to start
// first, and some, such as a browser, take a while per step.
const mcpTimeout = 3 * time.Minute

// Timeout is the longest a tool's call may run.
func Timeout(id string) time.Duration {
	if def, ok := Lookup(id); ok {
		if d, ok := timeouts[def.Capability]; ok {
			return d
		}
		if strings.HasPrefix(def.Source, "mcp:") {
			return mcpTimeout
		}
	}
	return defaultTimeout
}

// Error kinds a tool call can end with, so callers and the model see the
// same few outcomes whatever the tool (spec §19).
const (
	ErrKindTimeout    = "timeout"
	ErrKindCancelled  = "cancelled"
	ErrKindDenied     = "denied"
	ErrKindNotOffered = "not_offered"
	ErrKindInvalid    = "invalid"
	ErrKindFailed     = "failed"
)

// ErrNotOffered is returned for a call to a tool that was not offered for
// the request; the model cannot widen its own tools (spec §17).
var ErrNotOffered = errors.New("that tool is not available for this request")

// ErrorKind classifies a tool error.
func ErrorKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return ErrKindTimeout
	case errors.Is(err, context.Canceled):
		return ErrKindCancelled
	case errors.Is(err, ErrNotOffered):
		return ErrKindNotOffered
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "denied") || strings.Contains(msg, "not allowed") || strings.Contains(msg, "disabled"):
		return ErrKindDenied
	case strings.Contains(msg, "required") || strings.Contains(msg, "invalid") || strings.Contains(msg, "malformed") || strings.Contains(msg, "not found"):
		return ErrKindInvalid
	}
	return ErrKindFailed
}
