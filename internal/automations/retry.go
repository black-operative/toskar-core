package automations

import (
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

const (
	// MaxAttempts is the initial execution plus retries. The occurrence stops after this many tries.
	MaxAttempts = 3
)

// FailureClass decides whether an occurrence may be tried again.
type FailureClass string

const (
	FailureTransient FailureClass = "transient"
	FailurePermanent FailureClass = "permanent"
)

// RetryDelay is the wait before another attempt. The attempt is the try that just failed.
func RetryDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return time.Minute
	case attempt == 2:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// Error codes whose failures may pass on their own, and ones that won't
// (#204). A code decides before the error's English text does, so a
// message in another language, or reworded, is classified the same.
var (
	transientCodes = map[string]bool{
		"CONNECTION_LOST":    true,
		"COMPUTER_OFFLINE":   true,
		"MODEL_BUSY":         true,
		"SUPPORT_MODEL_BUSY": true,
		"RUNTIME_ERROR":      true,
	}
	permanentCodes = map[string]bool{
		"OUT_OF_MEMORY":         true,
		"MODEL_UNHEALTHY":       true,
		"MODEL_NOT_INSTALLED":   true,
		"NO_MODEL_INSTALLED":    true,
		"NO_MODEL_ASSIGNED":     true,
		"RUNTIME_NOT_INSTALLED": true,
		"CONTEXT_TOO_LONG":      true,
		"AUTOMATION_TIMEOUT":    true,
	}
)

// ClassifyFailure retries timeouts and connection drops.
// A model that cannot start, an out-of-memory failure, or a tool failure stays failed.
// Anything else stays failed as well, so an unknown error cannot retry without a bound that was chosen for it.
// The error's code decides first; its text only when it has no code this knows.
func ClassifyFailure(err error) FailureClass {
	if err == nil {
		return FailurePermanent
	}
	if code, _ := contracts.ErrorCode(err); code != "" {
		switch {
		case permanentCodes[code]:
			return FailurePermanent
		case transientCodes[code]:
			return FailureTransient
		}
	}
	msg := strings.ToLower(err.Error())
	if permanentFailure(msg) {
		return FailurePermanent
	}
	if transientFailure(msg) {
		return FailureTransient
	}
	return FailurePermanent
}

func permanentFailure(msg string) bool {
	switch {
	case strings.Contains(msg, "out of memory"),
		strings.Contains(msg, "cannot allocate"),
		strings.Contains(msg, "oom killed"),
		strings.Contains(msg, "(oom)"),
		strings.Contains(msg, " oom"),
		strings.Contains(msg, "no model"),
		strings.Contains(msg, "model failed"),
		strings.Contains(msg, "failed to start"),
		strings.Contains(msg, "not allowed for unattended"),
		strings.Contains(msg, "denied by policy"),
		strings.Contains(msg, "denied by user"),
		strings.Contains(msg, "tool "):
		return true
	default:
		return false
	}
}

func transientFailure(msg string) bool {
	switch {
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "temporarily"),
		strings.Contains(msg, "temporary"),
		strings.Contains(msg, "unreachable"),
		strings.Contains(msg, "i/o timeout"):
		return true
	default:
		return false
	}
}
