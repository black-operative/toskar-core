package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Statuses of a tool source.
const (
	StatusReady  = "ready"
	StatusSignIn = "sign_in"
	StatusError  = "error"
	StatusOff    = "off"
)

// SecretStore keeps credentials outside SQLite. *auth.SecretStore
// satisfies it.
type SecretStore interface {
	Write(name, value string) error
	Read(name string) (string, error)
	Delete(name string) error
}

// Registry is the tool registry a source's tools are added to.
type Registry interface {
	Register(t tools.Tool)
	Unregister(id string)
	IsDisabled(id string) bool
}

// Sampler answers a server's request for an AI reply with a local model.
type Sampler func(ctx context.Context, system string, messages []SampleMessage, maxTokens int) (text, model string, err error)

// ErrUnknown is returned for a tool source id that does not exist.
var ErrUnknown = contracts.NewError("MCP_NOT_FOUND", nil, errors.New("no such tool source"))

// Manager adds, runs, and removes tool sources.
type Manager struct {
	db       *sql.DB
	secrets  SecretStore
	registry Registry
	http     *http.Client
	version  string
	logger   *slog.Logger
	now      func() time.Time
	// IdleAfter is how long a source on this computer runs unused before
	// it is stopped. It starts again when a tool is needed.
	IdleAfter time.Duration
	// ConnectTimeout bounds adding or checking a source. The first start of
	// an npx or uvx server downloads it, which can take a while.
	ConnectTimeout time.Duration
	// Sample, when set, lets sources that are allowed use the AI.
	Sample Sampler

	mu      sync.Mutex
	servers map[string]*server
	signins map[string]*pendingSignIn
}

type server struct {
	id       string
	spec     Spec
	enabled  bool
	sampling bool
	always   bool
	policies map[string]string
	tools    []RemoteTool
	info     serverInfo
	status   string
	errText  string
	added    time.Time
	checked  time.Time
	logs     *logRing

	// connMu serializes starting the source.
	connMu   sync.Mutex
	client   *Client
	lastUsed time.Time
}

type serverInfo struct {
	Name         string `json:"name,omitempty"`
	Version      string `json:"version,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Resources    bool   `json:"resources,omitempty"`
	Prompts      bool   `json:"prompts,omitempty"`
}

type pendingSignIn struct {
	serverID string
	verifier string
	oauth    OAuth
	expires  time.Time
}

// NewManager returns a manager. clientVersion is Yggdrasil's version, sent
// to servers when connecting.
func NewManager(db *sql.DB, secrets SecretStore, registry Registry, clientVersion string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		db: db, secrets: secrets, registry: registry, version: clientVersion, logger: logger,
		http: &http.Client{Timeout: 0}, now: time.Now,
		IdleAfter: 10 * time.Minute, ConnectTimeout: 3 * time.Minute,
		servers: map[string]*server{}, signins: map[string]*pendingSignIn{},
	}
}

// SetHTTPClient replaces the HTTP client, for tests.
func (m *Manager) SetHTTPClient(c *http.Client) { m.http = c }

func secretName(id string) string { return "mcp-" + id + ".json" }

// Load registers the tools of every enabled source, from the list each
// gave when last checked. No source is started.
func (m *Manager) Load(ctx context.Context) error {
	rows, err := m.db.QueryContext(ctx, `SELECT id, spec, enabled, allow_sampling, always_offer, COALESCE(policies, ''), COALESCE(tools, ''), COALESCE(info, ''), status, COALESCE(error, ''), added_at, COALESCE(checked_at, '') FROM mcp_servers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	for rows.Next() {
		var s server
		var spec, policies, toolsJSON, info, added, checked string
		var enabled, sampling, always int
		if err := rows.Scan(&s.id, &spec, &enabled, &sampling, &always, &policies, &toolsJSON, &info, &s.status, &s.errText, &added, &checked); err != nil {
			return err
		}
		if json.Unmarshal([]byte(spec), &s.spec) != nil {
			continue
		}
		s.enabled, s.sampling, s.always = enabled == 1, sampling == 1, always == 1
		s.policies = map[string]string{}
		_ = json.Unmarshal([]byte(policies), &s.policies)
		_ = json.Unmarshal([]byte(toolsJSON), &s.tools)
		_ = json.Unmarshal([]byte(info), &s.info)
		s.added, _ = time.Parse(time.RFC3339Nano, added)
		s.checked, _ = time.Parse(time.RFC3339Nano, checked)
		s.logs = &logRing{}
		srv := &s
		m.servers[s.id] = srv
		if srv.enabled {
			m.registerLocked(srv)
		}
	}
	return rows.Err()
}

