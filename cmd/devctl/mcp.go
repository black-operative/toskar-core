package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/yeixio/yggdrasil-core/internal/config"
)

// mcpCommand bridges an app that starts MCP servers as programs, such as
// Claude Desktop, to Yggdrasil's MCP endpoint: each line on stdin is
// POSTed to /mcp, and each reply is written as a line on stdout.
//
// TOSKAR_URL changes the address (default http://127.0.0.1:7331), and
// TOSKAR_API_KEY is sent as the bearer token when set. The YGGDRASIL_ names
// from before the rename work too, so existing app configs keep working.
func mcpCommand(args []string, in io.Reader, out io.Writer) error {
	base := strings.TrimRight(config.Env("URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:7331"
	}
	for i := 0; i < len(args); i++ {
		if (args[i] == "--url" || args[i] == "-u") && i+1 < len(args) {
			base = strings.TrimRight(args[i+1], "/")
			i++
		}
	}
	b := &bridge{endpoint: base + "/mcp", key: config.Env("API_KEY"), out: out, client: http.DefaultClient}
	return b.run(in)
}

type bridge struct {
	endpoint string
	key      string
	client   *http.Client
	out      io.Writer
	mu       sync.Mutex
	wg       sync.WaitGroup
}

func (b *bridge) run(in io.Reader) error {
	r := bufio.NewReaderSize(in, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if msg := bytes.TrimSpace(line); len(msg) > 0 {
			// Calls run side by side, so a long answer does not hold up a
			// ping or a cancellation.
			b.wg.Add(1)
			go func(msg []byte) {
				defer b.wg.Done()
				b.forward(msg)
			}(append([]byte(nil), msg...))
		}
		if err == io.EOF {
			b.wg.Wait()
			return nil
		}
		if err != nil {
			b.wg.Wait()
			return err
		}
	}
}

func (b *bridge) forward(msg []byte) {
	req, err := http.NewRequest(http.MethodPost, b.endpoint, bytes.NewReader(msg))
	if err != nil {
		b.fail(msg, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if b.key != "" {
		req.Header.Set("Authorization", "Bearer "+b.key)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.fail(msg, "Toskar is not running on this computer. Start it, then try again.")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent:
		return
	case resp.StatusCode == http.StatusUnauthorized:
		b.fail(msg, "Toskar needs an API key. Create one on its API Access page and set TOSKAR_API_KEY.")
		return
	case resp.StatusCode >= 300:
		b.fail(msg, fmt.Sprintf("Toskar answered with HTTP %d", resp.StatusCode))
		return
	}
	if body = bytes.TrimSpace(body); len(body) > 0 {
		b.write(body)
	}
}

// fail answers a request with an error, so the app shows why instead of
// waiting. Notifications get no answer.
func (b *bridge) fail(msg []byte, text string) {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(msg, &m) != nil || len(m.ID) == 0 {
		return
	}
	resp, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": m.ID,
		"error": map[string]any{"code": -32603, "message": text},
	})
	b.write(resp)
}

func (b *bridge) write(line []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.out.Write(append(line, '\n'))
}
