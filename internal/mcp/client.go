package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Transport moves JSON-RPC messages to and from one server.
type Transport interface {
	// Send writes one message. For HTTP it also starts reading the reply.
	Send(ctx context.Context, msg []byte) error
	// Incoming yields messages from the server. It is closed when the
	// connection ends.
	Incoming() <-chan []byte
	// Err explains why Incoming closed, if it closed on its own.
	Err() error
	Close() error
}

// Hooks are what a server may ask of Yggdrasil while connected.
type Hooks struct {
	// Roots are the folders the server may work in, as file:// URIs.
	Roots func() []Root
	// Sample answers a server's request for an AI reply. Nil declines.
	Sample func(ctx context.Context, system string, messages []SampleMessage, maxTokens int) (text, model string, err error)
	// ToolsChanged is called when the server's tool list changes.
	ToolsChanged func()
	// Log receives the server's log messages.
	Log func(level, text string)
}

// Root is a folder a server may work in.
type Root struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

// SampleMessage is one text message of a sampling request.
type SampleMessage struct {
	Role string
	Text string
}

// ErrClosed is returned for a call on a connection that has ended.
var ErrClosed = errors.New("the tool source stopped")

// Client is one initialized connection to a server.
type Client struct {
	t       Transport
	hooks   Hooks
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[string]chan message
	done    chan struct{}
	err     error

	Server       implementation
	Version      string
	Instructions string
	HasTools     bool
	HasResources bool
	HasPrompts   bool
}

// Connect initializes a session over t. On failure t is closed.
func Connect(ctx context.Context, t Transport, hooks Hooks, clientVersion string) (*Client, error) {
	c := &Client{t: t, hooks: hooks, pending: map[string]chan message{}, done: make(chan struct{})}
	go c.read()
	caps := map[string]any{"roots": map[string]any{"listChanged": false}}
	if hooks.Sample != nil {
		caps["sampling"] = map[string]any{}
	}
	var res initializeResult
	err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    caps,
		ClientInfo:      implementation{Name: "toskar", Title: "Toskar", Version: clientVersion},
	}, &res)
	if err != nil {
		t.Close()
		return nil, err
	}
	if !supportedVersions[res.ProtocolVersion] {
		t.Close()
		return nil, fmt.Errorf("the server speaks MCP version %q, which Toskar does not support yet", res.ProtocolVersion)
	}
	c.Server, c.Version, c.Instructions = res.ServerInfo, res.ProtocolVersion, res.Instructions
	c.HasTools = res.Capabilities.Tools != nil
	c.HasResources = res.Capabilities.Resources != nil
	c.HasPrompts = res.Capabilities.Prompts != nil
	if v, ok := t.(interface{ SetProtocolVersion(string) }); ok {
		v.SetProtocolVersion(res.ProtocolVersion)
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		t.Close()
		return nil, err
	}
	return c, nil
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err explains why the connection ended.
func (c *Client) Err() error {
	select {
	case <-c.done:
	default:
		return nil
	}
	if c.err != nil {
		return c.err
	}
	return ErrClosed
}

// Close ends the connection.
func (c *Client) Close() error { return c.t.Close() }

// ListTools returns every tool, following pages.
func (c *Client) ListTools(ctx context.Context) ([]RemoteTool, error) {
	var out []RemoteTool
	cursor := ""
	for page := 0; page < 50; page++ {
		var res listToolsResult
		if err := c.call(ctx, "tools/list", cursorParams(cursor), &res); err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if cursor = res.NextCursor; cursor == "" {
			break
		}
	}
	return out, nil
}

// CallTool runs a tool. A result the server marks as an error is returned
// with isError set, not as err.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (callToolResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	var res callToolResult
	err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res)
	return res, err
}

// ListResources returns every resource, following pages.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var out []Resource
	cursor := ""
	for page := 0; page < 20; page++ {
		var res listResourcesResult
		if err := c.call(ctx, "resources/list", cursorParams(cursor), &res); err != nil {
			return nil, err
		}
		out = append(out, res.Resources...)
		if cursor = res.NextCursor; cursor == "" {
			break
		}
	}
	return out, nil
}

// ReadResource returns a resource's contents.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceText, error) {
	var res readResourceResult
	err := c.call(ctx, "resources/read", map[string]any{"uri": uri}, &res)
	return res.Contents, err
}

// ListPrompts returns every prompt, following pages.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var out []Prompt
	cursor := ""
	for page := 0; page < 20; page++ {
		var res listPromptsResult
		if err := c.call(ctx, "prompts/list", cursorParams(cursor), &res); err != nil {
			return nil, err
		}
		out = append(out, res.Prompts...)
		if cursor = res.NextCursor; cursor == "" {
			break
		}
	}
	return out, nil
}

