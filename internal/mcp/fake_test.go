package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/leakcheck"
)

// The test binary doubles as an MCP server over stdio when MCP_FAKE is
// set, so the stdio transport runs a real child process.
func TestMain(m *testing.M) {
	if os.Getenv("MCP_FAKE_CHILD") == "1" {
		// A helper process a server started, as npx starts node.
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if os.Getenv("MCP_FAKE") == "1" {
		runFakeStdio(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	// Goroutines the tests start must be gone when they end (#231).
	leakcheck.Main(m)
}

// fakeStdioSpec runs this test binary as the fake server.
func fakeStdioSpec(name string, env map[string]string) Spec {
	e := map[string]string{"MCP_FAKE": "1"}
	for k, v := range env {
		e[k] = v
	}
	return Spec{Name: name, Command: os.Args[0], Args: []string{"-test.run=^$"}, Env: e}
}

// fake is a small MCP server with one of each kind of tool.
type fake struct {
	// ask sends a request to the client and waits for its result; nil
	// when the transport cannot (HTTP without a stream).
	ask    func(method string, params any) (json.RawMessage, error)
	notify func(method string, params any)
}

func (f *fake) handle(m message) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}, "resources": map[string]any{}, "prompts": map[string]any{}, "logging": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake-server", "version": "1.2.3"},
			"instructions":    "Use get_weather for weather.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		var p struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(m.Params, &p)
		readOnly := true
		page1 := []map[string]any{
			{"name": "get_weather", "description": "Weather for a city.", "inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}, "units": map[string]any{"enum": []string{"c", "f"}}},
				"required": []string{"city"}}, "annotations": map[string]any{"readOnlyHint": readOnly}},
			{"name": "create_note", "description": "Create a note.", "inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}},
		}
		page2 := []map[string]any{
			{"name": "echo_secret", "description": "Echo the token.", "inputSchema": map[string]any{"type": "object"}},
			{"name": "fail", "description": "Always fails.", "inputSchema": map[string]any{"type": "object"}},
			{"name": "list_roots", "description": "Show roots.", "inputSchema": map[string]any{"type": "object"}},
			{"name": "ask_ai", "description": "Ask the client's AI.", "inputSchema": map[string]any{"type": "object"}},
		}
		if p.Cursor == "" {
			return map[string]any{"tools": page1, "nextCursor": "2"}, nil
		}
		return map[string]any{"tools": page2}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(m.Params, &p)
		text := func(s string) any {
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
		}
		switch p.Name {
		case "get_weather":
			return text(fmt.Sprintf("Sunny in %v", p.Arguments["city"])), nil
		case "create_note":
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "Created " + fmt.Sprint(p.Arguments["title"])},
				map[string]any{"type": "resource_link", "uri": "https://notes.example/1", "name": "My note"},
				map[string]any{"type": "image", "data": "AAAA", "mimeType": "image/png"},
			}}, nil
		case "echo_secret":
			return text("token is " + os.Getenv("API_TOKEN")), nil
		case "fail":
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": "boom"}}, "isError": true}, nil
		case "list_roots":
			if f.ask == nil {
				return text("no roots"), nil
			}
			raw, err := f.ask("roots/list", nil)
			if err != nil {
				return nil, &rpcError{Code: codeInternal, Message: err.Error()}
			}
			return text(string(raw)), nil
		case "ask_ai":
			if f.ask == nil {
				return text("no sampling"), nil
			}
			raw, err := f.ask("sampling/createMessage", map[string]any{
				"messages":  []any{map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "Say hi"}}},
				"maxTokens": 20,
			})
			if err != nil {
				return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
			}
			var res struct {
				Content Content `json:"content"`
				Model   string  `json:"model"`
			}
			_ = json.Unmarshal(raw, &res)
			return text(res.Model + ": " + res.Content.Text), nil
		}
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool " + p.Name}
	case "resources/list":
		return map[string]any{"resources": []any{map[string]any{"uri": "note://1", "name": "First note"}}}, nil
	case "resources/read":
		return map[string]any{"contents": []any{map[string]any{"uri": "note://1", "text": "hello resource"}}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{map[string]any{"name": "summarize", "arguments": []any{map[string]any{"name": "topic", "required": true}}}}}, nil
	case "prompts/get":
		var p struct {
			Arguments map[string]string `json:"arguments"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return map[string]any{"messages": []any{map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "Summarize " + p.Arguments["topic"]}}}}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "no " + m.Method}
}

func runFakeStdio(in io.Reader, out io.Writer) {
	var mu sync.Mutex
	write := func(v any) {
		raw, _ := json.Marshal(v)
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(append(raw, '\n'))
	}
	// A server may start a helper that outlives its own stdin, without
	// sharing its output.
	if pidFile := os.Getenv("MCP_FAKE_CHILD_PIDFILE"); pidFile != "" {
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "MCP_FAKE_CHILD=1")
		if child.Start() == nil {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		}
	}
	// Some servers print a banner before speaking the protocol.
	mu.Lock()
	_, _ = out.Write([]byte("fake server starting\n"))
	mu.Unlock()
	fmt.Fprintln(os.Stderr, "fake server log line")

	pending := map[string]chan message{}
	var pmu sync.Mutex
	next := 0
	f := &fake{}
	f.notify = func(method string, params any) {
		write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}
	f.ask = func(method string, params any) (json.RawMessage, error) {
		pmu.Lock()
		next++
		id := "s" + strconv.Itoa(next)
		ch := make(chan message, 1)
		pending[id] = ch
		pmu.Unlock()
		write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		resp := <-ch
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if s := strings.TrimSpace(string(line)); s != "" {
			var m message
			if json.Unmarshal([]byte(s), &m) == nil {
				switch {
				case m.isResponse():
					pmu.Lock()
					ch := pending[idKey(m.ID)]
					pmu.Unlock()
					if ch != nil {
						ch <- m
					}
				case m.isRequest():
					go func(m message) {
						res, rerr := f.handle(m)
						write(rpcResponse(m.ID, res, rerr))
					}(m)
				case m.Method == "notifications/initialized":
					f.notify("notifications/message", map[string]any{"level": "info", "data": "ready to serve"})
				}
			}
		}
		if err != nil {
			return
		}
	}
}