// Run stops idle sources until ctx ends, then stops them all.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			m.closeAll()
			return
		case <-tick.C:
			m.stopIdle()
		}
	}
}

func (m *Manager) stopIdle() {
	m.mu.Lock()
	var idle []*Client
	for _, s := range m.servers {
		if s.client != nil && m.now().Sub(s.lastUsed) > m.IdleAfter {
			idle = append(idle, s.client)
			s.client = nil
			s.logs.add("info", "Stopped after sitting idle. It starts again when a tool is needed.")
		}
	}
	m.mu.Unlock()
	for _, c := range idle {
		c.Close()
	}
}

func (m *Manager) closeAll() {
	m.mu.Lock()
	var all []*Client
	for _, s := range m.servers {
		if s.client != nil {
			all = append(all, s.client)
			s.client = nil
		}
	}
	m.mu.Unlock()
	for _, c := range all {
		c.Close()
	}
}

// AddRequest is one way to add a source: a gallery entry with answers, a
// spec (from pasted text or the advanced form), or a server another app
// on this computer has.
type AddRequest struct {
	Preset string            `json:"preset,omitempty"`
	Spec   *Spec             `json:"spec,omitempty"`
	Import *ImportRef        `json:"import,omitempty"`
	Values map[string]string `json:"values,omitempty"`
	// RedirectBase is the address the browser reaches Yggdrasil at, for a
	// sign-in that has to come back to it.
	RedirectBase string `json:"redirect_base,omitempty"`
}

// ImportRef names a server in another app's settings.
type ImportRef struct {
	App  string `json:"app"`
	Name string `json:"name"`
}

// Added is the result of adding a source.
type Added struct {
	Server View `json:"server"`
	// SignInURL is set when the service needs you to sign in first: open
	// it in the browser.
	SignInURL string `json:"sign_in_url,omitempty"`
}

// specFor builds the spec an AddRequest describes.
func (m *Manager) specFor(req AddRequest) (Spec, error) {
	switch {
	case req.Preset != "":
		p, ok := presetByID(req.Preset)
		if !ok {
			return Spec{}, fmt.Errorf("%w %q", ErrUnknown, req.Preset)
		}
		return p.Spec(req.Values)
	case req.Import != nil:
		for _, f := range Discover() {
			if f.App == req.Import.App && f.Spec.Name == req.Import.Name {
				return f.Spec.Fill(req.Values), nil
			}
		}
		return Spec{}, fmt.Errorf("%s is no longer in that app's settings", req.Import.Name)
	case req.Spec != nil:
		return req.Spec.Fill(req.Values), nil
	}
	return Spec{}, errors.New("choose a gallery entry, paste a server's settings, or fill in the form")
}

