package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Backend is what Yggdrasil offers other apps through its MCP server.
type Backend struct {
	// Ask answers a question with the local AI. model may be empty or
	// "auto" to let Yggdrasil choose.
	Ask func(ctx context.Context, prompt, model string) (string, error)
	// Models lists the AIs a caller may choose from.
	Models func(ctx context.Context) ([]ModelInfo, error)
	// Search finds passages in the person's connected knowledge. Nil when
	// knowledge is not available.
	Search func(ctx context.Context, query string, limit int) ([]Passage, error)
	// Authorize checks the caller may use the API, as /v1 does, and
	// returns the context the calls run in, carrying what its key allows.
	Authorize func(r *http.Request) (context.Context, error)
	Version   string
}

// ModelInfo is one AI a caller may ask.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Passage is one search result from connected knowledge.
type Passage struct {
	Source string `json:"source"`
	Title  string `json:"title,omitempty"`
	Text   string `json:"text"`
}

// Server is Yggdrasil's MCP endpoint over Streamable HTTP. It keeps no
// session: every request stands alone, which the protocol allows.
type Server struct{ b Backend }

// NewServer returns the MCP endpoint for b.
func NewServer(b Backend) *Server { return &Server{b: b} }

// ServeHTTP handles POST (messages), GET (no server stream: 405), and
// DELETE (session end: nothing to do).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A web page must not reach the local AI through the visitor's
	// browser (DNS rebinding): only Yggdrasil's own pages may send an
	// Origin. Apps such as Claude Desktop and Cursor send none.
	if !allowedOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	if s.b.Authorize != nil {
		var err error
		if ctx, err = s.b.Authorize(r); err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="toskar"`)
			http.Error(w, "authorization required: create an API key on the API Access page", http.StatusUnauthorized)
			return
		}
	}
	switch r.Method {
	case http.MethodPost:
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "this endpoint takes POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse(nil, nil, &rpcError{Code: codeParseError, Message: "could not read the request"}))
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var batch []message
		if err := json.Unmarshal(body, &batch); err != nil {
			writeRPC(w, http.StatusBadRequest, rpcResponse(nil, nil, &rpcError{Code: codeParseError, Message: "invalid JSON"}))
			return
		}
		var out []message
		for _, m := range batch {
			if resp, ok := s.handle(ctx, m); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPC(w, http.StatusOK, out)
		return
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse(nil, nil, &rpcError{Code: codeParseError, Message: "invalid JSON"}))
		return
	}
	resp, ok := s.handle(ctx, m)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, http.StatusOK, resp)
}

func allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return origin == ""
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	// Yggdrasil's own UI, opened from another computer on the network.
	reqHost, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		reqHost = r.Host
	}
	return strings.EqualFold(host, reqHost)
}

func writeRPC(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func rpcResponse(id json.RawMessage, result any, rerr *rpcError) message {
	m := message{JSONRPC: "2.0", ID: id}
	if m.ID == nil {
		m.ID = json.RawMessage("null")
	}
	if rerr != nil {
		m.Error = rerr
	} else {
		m.Result = mustJSON(result)
	}
	return m
}

// handle answers one message. Notifications and responses get no reply.
func (s *Server) handle(ctx context.Context, m message) (message, bool) {
	if len(m.ID) == 0 || m.Method == "" {
		return message{}, false
	}
	result, rerr := s.dispatch(ctx, m)
	return rpcResponse(m.ID, result, rerr), true
}

func (s *Server) dispatch(ctx context.Context, m message) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		var p initializeParams
		_ = json.Unmarshal(m.Params, &p)
		version := ProtocolVersion
		if supportedVersions[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      implementation{Name: "toskar", Title: "Toskar", Version: s.b.Version},
			"instructions": "Toskar runs AI models on the user's own computers. Use ask_local_ai to get an answer " +
				"from a local model, for private data or when the user asks for a local AI. Use search_my_knowledge " +
				"to find passages in documents the user connected to Toskar.",
		}, nil
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params"}
		}
		text, err := s.call(ctx, p.Name, p.Arguments)
		if err != nil {
			if rerr, ok := err.(*rpcError); ok {
				return nil, rerr
			}
			return toolResult(err.Error(), true), nil
		}
		return toolResult(text, false), nil
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + m.Method}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isError}
}

func (s *Server) tools() []map[string]any {
	readOnly := map[string]any{"readOnlyHint": true, "openWorldHint": false}
	out := []map[string]any{
		{
			"name":  "ask_local_ai",
			"title": "Ask the local AI",
			"description": "Ask an AI model running on the user's own computer, through Toskar. " +
				"The question and answer stay on the user's computers. It may read the web or the user's connected " +
				"knowledge to answer, but it does not change anything.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{"type": "string", "description": "The question or task, with any context it needs."},
					"model":  map[string]any{"type": "string", "description": "Optional. An id from list_local_models; leave out to let Toskar choose."},
				},
				"required": []string{"prompt"},
			},
			"annotations": readOnly,
		},
		{
			"name":        "list_local_models",
			"title":       "List local AI models",
			"description": "List the AI models Toskar can answer with, including specialized AIs the user trained.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			"annotations": readOnly,
		},
	}
	if s.b.Search != nil {
		out = append(out, map[string]any{
			"name":        "search_my_knowledge",
			"title":       "Search my knowledge",
			"description": "Search documents, notes, and pages the user connected to Toskar, and return the passages that match.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "What to look for."},
					"limit": map[string]any{"type": "integer", "description": "How many passages, up to 20. Default 5."},
				},
				"required": []string{"query"},
			},
			"annotations": readOnly,
		})
	}
	return out
}

func (s *Server) call(ctx context.Context, name string, args map[string]any) (string, error) {
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	switch name {
	case "ask_local_ai":
		prompt := str("prompt")
		if prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}
		if s.b.Ask == nil {
			return "", fmt.Errorf("the local AI is not available")
		}
		return s.b.Ask(ctx, prompt, str("model"))
	case "list_local_models":
		if s.b.Models == nil {
			return "", fmt.Errorf("the model list is not available")
		}
		list, err := s.b.Models(ctx)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "No models are installed yet. Install one on Toskar's Models page.", nil
		}
		var b strings.Builder
		for _, m := range list {
			b.WriteString("- " + m.ID)
			if m.Name != "" && m.Name != m.ID {
				b.WriteString(" (" + m.Name + ")")
			}
			if m.Description != "" {
				b.WriteString(": " + m.Description)
			}
			b.WriteString("\n")
		}
		return strings.TrimSpace(b.String()), nil
	case "search_my_knowledge":
		if s.b.Search == nil {
			return "", &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + name}
		}
		query := str("query")
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		limit := 5
		if n, ok := args["limit"].(float64); ok && n > 0 {
			limit = min(int(n), 20)
		}
		hits, err := s.b.Search(ctx, query, limit)
		if err != nil {
			return "", err
		}
		if len(hits) == 0 {
			return "Nothing in the connected knowledge matches that.", nil
		}
		var b strings.Builder
		for i, h := range hits {
			fmt.Fprintf(&b, "[%d] %s", i+1, h.Source)
			if h.Title != "" {
				b.WriteString(" — " + h.Title)
			}
			b.WriteString("\n" + strings.TrimSpace(h.Text) + "\n\n")
		}
		return strings.TrimSpace(b.String()), nil
	}
	return "", &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + name}
}
