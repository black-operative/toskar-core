package llamacpp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Tokenize counts the tokens text takes with the model a llama-server is
// running, without the special tokens a chat template adds (AI experience
// spec §66).
func Tokenize(ctx context.Context, endpoint, text string) (int, error) {
	if endpoint == "" {
		return 0, fmt.Errorf("model endpoint required")
	}
	var out struct {
		Tokens *[]any `json:"tokens"`
	}
	if err := postJSON(ctx, endpoint+"/tokenize", map[string]any{"content": text, "add_special": false}, &out); err != nil {
		return 0, err
	}
	if out.Tokens == nil {
		return 0, fmt.Errorf("llama-server returned no tokens")
	}
	return len(*out.Tokens), nil
}

// Window is the token window a llama-server is actually running with, from
// its /props (default_generation_settings.n_ctx, or n_ctx in older builds).
// The context gauge uses it so its limit matches the model (#230).
func Window(ctx context.Context, endpoint string) (int, error) {
	if endpoint == "" {
		return 0, fmt.Errorf("model endpoint required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/props", nil)
	if err != nil {
		return 0, err
	}
	resp, err := supportHTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("llama-server /props: %d", resp.StatusCode)
	}
	var out struct {
		NCtx     int `json:"n_ctx"`
		Defaults struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	if out.Defaults.NCtx > 0 {
		return out.Defaults.NCtx, nil
	}
	if out.NCtx > 0 {
		return out.NCtx, nil
	}
	return 0, fmt.Errorf("llama-server /props has no n_ctx")
}