// Add checks a source by connecting to it and listing its tools, then
// stores it and adds the tools. Nothing is stored when it cannot connect,
// except a service that needs you to sign in: that is stored, and the
// result says where to sign in.
func (m *Manager) Add(ctx context.Context, req AddRequest) (Added, error) {
	spec, err := m.specFor(req)
	if err != nil {
		return Added{}, err
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	if spec.Headers == nil {
		spec.Headers = map[string]string{}
	}
	if err := spec.Validate(); err != nil {
		return Added{}, err
	}
	if needs := spec.Needs(); len(needs) > 0 {
		var labels []string
		for _, n := range needs {
			labels = append(labels, n.Label)
		}
		return Added{}, fmt.Errorf("still needed: %s", strings.Join(labels, ", "))
	}
	stored, sec := split(spec)

	m.mu.Lock()
	id := m.uniqueIDLocked(spec)
	srv := &server{id: id, spec: stored, enabled: true, policies: map[string]string{}, added: m.now().UTC(), logs: &logRing{}}
	m.mu.Unlock()

	if err := m.writeSecrets(id, sec); err != nil {
		return Added{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, m.ConnectTimeout)
	defer cancel()
	client, err := m.dial(cctx, srv)
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) && len(sec.Headers) == 0 {
		// Sign in first; keep the source so the sign-in can finish it.
		srv.status = StatusSignIn
		m.mu.Lock()
		m.servers[id] = srv
		err = m.saveLocked(ctx, srv)
		m.mu.Unlock()
		if err != nil {
			return Added{}, err
		}
		signIn, err := m.startSignIn(ctx, srv, req.RedirectBase, authErr)
		if err != nil {
			return Added{Server: m.view(srv)}, err
		}
		return Added{Server: m.view(srv), SignInURL: signIn}, nil
	}
	if err != nil {
		_ = m.secrets.Delete(secretName(id))
		return Added{}, m.explain(srv, err)
	}
	if err := m.adopt(ctx, srv, client); err != nil {
		client.Close()
		_ = m.secrets.Delete(secretName(id))
		return Added{}, err
	}
	m.mu.Lock()
	m.servers[id] = srv
	err = m.saveLocked(ctx, srv)
	m.registerLocked(srv)
	m.mu.Unlock()
	if err != nil {
		return Added{}, err
	}
	srv.logs.add("info", fmt.Sprintf("Added with %d tools.", len(srv.tools)))
	return Added{Server: m.view(srv)}, nil
}

// adopt lists a fresh connection's tools and makes it the live one.
func (m *Manager) adopt(ctx context.Context, srv *server, c *Client) error {
	var list []RemoteTool
	if c.HasTools {
		var err error
		if list, err = c.ListTools(ctx); err != nil {
			return fmt.Errorf("connected, but could not list its tools: %w", err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := srv.client
	srv.client, srv.lastUsed = c, m.now()
	srv.tools = list
	srv.info = serverInfo{Name: c.Server.Name, Version: c.Server.Version, Protocol: c.Version,
		Instructions: truncate(c.Instructions, 2000), Resources: c.HasResources, Prompts: c.HasPrompts}
	srv.status, srv.errText, srv.checked = StatusReady, "", m.now().UTC()
	if old != nil && old != c {
		go old.Close()
	}
	go m.watch(srv, c)
	return nil
}

// watch notes when a running source stops on its own.
func (m *Manager) watch(srv *server, c *Client) {
	<-c.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	if srv.client != c {
		return
	}
	srv.client = nil
	if err := c.Err(); err != nil && !errors.Is(err, ErrClosed) {
		srv.logs.add("error", "Stopped: "+err.Error())
	}
}

// Update is a change to a source's settings. Nil fields stay as they are.
type Update struct {
	Name          *string           `json:"name,omitempty"`
	Enabled       *bool             `json:"enabled,omitempty"`
	AllowSampling *bool             `json:"allow_sampling,omitempty"`
	AlwaysOffer   *bool             `json:"always_offer,omitempty"`
	Keywords      *[]string         `json:"keywords,omitempty"`
	Policies      map[string]string `json:"policies,omitempty"`
}

// Update changes settings that need no new connection.
func (m *Manager) Update(ctx context.Context, id string, u Update) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[id]
	if !ok {
		return View{}, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
		srv.spec.Name = strings.TrimSpace(*u.Name)
	}
	if u.AllowSampling != nil {
		srv.sampling = *u.AllowSampling
		if srv.client != nil {
			// The capability is given when connecting.
			go srv.client.Close()
			srv.client = nil
		}
	}
	if u.AlwaysOffer != nil {
		srv.always = *u.AlwaysOffer
	}
	if u.Keywords != nil {
		srv.spec.Keywords = cleanKeywords(*u.Keywords)
	}
	for name, p := range u.Policies {
		switch p {
		case tools.PolicyAllow, tools.PolicyAsk:
			srv.policies[name] = p
		case "", "default":
			delete(srv.policies, name)
		default:
			return View{}, fmt.Errorf("policy must be allow or ask")
		}
	}
	if u.Enabled != nil && *u.Enabled != srv.enabled {
		srv.enabled = *u.Enabled
		if !srv.enabled && srv.client != nil {
			go srv.client.Close()
			srv.client = nil
		}
	}
	if err := m.saveLocked(ctx, srv); err != nil {
		return View{}, err
	}
	if srv.enabled {
		m.registerLocked(srv)
	} else {
		m.unregisterLocked(srv)
	}
	return m.viewLocked(srv), nil
}

func cleanKeywords(in []string) []string {
	var out []string
	for _, k := range in {
		if k = strings.TrimSpace(k); k != "" && len(out) < 50 {
			out = append(out, k)
		}
	}
	return out
}

// Replace changes how a source is reached, then checks it. Secret values
// left blank keep the stored ones.
func (m *Manager) Replace(ctx context.Context, id string, spec Spec, values map[string]string) (View, error) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return View{}, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	old, _ := m.readSecrets(id)
	spec = spec.Fill(values)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	if spec.Headers == nil {
		spec.Headers = map[string]string{}
	}
	for k, v := range spec.Env {
		if v == "" && old.Env[k] != "" {
			spec.Env[k] = old.Env[k]
		}
	}
	for k, v := range spec.Headers {
		if v == "" && old.Headers[k] != "" {
			spec.Headers[k] = old.Headers[k]
		}
	}
	for i, a := range spec.Args {
		if a == "" && old.Args[i] != "" {
			spec.Args[i] = old.Args[i]
		}
	}
	if spec.ClientSecret == "" {
		spec.ClientSecret = old.ClientSecret
	}
	spec.Preset = srv.spec.Preset
	if err := spec.Validate(); err != nil {
		return View{}, err
	}
	stored, sec := split(spec)
	if stored.URL == srv.spec.URL {
		sec.OAuth = old.OAuth
	}
	if err := m.writeSecrets(id, sec); err != nil {
		return View{}, err
	}
	m.mu.Lock()
	srv.spec = stored
	if srv.client != nil {
		go srv.client.Close()
		srv.client = nil
	}
	err := m.saveLocked(ctx, srv)
	m.mu.Unlock()
	if err != nil {
		return View{}, err
	}
	return m.Check(ctx, id)
}

// Remove stops a source, removes its tools, and deletes what was stored.
func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	srv, ok := m.servers[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("%w %q", ErrUnknown, id)
	}
	delete(m.servers, id)
	m.unregisterLocked(srv)
	c := srv.client
	srv.client = nil
	m.mu.Unlock()
	if c != nil {
		c.Close()
	}
	if err := m.secrets.Delete(secretName(id)); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, id)
	return err
}

