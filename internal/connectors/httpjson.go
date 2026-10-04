package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxBody caps how much of a service's response is read.
const maxBody = 4 << 20

// call sends a JSON request and decodes the JSON response into out. Errors
// carry the service's own message when it gives one, never the request's
// headers.
func call(ctx context.Context, c *http.Client, method, url string, header map[string]string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Toskar")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", req.URL.Host, unwrapURLError(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return statusError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func statusError(code int, raw []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &msg)
	detail := strings.TrimSpace(msg.Message)
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("the token was not accepted (401). It may have expired or been revoked")
	case http.StatusForbidden:
		if detail == "" {
			detail = "the token does not have permission for this"
		}
		return fmt.Errorf("not allowed (403): %s", detail)
	case http.StatusNotFound:
		return fmt.Errorf("not found (404), or the token cannot see it")
	}
	if detail == "" {
		detail = http.StatusText(code)
	}
	return fmt.Errorf("the service returned %d: %s", code, detail)
}

// unwrapURLError drops the URL from a transport error, keeping the cause.
func unwrapURLError(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok && u.Unwrap() != nil {
		return u.Unwrap()
	}
	return err
}

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func num(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(v), "#"), "%d", &n)
		return n
	}
	return 0
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
