package llamacpp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Client streams OpenAI-compatible chat completions from llama-server.
type Client struct {
	http *http.Client
}

// NewClient creates a llama-server HTTP client.
func NewClient() *Client {
	return &Client{http: &http.Client{}}
}

// Chat implements pluginapi.Generator.
func (c *Client) Chat(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
	if req.ModelEndpoint == "" {
		return nil, fmt.Errorf("model endpoint required")
	}
	body := map[string]any{
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if len(req.ResponseSchema) > 0 {
		// llama-server turns the schema into a grammar, so the reply is
		// valid JSON of that shape (§27).
		body["response_format"] = map[string]any{"type": "json_object", "schema": req.ResponseSchema}
	}
	lora, err := loraScales(req.ModelEndpoint, req.Adapter)
	if err != nil {
		return nil, err
	}
	if lora != nil {
		body["lora"] = lora
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(req.ModelEndpoint, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	started := time.Now()
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan pluginapi.ChatChunk, 16)
	if !req.Stream {
		go c.readNonStream(resp, ch, started)
		return ch, nil
	}
	go c.readStream(resp, ch, started)
	return ch, nil
}

func (c *Client) readNonStream(resp *http.Response, ch chan<- pluginapi.ChatChunk, started time.Time) {
	defer close(ch)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	if message, failed := streamDataError(string(body)); failed {
		ch <- pluginapi.ChatChunk{Error: message, Done: true}
		return
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage   *usagePayload   `json:"usage"`
		Timings *timingsPayload `json:"timings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	content := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
	}
	metrics := mergeServerMetrics(out.Usage, out.Timings, started, time.Time{}, time.Now(), content)
	ch <- pluginapi.ChatChunk{Content: content, Done: true, Metrics: metrics}
}

func (c *Client) readStream(resp *http.Response, ch chan<- pluginapi.ChatChunk, started time.Time) {
	defer close(ch)
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var firstToken time.Time
	var content strings.Builder
	var usage *usagePayload
	var timings *timingsPayload
	sentDone := false

	flushDone := func() {
		if sentDone {
			return
		}
		sentDone = true
		metrics := mergeServerMetrics(usage, timings, started, firstToken, time.Now(), content.String())
		ch <- pluginapi.ChatChunk{Done: true, Metrics: metrics}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			flushDone()
			return
		}
		if message, failed := streamDataError(data); failed {
			ch <- pluginapi.ChatChunk{Error: message, Done: true}
			return
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage   *usagePayload   `json:"usage"`
			Timings *timingsPayload `json:"timings"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Timings != nil {
			timings = chunk.Timings
		}
		if len(chunk.Choices) == 0 {
			// Some llama.cpp builds emit a final timings-only frame.
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		done := chunk.Choices[0].FinishReason != nil
		if delta != "" {
			if firstToken.IsZero() {
				firstToken = time.Now()
			}
			content.WriteString(delta)
			ch <- pluginapi.ChatChunk{Content: delta}
		}
		if done {
			flushDone()
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	flushDone()
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type timingsPayload struct {
	PromptN            int     `json:"prompt_n"`
	PromptMS           float64 `json:"prompt_ms"`
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedN         int     `json:"predicted_n"`
	PredictedMS        float64 `json:"predicted_ms"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
	// CacheN is how many prompt tokens came from the cache.
	CacheN int `json:"cache_n"`
}

func mergeServerMetrics(
	usage *usagePayload,
	timings *timingsPayload,
	started, firstToken, ended time.Time,
	content string,
) *pluginapi.GenerationMetrics {
	m := &pluginapi.GenerationMetrics{}
	totalMs := ended.Sub(started).Seconds() * 1000
	m.TotalMs = totalMs
	if !firstToken.IsZero() {
		m.TTFTMs = firstToken.Sub(started).Seconds() * 1000
		m.EvalMs = ended.Sub(firstToken).Seconds() * 1000
	} else {
		m.EvalMs = totalMs
	}

	if timings != nil {
		// prompt_n counts only the tokens processed for this call; the rest
		// of the prompt came from llama-server's cache. The prompt's size,
		// which is what fills the window, is both (§66).
		if timings.PromptN+timings.CacheN > 0 {
			m.PromptTokens = timings.PromptN + timings.CacheN
		}
		if timings.PromptPerSecond == 0 && timings.PromptMS > 0 && timings.PromptN > 0 {
			m.PromptTokPerSec = float64(timings.PromptN) / (timings.PromptMS / 1000)
		}
		if timings.PredictedN > 0 {
			m.CompletionTokens = timings.PredictedN
		}
		if timings.PromptMS > 0 {
			m.PromptMs = timings.PromptMS
		}
		if timings.PredictedMS > 0 {
			m.EvalMs = timings.PredictedMS
		}
		if timings.PromptPerSecond > 0 {
			m.PromptTokPerSec = timings.PromptPerSecond
		}
		if timings.PredictedPerSecond > 0 {
			m.EvalTokPerSec = timings.PredictedPerSecond
		}
		m.CachedTokens = timings.CacheN
	}
	if usage != nil {
		if usage.PromptTokens > 0 {
			m.PromptTokens = usage.PromptTokens
		}
		if usage.CompletionTokens > 0 {
			m.CompletionTokens = usage.CompletionTokens
		}
		if usage.TotalTokens > 0 {
			m.TotalTokens = usage.TotalTokens
		}
	}
	if m.CompletionTokens == 0 && content != "" {
		m.CompletionTokens = estimateTokens(content)
	}
	if m.TotalTokens == 0 {
		m.TotalTokens = m.PromptTokens + m.CompletionTokens
	}
	if m.PromptMs == 0 && m.TTFTMs > 0 {
		m.PromptMs = m.TTFTMs
	}
	if m.PromptTokPerSec == 0 && m.PromptMs > 0 && m.PromptTokens > 0 {
		m.PromptTokPerSec = float64(m.PromptTokens) / (m.PromptMs / 1000)
	}
	if m.EvalTokPerSec == 0 && m.EvalMs > 0 && m.CompletionTokens > 0 {
		m.EvalTokPerSec = float64(m.CompletionTokens) / (m.EvalMs / 1000)
	}
	return m
}

func estimateTokens(s string) int {
	n := len(strings.Fields(s))
	if n == 0 && s != "" {
		return max(1, len(s)/4)
	}
	return n
}

// streamDataError reads a llama-server failure frame. A token frame has no error object.
func streamDataError(data string) (string, bool) {
	var payload struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(data), &payload) != nil || len(payload.Error) == 0 || string(payload.Error) == "null" {
		return "", false
	}
	var asString string
	if json.Unmarshal(payload.Error, &asString) == nil {
		asString = strings.TrimSpace(asString)
		if asString != "" {
			return asString, true
		}
	}
	var asObject struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(payload.Error, &asObject) == nil {
		message := strings.TrimSpace(asObject.Message)
		if message != "" {
			return message, true
		}
	}
	return "", false
}