// Check connects again, lists the tools, and records the result.
func (m *Manager) Check(ctx context.Context, id string) (View, error) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return View{}, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	srv.connMu.Lock()
	defer srv.connMu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, m.ConnectTimeout)
	defer cancel()
	c, err := m.dial(cctx, srv)
	if err == nil {
		err = m.adopt(cctx, srv, c)
		if err != nil {
			c.Close()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		var auth *AuthRequiredError
		if errors.As(err, &auth) {
			srv.status, srv.errText = StatusSignIn, ""
		} else {
			srv.status, srv.errText = StatusError, m.explain(srv, err).Error()
		}
		srv.checked = m.now().UTC()
		srv.logs.add("error", "Check failed: "+srv.errText)
	} else {
		srv.logs.add("info", fmt.Sprintf("Checked: %d tools.", len(srv.tools)))
	}
	if serr := m.saveLocked(ctx, srv); serr != nil {
		return View{}, serr
	}
	if srv.enabled {
		m.registerLocked(srv)
	}
	return m.viewLocked(srv), nil
}

// live returns a running connection, starting the source if it is not.
func (m *Manager) live(ctx context.Context, srv *server) (*Client, error) {
	m.mu.Lock()
	c := srv.client
	if c != nil {
		srv.lastUsed = m.now()
	}
	m.mu.Unlock()
	if c != nil {
		return c, nil
	}
	srv.connMu.Lock()
	defer srv.connMu.Unlock()
	m.mu.Lock()
	c = srv.client
	m.mu.Unlock()
	if c != nil {
		return c, nil
	}
	c, err := m.dial(ctx, srv)
	if err != nil {
		var auth *AuthRequiredError
		m.mu.Lock()
		if errors.As(err, &auth) {
			srv.status = StatusSignIn
			_ = m.saveLocked(context.WithoutCancel(ctx), srv)
		}
		srv.logs.add("error", "Could not start: "+err.Error())
		m.mu.Unlock()
		if auth != nil {
			return nil, fmt.Errorf("%s needs you to sign in again. Open Tools and choose Sign in", srv.spec.Name)
		}
		return nil, m.explain(srv, err)
	}
	m.mu.Lock()
	srv.client, srv.lastUsed = c, m.now()
	srv.info.Resources, srv.info.Prompts = c.HasResources, c.HasPrompts
	srv.logs.add("info", "Started.")
	m.mu.Unlock()
	go m.watch(srv, c)
	return c, nil
}

