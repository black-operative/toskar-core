package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthRequiredError means the server wants a sign-in. ResourceMetadata is
// where it says to start, when it says.
type AuthRequiredError struct {
	ResourceMetadata string
	Scope            string
}

func (e *AuthRequiredError) Error() string { return "this tool source needs you to sign in" }

// ErrSessionExpired means the server forgot the session; connect again.
var ErrSessionExpired = errors.New("the tool source ended the session")

// errLegacy means the URL is an older HTTP+SSE server.
var errLegacy = errors.New("legacy sse server")

// HTTPOptions configure a remote server connection.
type HTTPOptions struct {
	Client  *http.Client
	Headers map[string]string
	// Token returns a bearer token, refreshed if needed. Nil sends none.
	Token func(ctx context.Context) (string, error)
}

func (o HTTPOptions) apply(ctx context.Context, req *http.Request) error {
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}
	if o.Token != nil {
		tok, err := o.Token(ctx)
		if err != nil {
			return err
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	return nil
}

func (o HTTPOptions) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return http.DefaultClient
}

// DialHTTP connects to a remote server: Streamable HTTP first, then the
// older HTTP+SSE transport for servers that only speak that.
func DialHTTP(ctx context.Context, rawURL string, opts HTTPOptions, hooks Hooks, clientVersion string) (*Client, error) {
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return nil, fmt.Errorf("%q is not a web address", rawURL)
	}
	if !strings.HasSuffix(strings.TrimRight(rawURL, "/"), "/sse") {
		c, err := Connect(ctx, newStreamable(rawURL, opts), hooks, clientVersion)
		if !errors.Is(err, errLegacy) {
			return c, err
		}
	}
	t, err := dialSSE(ctx, rawURL, opts)
	if err != nil {
		return nil, err
	}
	return Connect(ctx, t, hooks, clientVersion)
}

// streamable is the Streamable HTTP transport: each message is a POST,
// and the reply comes back as JSON or as an event stream.
type streamable struct {
	url      string
	opts     HTTPOptions
	incoming chan []byte
	mu       sync.Mutex
	session  string
	version  string
	err      error
	closed   chan struct{}
	once     sync.Once
	readers  sync.WaitGroup
	sent     bool
}

func newStreamable(u string, opts HTTPOptions) *streamable {
	return &streamable{url: u, opts: opts, incoming: make(chan []byte, 16), closed: make(chan struct{})}
}

func (t *streamable) SetProtocolVersion(v string) {
	t.mu.Lock()
	t.version = v
	t.mu.Unlock()
}

func (t *streamable) Send(ctx context.Context, msg []byte) error {
	select {
	case <-t.closed:
		return ErrClosed
	default:
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	t.mu.Lock()
	first := !t.sent
	t.sent = true
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	if t.version != "" {
		req.Header.Set("MCP-Protocol-Version", t.version)
	}
	t.mu.Unlock()
	if err := t.opts.apply(ctx, req); err != nil {
		return err
	}
	resp, err := t.opts.client().Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the tool source: %w", err)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.session = sid
		t.mu.Unlock()
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden && strings.Contains(resp.Header.Get("WWW-Authenticate"), "insufficient_scope"):
		resp.Body.Close()
		return authErr(resp)
	case resp.StatusCode == http.StatusNotFound && !first && t.hasSession():
		resp.Body.Close()
		t.fail(ErrSessionExpired)
		return ErrSessionExpired
	case first && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusBadRequest && looksLikeSSE(resp)):
		resp.Body.Close()
		return errLegacy
	case resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent:
		resp.Body.Close()
		return nil
	case resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return httpErr(resp.StatusCode, body)
	}
	ctype := resp.Header.Get("Content-Type")
	t.readers.Add(1)
	go func() {
		defer t.readers.Done()
		defer resp.Body.Close()
		if strings.HasPrefix(ctype, "text/event-stream") {
			readEvents(resp.Body, func(event, data string) bool {
				if event == "" || event == "message" {
					return t.push([]byte(data))
				}
				return true
			})
			return
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err == nil && len(bytes.TrimSpace(body)) > 0 {
			t.push(bytes.TrimSpace(body))
		}
	}()
	return nil
}

func looksLikeSSE(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "event-stream")
}

func (t *streamable) hasSession() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session != ""
}

func (t *streamable) push(msg []byte) bool {
	select {
	case <-t.closed:
		return false
	case t.incoming <- msg:
		return true
	}
}

func (t *streamable) Incoming() <-chan []byte { return t.incoming }

func (t *streamable) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *streamable) fail(err error) {
	t.mu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.mu.Unlock()
	t.Close()
}