// GetPrompt fills a prompt and returns its text.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	var res getPromptResult
	if err := c.call(ctx, "prompts/get", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return "", err
	}
	var parts []string
	for _, m := range res.Messages {
		if t := contentText(m.Content); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// Ping checks the server answers.
func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, "ping", nil, nil)
}

func cursorParams(cursor string) any {
	if cursor == "" {
		return nil
	}
	return map[string]any{"cursor": cursor}
}

func (c *Client) call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan message, 1)
	c.mu.Lock()
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()
	msg := message{JSONRPC: "2.0", ID: json.RawMessage(key), Method: method}
	if params != nil {
		msg.Params = mustJSON(params)
	}
	if err := c.t.Send(ctx, mustJSON(msg)); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		// Tell the server to stop working on it; initialize cannot be
		// cancelled.
		if method != "initialize" {
			_ = c.notify(context.WithoutCancel(ctx), "notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled"})
		}
		return ctx.Err()
	case <-c.done:
		return c.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("the server sent a reply Toskar could not read: %w", err)
			}
		}
		return nil
	}
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	msg := message{JSONRPC: "2.0", Method: method}
	if params != nil {
		msg.Params = mustJSON(params)
	}
	return c.t.Send(ctx, mustJSON(msg))
}

func (c *Client) read() {
	defer func() {
		c.err = c.t.Err()
		close(c.done)
	}()
	for raw := range c.t.Incoming() {
		// A batch is an array of messages.
		if len(raw) > 0 && raw[0] == '[' {
			var batch []message
			if json.Unmarshal(raw, &batch) == nil {
				for _, m := range batch {
					c.handle(m)
				}
			}
			continue
		}
		var m message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		c.handle(m)
	}
}

func (c *Client) handle(m message) {
	switch {
	case m.isResponse():
		c.mu.Lock()
		ch := c.pending[idKey(m.ID)]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- m:
			default:
			}
		}
	case m.isRequest():
		go c.answer(m)
	default:
		c.notification(m)
	}
}

// idKey normalizes an id so 7 and "7" match.
func idKey(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func (c *Client) notification(m message) {
	switch m.Method {
	case "notifications/tools/list_changed":
		if c.hooks.ToolsChanged != nil {
			go c.hooks.ToolsChanged()
		}
	case "notifications/message":
		if c.hooks.Log == nil {
			return
		}
		var p logMessageParams
		if json.Unmarshal(m.Params, &p) != nil {
			return
		}
		text := string(p.Data)
		var s string
		if json.Unmarshal(p.Data, &s) == nil {
			text = s
		}
		if p.Logger != "" {
			text = p.Logger + ": " + text
		}
		c.hooks.Log(p.Level, text)
	}
}

// answer replies to a request the server sent.
func (c *Client) answer(m message) {
	ctx := context.Background()
	result, rerr := c.serve(ctx, m)
	resp := message{JSONRPC: "2.0", ID: m.ID}
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = mustJSON(result)
	}
	_ = c.t.Send(ctx, mustJSON(resp))
}

func (c *Client) serve(ctx context.Context, m message) (any, *rpcError) {
	switch m.Method {
	case "ping":
		return struct{}{}, nil
	case "roots/list":
		roots := []Root{}
		if c.hooks.Roots != nil {
			roots = append(roots, c.hooks.Roots()...)
		}
		return map[string]any{"roots": roots}, nil
	case "sampling/createMessage":
		if c.hooks.Sample == nil {
			return nil, &rpcError{Code: codeInvalidRequest, Message: "This server is not allowed to use the AI. Turn it on in Toskar's Tools page."}
		}
		var p createMessageParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		var msgs []SampleMessage
		for _, pm := range p.Messages {
			var content Content
			if json.Unmarshal(pm.Content, &content) != nil || content.Type != "text" {
				continue
			}
			msgs = append(msgs, SampleMessage{Role: pm.Role, Text: content.Text})
		}
		if len(msgs) == 0 {
			return nil, &rpcError{Code: codeInvalidParams, Message: "Toskar can only answer text messages."}
		}
		text, model, err := c.hooks.Sample(ctx, p.SystemPrompt, msgs, p.MaxTokens)
		if err != nil {
			return nil, &rpcError{Code: codeInternal, Message: err.Error()}
		}
		return map[string]any{
			"role": "assistant", "model": model, "stopReason": "endTurn",
			"content": map[string]any{"type": "text", "text": text},
		}, nil
	case "elicitation/create":
		// Yggdrasil cannot show a server's own form mid-call yet.
		return map[string]any{"action": "decline"}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "Toskar does not support " + m.Method}
}

// contentText is the readable text of one content item.
func contentText(ct Content) string {
	switch ct.Type {
	case "text":
		return ct.Text
	case "resource":
		if ct.Resource != nil && ct.Resource.Text != "" {
			return ct.Resource.Text
		}
	}
	return ""
}