// dial connects to a source with its secrets.
func (m *Manager) dial(ctx context.Context, srv *server) (*Client, error) {
	sec, _ := m.readSecrets(srv.id)
	spec := join(srv.spec, sec)
	hooks := Hooks{
		Roots: func() []Root { return rootsOf(spec) },
		ToolsChanged: func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			m.refreshTools(ctx, srv)
		},
		Log: func(level, text string) { srv.logs.add(level, text) },
	}
	m.mu.Lock()
	if srv.sampling && m.Sample != nil {
		sample := m.Sample
		hooks.Sample = func(ctx context.Context, system string, msgs []SampleMessage, maxTokens int) (string, string, error) {
			srv.logs.add("info", "Asked the AI for a reply.")
			return sample(ctx, system, msgs, maxTokens)
		}
	}
	m.mu.Unlock()
	if spec.Remote() {
		opts := HTTPOptions{Client: m.http, Headers: spec.Headers}
		if sec.OAuth.SignedIn() {
			opts.Token = func(ctx context.Context) (string, error) { return m.token(ctx, srv.id) }
		}
		c, err := DialHTTP(ctx, spec.URL, opts, hooks, m.version)
		var auth *AuthRequiredError
		if errors.As(err, &auth) && sec.OAuth != nil && sec.OAuth.RefreshToken != "" && m.refreshNow(ctx, srv.id) == nil {
			// The service may end a token before it said it would.
			c, err = DialHTTP(ctx, spec.URL, opts, hooks, m.version)
		}
		return c, err
	}
	args := make([]string, len(spec.Args))
	for i, a := range spec.Args {
		args[i] = expandHome(a)
	}
	t, err := StartProcess(spec.Command, args, spec.Env, expandHome(spec.Dir), func(line string) { srv.logs.add("output", line) })
	if err != nil {
		return nil, err
	}
	return Connect(ctx, t, hooks, m.version)
}

// rootsOf are the folders a source on this computer was given, such as
// the folders entry's choices. Servers that support roots use them as the
// boundary of what they may touch.
func rootsOf(spec Spec) []Root {
	var out []Root
	for _, a := range spec.Args {
		p := expandHome(a)
		if !filepath.IsAbs(p) {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			out = append(out, Root{URI: (&url.URL{Scheme: "file", Path: p}).String(), Name: p})
		}
	}
	return out
}

// refreshTools lists a running source's tools again, after it said they
// changed.
func (m *Manager) refreshTools(ctx context.Context, srv *server) {
	m.mu.Lock()
	c := srv.client
	m.mu.Unlock()
	if c == nil {
		return
	}
	list, err := c.ListTools(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	srv.tools = list
	_ = m.saveLocked(ctx, srv)
	if srv.enabled {
		m.registerLocked(srv)
	}
	srv.logs.add("info", fmt.Sprintf("Tool list changed: %d tools.", len(list)))
}

// explain turns a connection failure into what to do about it.
func (m *Manager) explain(srv *server, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		if srv.spec.Remote() {
			return errors.New("the tool source did not answer in time. Check the address and your connection")
		}
		return errors.New("the tool source did not start in time. The first start downloads it; try again, and check the log if it still fails")
	}
	var auth *AuthRequiredError
	if errors.As(err, &auth) {
		return errors.New("the token or key was not accepted. Check it, and that it has the access the service's instructions say")
	}
	sec, _ := m.readSecrets(srv.id)
	return errors.New(redact(err.Error(), sec.values()))
}

