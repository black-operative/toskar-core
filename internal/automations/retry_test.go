package automations_test

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
)

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		message string
		kind    automations.FailureClass
	}{
		{message: "connection refused", kind: automations.FailureTransient},
		{message: "model endpoint timeout", kind: automations.FailureTransient},
		{message: "deadline exceeded", kind: automations.FailureTransient},
		{message: "node is unreachable", kind: automations.FailureTransient},
		{message: "model failed to start", kind: automations.FailurePermanent},
		{message: "model ran out of memory", kind: automations.FailurePermanent},
		{message: "llama oom killed", kind: automations.FailurePermanent},
		{message: `tool "internet.search" denied by policy`, kind: automations.FailurePermanent},
		{message: "tool is not allowed for unattended execution", kind: automations.FailurePermanent},
		{message: "prompt was empty", kind: automations.FailurePermanent},
	}
	for _, tc := range cases {
		t.Run(tc.message, func(t *testing.T) {
			got := automations.ClassifyFailure(errExec(tc.message))
			if got != tc.kind {
				t.Fatalf("class = %s, want %s", got, tc.kind)
			}
		})
	}
}

func TestRetryDelayGrowsThenStops(t *testing.T) {
	if automations.RetryDelay(1) != time.Minute {
		t.Fatalf("first delay = %s", automations.RetryDelay(1))
	}
	if automations.RetryDelay(2) != 5*time.Minute {
		t.Fatalf("second delay = %s", automations.RetryDelay(2))
	}
	if automations.MaxAttempts != 3 {
		t.Fatalf("max attempts = %d", automations.MaxAttempts)
	}
}
