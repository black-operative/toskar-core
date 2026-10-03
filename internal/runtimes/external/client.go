package external

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// ModelPrefix marks a model served by the external server in Yggdrasil's
// model list, such as ext:gpt-4o-mini.
const ModelPrefix = "ext:"

// IsModel reports whether a model ID names a model on the external server.
func IsModel(id string) bool { return strings.HasPrefix(id, ModelPrefix) }

// ServerModel is the server's own name for an ext: model ID.
func ServerModel(id string) string { return strings.TrimPrefix(id, ModelPrefix) }

// NormalizeURL checks a base URL and returns it without a trailing slash or
// /v1, which requests add.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("the base URL must start with http:// or https://, such as https://api.openai.com/v1")
	}
	return strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "/v1"), nil
}

// Host is where requests go, for What left this computer.
func (r *Runtime) Host() string {
	if u, err := url.Parse(r.Config().BaseURL); err == nil {
		return u.Host
	}
	return ""
}

func (r *Runtime) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	cfg := r.Config()
	if cfg.BaseURL == "" {
		return nil, errors.New("no external server is set up")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+"/v1"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := r.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the external server: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("the external server refused the API key (%d): %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("the external server answered %d: %s", resp.StatusCode, msg)
	}
	return resp, nil
}

func (r *Runtime) http() *http.Client {
	if r.client != nil {
		return r.client
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// ListModels is the server's model list (GET /v1/models).
func (r *Runtime) ListModels(ctx context.Context) ([]string, error) {
	resp, err := r.request(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, errors.New("the external server's model list wasn't OpenAI-compatible")
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// Chat streams a reply from model on the external server (POST
// /v1/chat/completions with stream).
func (r *Runtime) Chat(ctx context.Context, model string, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
	body := map[string]any{"model": model, "messages": req.Messages, "stream": true}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	// Ask for the token counts at the end of the stream, so the context
	// gauge shows the real prompt size (#230). A server that rejects the
	// option is asked again without it.
	body["stream_options"] = map[string]any{"include_usage": true}
	resp, err := r.request(ctx, http.MethodPost, "/chat/completions", body)
	if err != nil && strings.Contains(err.Error(), "stream_options") {
		delete(body, "stream_options")
		resp, err = r.request(ctx, http.MethodPost, "/chat/completions", body)
	}
	if err != nil {
		return nil, err
	}
	out := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		send := func(c pluginapi.ChatChunk) bool {
			select {
			case out <- c:
				return true
			case <-ctx.Done():
				return false
			}
		}
		var metrics *pluginapi.GenerationMetrics
		done := func() { send(pluginapi.ChatChunk{Done: true, Metrics: metrics}) }
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				done()
				return
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &ev) != nil {
				continue
			}
			if ev.Usage != nil && ev.Usage.PromptTokens > 0 {
				metrics = &pluginapi.GenerationMetrics{
					PromptTokens:     ev.Usage.PromptTokens,
					CompletionTokens: ev.Usage.CompletionTokens,
					TotalTokens:      ev.Usage.PromptTokens + ev.Usage.CompletionTokens,
				}
			}
			if ev.Error != nil {
				send(pluginapi.ChatChunk{Error: "the external server stopped: " + ev.Error.Message, Done: true})
				return
			}
			for _, c := range ev.Choices {
				if c.Delta.Content != "" && !send(pluginapi.ChatChunk{Content: c.Delta.Content}) {
					return
				}
			}
		}
		if err := sc.Err(); err != nil && ctx.Err() == nil {
			send(pluginapi.ChatChunk{Error: "the external server's reply was cut off: " + err.Error(), Done: true})
			return
		}
		done()
	}()
	return out, nil
}
