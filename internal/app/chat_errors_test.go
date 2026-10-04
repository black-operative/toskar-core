package app

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestChatErrorCode(t *testing.T) {
	cases := []struct {
		text, code string
		params     map[string]any
	}{
		{"llama-server not installed: run Models → Install engine", "RUNTIME_NOT_INSTALLED", map[string]any{"runtime": "llama-server"}},
		{`model "qwen3-8b" not installed`, "MODEL_NOT_INSTALLED", map[string]any{"model_id": "qwen3-8b"}},
		{"no model assigned for role", "NO_MODEL_ASSIGNED", nil},
		{"no installed models", "NO_MODEL_INSTALLED", nil},
		{"the request exceeds the available context size (8192 tokens)", "CONTEXT_TOO_LONG", nil},
		{"llama-server error 500: failed to allocate buffer", "OUT_OF_MEMORY", nil},
		{"computer studio-mac is offline.", "COMPUTER_OFFLINE", nil},
		{"read tcp 10.0.0.2:7332: connection reset by peer", "CONNECTION_LOST", nil},
		{"llama-server error 503: loading model", "RUNTIME_ERROR", nil},
		{`{"kind":"model_health","reason":"crashed","message":"The model stopped."}`, "MODEL_UNHEALTHY", nil},
		{"this profile keeps work on this computer, so it doesn't use the external server; choose a model on this computer or another profile", "EXTERNAL_OFFLINE_PROFILE", nil},
		{"this chat uses memories or knowledge marked This computer only, so it can't go to the external server; choose a model on this computer", "EXTERNAL_LOCAL_ONLY", nil},
		{"the external server refused the API key (401): bad key", "EXTERNAL_FAILED", map[string]any{"detail": "the external server refused the API key (401): bad key"}},
		{"couldn't reach the external server: dial tcp 10.0.0.9:443: connection refused", "EXTERNAL_FAILED",
			map[string]any{"detail": "couldn't reach the external server: dial tcp 10.0.0.9:443: connection refused"}},
		{"The tool returned nothing.", "", nil},
	}
	for _, c := range cases {
		code, params := chatErrorCode(c.text)
		if code != c.code {
			t.Errorf("%q: code %q, want %q", c.text, code, c.code)
			continue
		}
		for k, v := range c.params {
			if params[k] != v {
				t.Errorf("%q: %s = %v, want %v", c.text, k, params[k], v)
			}
		}
	}
}

func TestCodedChatErrorKeepsText(t *testing.T) {
	err := codedChatError("out of memory while loading")
	if code, _ := contracts.ErrorCode(err); code != "OUT_OF_MEMORY" || err.Error() != "out of memory while loading" {
		t.Fatalf("got %q %q", code, err.Error())
	}
	if code, _ := contracts.ErrorCode(codedChatError("something else")); code != "" {
		t.Fatal("unrecognized text has no code")
	}
}