func (t *streamable) Close() error {
	t.once.Do(func() {
		close(t.closed)
		t.mu.Lock()
		session := t.session
		t.mu.Unlock()
		if session != "" {
			// Tell the server the session is over; it may ignore this.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil); err == nil {
				req.Header.Set("Mcp-Session-Id", session)
				if t.opts.apply(ctx, req) == nil {
					if resp, err := t.opts.client().Do(req); err == nil {
						resp.Body.Close()
					}
				}
			}
			cancel()
		}
		go func() {
			t.readers.Wait()
			close(t.incoming)
		}()
	})
	return nil
}

// sseTransport is the 2024-11-05 HTTP+SSE transport: one long GET carries
// every reply, and messages are POSTed to the address it names first.
type sseTransport struct {
	opts     HTTPOptions
	endpoint string
	incoming chan []byte
	body     io.Closer
	cancel   context.CancelFunc
	err      error
	mu       sync.Mutex
}

func dialSSE(ctx context.Context, rawURL string, opts HTTPOptions) (*sseTransport, error) {
	streamCtx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if err := opts.apply(ctx, req); err != nil {
		cancel()
		return nil, err
	}
	// The stream lives past ctx, so time the first response separately.
	type result struct {
		resp *http.Response
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := opts.client().Do(req)
		got <- result{resp, err}
	}()
	var resp *http.Response
	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case r := <-got:
		if r.err != nil {
			cancel()
			return nil, fmt.Errorf("could not reach the tool source: %w", r.err)
		}
		resp = r.resp
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		cancel()
		return nil, authErr(resp)
	}
	if resp.StatusCode >= 300 || !looksLikeSSE(resp) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		cancel()
		if resp.StatusCode < 300 {
			return nil, errors.New("that address did not answer like an MCP server. Check the address in the server's instructions")
		}
		return nil, httpErr(resp.StatusCode, body)
	}
	t := &sseTransport{opts: opts, incoming: make(chan []byte, 16), body: resp.Body, cancel: cancel}
	endpoint := make(chan string, 1)
	go func() {
		defer close(t.incoming)
		readEvents(resp.Body, func(event, data string) bool {
			switch event {
			case "endpoint":
				select {
				case endpoint <- data:
				default:
				}
			case "", "message":
				select {
				case t.incoming <- []byte(data):
				case <-streamCtx.Done():
					return false
				}
			}
			return true
		})
		t.mu.Lock()
		if t.err == nil {
			t.err = ErrClosed
		}
		t.mu.Unlock()
	}()
	select {
	case e := <-endpoint:
		base, _ := url.Parse(rawURL)
		ref, err := url.Parse(e)
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("the tool source named an address Toskar could not read")
		}
		t.endpoint = base.ResolveReference(ref).String()
		return t, nil
	case <-ctx.Done():
		t.Close()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		t.Close()
		return nil, errors.New("the tool source did not finish connecting")
	}
}

func (t *sseTransport) Send(ctx context.Context, msg []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := t.opts.apply(ctx, req); err != nil {
		return err
	}
	resp, err := t.opts.client().Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the tool source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return authErr(resp)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return httpErr(resp.StatusCode, body)
	}
	return nil
}

func (t *sseTransport) Incoming() <-chan []byte { return t.incoming }

func (t *sseTransport) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *sseTransport) Close() error {
	t.cancel()
	return t.body.Close()
}

// readEvents parses a server-sent event stream and calls fn for each
// event until fn returns false or the stream ends.
func readEvents(r io.Reader, fn func(event, data string) bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 32<<20)
	var event string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(data) > 0 {
				if !fn(event, strings.Join(data, "\n")) {
					return
				}
			}
			event, data = "", nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if len(data) > 0 {
		fn(event, strings.Join(data, "\n"))
	}
}

func authErr(resp *http.Response) error {
	e := &AuthRequiredError{}
	for _, h := range resp.Header.Values("WWW-Authenticate") {
		params := authParams(h)
		if v := params["resource_metadata"]; v != "" {
			e.ResourceMetadata = v
		}
		if v := params["scope"]; v != "" {
			e.Scope = v
		}
	}
	return e
}

// authParams reads the key="value" pairs of a WWW-Authenticate header.
func authParams(h string) map[string]string {
	out := map[string]string{}
	_, rest, ok := strings.Cut(h, " ")
	if !ok {
		return out
	}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ,")
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		var val string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				val, rest = after[1:], ""
			} else {
				val, rest = after[1:end+1], after[end+2:]
			}
		} else {
			val, rest, _ = strings.Cut(after, ",")
		}
		out[key] = strings.TrimSpace(val)
	}
	return out
}

func httpErr(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	switch status {
	case http.StatusNotFound:
		return errors.New("nothing answered at that address (404). Check the address in the server's instructions")
	case http.StatusTooManyRequests:
		return errors.New("the tool source is busy (too many requests). Try again in a minute")
	}
	if msg == "" || strings.HasPrefix(msg, "<") {
		return fmt.Errorf("the tool source answered with HTTP %d", status)
	}
	return fmt.Errorf("the tool source answered with HTTP %d: %s", status, msg)
}