// Call runs one of a source's tools.
func (m *Manager) Call(ctx context.Context, serverID, name string, args map[string]any) (map[string]any, error) {
	m.mu.Lock()
	srv, ok := m.servers[serverID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknown, serverID)
	}
	if !srv.enabled {
		return nil, fmt.Errorf("%s is turned off. Turn it on in Tools", srv.spec.Name)
	}
	var res callToolResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var c *Client
		if c, err = m.live(ctx, srv); err != nil {
			return nil, err
		}
		res, err = c.CallTool(ctx, name, args)
		// A remote server that forgot the session, or a source that had
		// stopped before the call reached it, is started once more.
		if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrClosed) && attempt == 0 {
			m.mu.Lock()
			if srv.client == c {
				srv.client = nil
			}
			m.mu.Unlock()
			continue
		}
		break
	}
	m.mu.Lock()
	srv.lastUsed = m.now()
	m.mu.Unlock()
	sec, _ := m.readSecrets(serverID)
	secrets := sec.values()
	if err != nil {
		var rpc *rpcError
		if errors.As(err, &rpc) {
			srv.logs.add("error", name+": "+redact(rpc.Message, secrets))
		}
		return nil, errors.New(redact(err.Error(), secrets))
	}
	out, err := resultMap(res)
	if err != nil {
		return nil, errors.New(redact(err.Error(), secrets))
	}
	return scrub(out, secrets).(map[string]any), nil
}

// Resources lists a source's resources.
func (m *Manager) Resources(ctx context.Context, id string) ([]Resource, error) {
	c, err := m.liveByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !c.HasResources {
		return []Resource{}, nil
	}
	return c.ListResources(ctx)
}

// ReadResource returns a resource's text.
func (m *Manager) ReadResource(ctx context.Context, id, uri string) (map[string]any, error) {
	c, err := m.liveByID(ctx, id)
	if err != nil {
		return nil, err
	}
	contents, err := c.ReadResource(ctx, uri)
	if err != nil {
		return nil, err
	}
	var texts []string
	for _, ct := range contents {
		if ct.Text != "" {
			texts = append(texts, ct.Text)
		}
	}
	if len(texts) == 0 {
		return map[string]any{"text": "That resource is not text, so it cannot be read here."}, nil
	}
	sec, _ := m.readSecrets(id)
	return map[string]any{"uri": uri, "text": redact(truncate(strings.Join(texts, "\n\n"), maxResultText), sec.values())}, nil
}

// Prompts lists a source's ready-made prompts.
func (m *Manager) Prompts(ctx context.Context, id string) ([]Prompt, error) {
	c, err := m.liveByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !c.HasPrompts {
		return []Prompt{}, nil
	}
	return c.ListPrompts(ctx)
}

// GetPrompt fills a prompt and returns its text, to start a chat with.
func (m *Manager) GetPrompt(ctx context.Context, id, name string, args map[string]string) (string, error) {
	c, err := m.liveByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.GetPrompt(ctx, name, args)
}

func (m *Manager) liveByID(ctx context.Context, id string) (*Client, error) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	return m.live(ctx, srv)
}

// Logs returns a source's recent log lines, newest last.
func (m *Manager) Logs(id string) ([]LogLine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[id]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	return srv.logs.list(), nil
}

// definitions are a source's tools as the catalog lists them.
func (m *Manager) definitionsLocked(srv *server) []tools.Definition {
	cues := cuesFor(srv)
	defs := make([]tools.Definition, 0, len(srv.tools)+2)
	seen := map[string]bool{}
	for _, t := range srv.tools {
		id := toolID(srv.id, t.Name)
		if seen[id] {
			continue
		}
		seen[id] = true
		risk := riskOf(t)
		policy := defaultPolicy(risk)
		if p := srv.policies[t.Name]; p != "" {
			policy = p
		}
		defs = append(defs, tools.Definition{
			ID: id, Name: displayName(t), Description: truncate(strings.TrimSpace(t.Description), 600),
			Capability: srv.id, Source: "mcp:" + srv.id, Schema: compactSchema(t.InputSchema),
			DefaultPolicy: policy, Risk: risk, Cues: cues, Always: srv.always,
		})
	}
	if srv.info.Resources {
		for _, r := range resourceTools {
			id := srv.id + "." + r.name
			if seen[id] {
				continue
			}
			defs = append(defs, tools.Definition{
				ID: id, Name: r.title + " (" + srv.spec.Name + ")", Description: r.description,
				Capability: srv.id, Source: "mcp:" + srv.id, Schema: r.schema,
				DefaultPolicy: tools.PolicyAllow, Risk: tools.RiskRead, Cues: cues, Always: srv.always,
			})
		}
	}
	return defs
}

// resourceTools let the model read what a source offers as resources.
var resourceTools = []struct{ name, title, description, schema string }{
	{"list_resources", "List resources", "List the documents and data this source offers to read.", `{}`},
	{"read_resource", "Read resource", "Read one of this source's resources by its uri.", `{"uri":"string"}`},
}

// cuesFor are the words that show a message is about a source: its name,
// the gallery entry's words, and the keywords the person added.
func cuesFor(srv *server) []string {
	set := map[string]bool{}
	add := func(w string) {
		w = strings.ToLower(strings.TrimSpace(w))
		if len(w) >= 3 && !genericWords[w] {
			set[w] = true
		}
	}
	add(srv.spec.Name)
	for _, w := range strings.FieldsFunc(srv.spec.Name, func(r rune) bool { return r == ' ' || r == '-' || r == '(' || r == ')' || r == '&' }) {
		add(w)
	}
	add(srv.info.Name)
	if p, ok := presetByID(srv.spec.Preset); ok {
		for _, c := range p.cues {
			add(c)
		}
	}
	for _, k := range srv.spec.Keywords {
		add(k)
	}
	out := make([]string, 0, len(set))
	for w := range set {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

var genericWords = map[string]bool{"all": true, "tools": true, "tool": true, "server": true, "mcp": true, "the": true, "and": true, "source": true}

// registerLocked adds a source's tools to the registry and catalog,
// replacing what it had.
func (m *Manager) registerLocked(srv *server) {
	m.unregisterLocked(srv)
	defs := m.definitionsLocked(srv)
	names := map[string]string{}
	for _, t := range srv.tools {
		names[toolID(srv.id, t.Name)] = t.Name
	}
	for _, def := range defs {
		m.registry.Register(&sourceTool{m: m, serverID: srv.id, name: names[def.ID], def: def})
	}
	tools.SetConnected("mcp:"+srv.id, defs)
}

func (m *Manager) unregisterLocked(srv *server) {
	for _, def := range tools.ConnectedDefinitions() {
		if def.Source == "mcp:"+srv.id {
			m.registry.Unregister(def.ID)
		}
	}
	tools.SetConnected("mcp:"+srv.id, nil)
}

// sourceTool runs one of a source's tools through the registry.
type sourceTool struct {
	m        *Manager
	serverID string
	name     string
	def      tools.Definition
}

func (t *sourceTool) ID() string          { return t.def.ID }
func (t *sourceTool) DisplayName() string { return t.def.Name }
func (t *sourceTool) Description() string { return t.def.Description }

func (t *sourceTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	switch {
	case t.name != "":
		return t.m.Call(ctx, t.serverID, t.name, args)
	case strings.HasSuffix(t.def.ID, ".list_resources"):
		list, err := t.m.Resources(ctx, t.serverID)
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, len(list))
		for i, r := range list {
			if i == 100 {
				break
			}
			items = append(items, map[string]any{"uri": r.URI, "name": firstNonEmpty(r.Title, r.Name), "description": r.Description})
		}
		return map[string]any{"resources": items}, nil
	case strings.HasSuffix(t.def.ID, ".read_resource"):
		uri, _ := args["uri"].(string)
		if strings.TrimSpace(uri) == "" {
			return nil, errors.New("uri is required")
		}
		return t.m.ReadResource(ctx, t.serverID, uri)
	}
	return nil, fmt.Errorf("tool %q not found", t.def.ID)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// reservedIDs are tool id prefixes built-in tools and connected services
// use; a source cannot take them.
var reservedIDs = map[string]bool{
	"internet": true, "filesystem": true, "files": true, "file": true, "terminal": true, "shell": true,
	"git": true, "web": true, "github": true, "homeassistant": true, "mcp": true, "auto": true, "yggdrasil": true, "toskar": true,
}

func (m *Manager) uniqueIDLocked(spec Spec) string {
	base := spec.Preset
	if base == "" {
		base = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(spec.Name), "-"), "-")
	}
	if len(base) > 32 {
		base = strings.TrimRight(base[:32], "-")
	}
	if base == "" {
		base = "source"
	}
	if reservedIDs[base] {
		base += "-mcp"
	}
	id := base
	for n := 2; m.servers[id] != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func (m *Manager) saveLocked(ctx context.Context, srv *server) error {
	spec, _ := json.Marshal(srv.spec)
	policies, _ := json.Marshal(srv.policies)
	toolsJSON, _ := json.Marshal(srv.tools)
	info, _ := json.Marshal(srv.info)
	var checked any
	if !srv.checked.IsZero() {
		checked = srv.checked.Format(time.RFC3339Nano)
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, spec, enabled, allow_sampling, always_offer, policies, tools, info, status, error, added_at, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET spec = excluded.spec, enabled = excluded.enabled, allow_sampling = excluded.allow_sampling,
			always_offer = excluded.always_offer, policies = excluded.policies, tools = excluded.tools, info = excluded.info,
			status = excluded.status, error = excluded.error, checked_at = excluded.checked_at`,
		srv.id, string(spec), boolInt(srv.enabled), boolInt(srv.sampling), boolInt(srv.always), string(policies),
		string(toolsJSON), string(info), srv.status, nullable(srv.errText), srv.added.Format(time.RFC3339Nano), checked)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (m *Manager) readSecrets(id string) (Secrets, error) {
	sec := Secrets{Env: map[string]string{}, Headers: map[string]string{}, Args: map[int]string{}}
	raw, err := m.secrets.Read(secretName(id))
	if err != nil {
		return sec, err
	}
	if err := json.Unmarshal([]byte(raw), &sec); err != nil {
		return sec, err
	}
	if sec.Env == nil {
		sec.Env = map[string]string{}
	}
	if sec.Headers == nil {
		sec.Headers = map[string]string{}
	}
	if sec.Args == nil {
		sec.Args = map[int]string{}
	}
	return sec, nil
}

func (m *Manager) writeSecrets(id string, sec Secrets) error {
	raw, err := json.Marshal(sec)
	if err != nil {
		return err
	}
	return m.secrets.Write(secretName(id), string(raw))
}

// redact removes secret values from text.
func redact(s string, secrets []string) string {
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}

// scrub removes secret values from a result, wherever they are.
func scrub(v any, secrets []string) any {
	switch x := v.(type) {
	case string:
		return redact(x, secrets)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = scrub(val, secrets)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = scrub(val, secrets)
		}
		return out
	}
	return v
}

// LogLine is one line of a source's log.
type LogLine struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

// logRing keeps a source's recent log lines.
type logRing struct {
	mu    sync.Mutex
	lines []LogLine
}

func (r *logRing) add(level, text string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(text) > 1000 {
		text = text[:1000] + "…"
	}
	r.lines = append(r.lines, LogLine{At: time.Now().UTC(), Level: level, Text: text})
	if len(r.lines) > 300 {
		r.lines = r.lines[len(r.lines)-300:]
	}
}

func (r *logRing) list() []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LogLine{}, r.lines...)
}
