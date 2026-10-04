// Package connectors connects Yggdrasil to services such as GitHub and Home
// Assistant (spec §32). A connected service adds tools to the catalog. The
// model calls them like any tool; the connector adds the stored credential
// when the call runs, so credentials never enter model context, and the
// result is sanitized before it returns.
package connectors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Field is one value a service needs to connect, such as a token.
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	// Secret values are stored in the secrets directory and never shown again.
	Secret      bool   `json:"secret"`
	Optional    bool   `json:"optional,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Credential is the stored values for one service, keyed by Field.Key.
type Credential map[string]string

// Tool is one capability a service provides.
type Tool struct {
	Def tools.Definition
	Run func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error)
	// Learn, when set, reads words from a result of a call with no
	// arguments, such as device names, that show a message is about the
	// service. They become the service's cues for tool selection.
	Learn func(result map[string]any) []string
}

// Service is a kind of connection.
type Service interface {
	ID() string
	Name() string
	Description() string
	// Scopes explains the narrowest access to grant (§32).
	Scopes() string
	Fields() []Field
	// Check verifies a credential and returns the account it belongs to.
	Check(ctx context.Context, c *http.Client, cred Credential) (string, error)
	Tools() []Tool
}

// Secrets stores credentials outside SQLite. *auth.SecretStore satisfies it.
type Secrets interface {
	Write(name, value string) error
	Read(name string) (string, error)
	Delete(name string) error
}

// Statuses.
const (
	StatusConnected = "connected"
	StatusError     = "error"
)

// Status is what may be shown about a service: never a secret value.
type Status struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Scopes      string    `json:"scopes"`
	Fields      []Field   `json:"fields"`
	Connected   bool      `json:"connected"`
	Status      string    `json:"status,omitempty"`
	Account     string    `json:"account,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitzero"`
	CheckedAt   time.Time `json:"checked_at,omitzero"`
	Error       string    `json:"error,omitempty"`
	// Values shows non-secret fields as stored, and secret ones masked.
	Values map[string]string  `json:"values,omitempty"`
	Tools  []tools.Definition `json:"tools"`
}

// ErrUnknown is returned for a service id that does not exist.
var ErrUnknown = contracts.NewError("CONNECTOR_NOT_FOUND", nil, errors.New("unknown service"))

// Registry is the tool registry connected tools are added to.
type Registry interface {
	Register(t tools.Tool)
	Unregister(id string)
}

// Manager connects and disconnects services.
type Manager struct {
	db       *sql.DB
	secrets  Secrets
	registry Registry
	client   *http.Client
	services map[string]Service
	now      func() time.Time
	cues     map[string][]string
	mu       sync.Mutex
}

// NewManager returns a manager for services.
func NewManager(db *sql.DB, secrets Secrets, registry Registry, services ...Service) *Manager {
	m := &Manager{db: db, secrets: secrets, registry: registry, client: &http.Client{Timeout: 30 * time.Second},
		services: map[string]Service{}, cues: map[string][]string{}, now: time.Now}
	for _, s := range services {
		m.services[s.ID()] = s
	}
	return m
}

// SetClient replaces the HTTP client, for tests.
func (m *Manager) SetClient(c *http.Client) { m.client = c }

func secretName(id string) string { return "connector-" + id + ".json" }

func (m *Manager) service(id string) (Service, error) {
	s, ok := m.services[id]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknown, id)
	}
	return s, nil
}

// Load registers the tools of every service connected before a restart.
func (m *Manager) Load(ctx context.Context) error {
	rows, err := m.db.QueryContext(ctx, `SELECT service_id, COALESCE(cues, '') FROM connections`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id, cues string
		if err := rows.Scan(&id, &cues); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		var list []string
		if json.Unmarshal([]byte(cues), &list) == nil {
			m.cues[id] = list
		}
	}
	rows.Close()
	for _, id := range ids {
		if s, ok := m.services[id]; ok {
			m.register(s)
		}
	}
	return rows.Err()
}

// Connect checks the values against the service, then stores them and adds
// the service's tools. Blank secret fields keep the stored value, so a
// setting can change without entering the token again.
func (m *Manager) Connect(ctx context.Context, id string, values map[string]string) (Status, error) {
	s, err := m.service(id)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, _ := m.credential(id)
	cred := Credential{}
	for _, f := range s.Fields() {
		v := strings.TrimSpace(values[f.Key])
		if v == "" && f.Secret {
			v = old[f.Key]
		}
		if v == "" && !f.Optional {
			return Status{}, fmt.Errorf("%s is required", f.Label)
		}
		if v != "" {
			cred[f.Key] = v
		}
	}
	account, err := s.Check(ctx, m.client, cred)
	if err != nil {
		return Status{}, fmt.Errorf("could not connect to %s: %w", s.Name(), sanitizeErr(err, secretsOnly(s, cred)))
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return Status{}, err
	}
	if err := m.secrets.Write(secretName(id), string(raw)); err != nil {
		return Status{}, err
	}
	now := m.now().UTC().Format(time.RFC3339Nano)
	if _, err := m.db.ExecContext(ctx, `
		INSERT INTO connections (service_id, account, connected_at, checked_at, status, error) VALUES (?, ?, ?, ?, ?, NULL)
		ON CONFLICT(service_id) DO UPDATE SET account = excluded.account, checked_at = excluded.checked_at, status = excluded.status, error = NULL`,
		id, account, now, now, StatusConnected); err != nil {
		return Status{}, err
	}
	m.register(s)
	m.learnLocked(ctx, s, cred)
	return m.statusLocked(ctx, s)
}

// learnLocked calls the service's learning tools once, so tool selection
// knows its vocabulary, such as device names, from the start.
func (m *Manager) learnLocked(ctx context.Context, s Service, cred Credential) {
	for _, t := range s.Tools() {
		if t.Learn == nil {
			continue
		}
		if out, err := t.Run(ctx, m.client, cred, map[string]any{}); err == nil {
			m.setCuesLocked(ctx, s, t.Learn(out))
		}
	}
}

// setCuesLocked stores learned words and updates the catalog.
func (m *Manager) setCuesLocked(ctx context.Context, s Service, cues []string) {
	sort.Strings(cues)
	if len(cues) > maxCues {
		cues = cues[:maxCues]
	}
	m.cues[s.ID()] = cues
	if raw, err := json.Marshal(cues); err == nil {
		_, _ = m.db.ExecContext(ctx, `UPDATE connections SET cues = ? WHERE service_id = ?`, string(raw), s.ID())
	}
	tools.SetConnected(s.ID(), m.definitions(s))
}

// maxCues caps the words kept per service.
const maxCues = 300

// Disconnect deletes the stored credential and removes the tools.
func (m *Manager) Disconnect(ctx context.Context, id string) error {
	s, err := m.service(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range s.Tools() {
		m.registry.Unregister(t.Def.ID)
	}
	tools.SetConnected(s.ID(), nil)
	delete(m.cues, id)
	if err := m.secrets.Delete(secretName(id)); err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM connections WHERE service_id = ?`, id)
	return err
}

// Check verifies the stored credential again and records the result.
func (m *Manager) Check(ctx context.Context, id string) (Status, error) {
	s, err := m.service(id)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cred, err := m.credential(id)
	if err != nil {
		return Status{}, fmt.Errorf("%s is not connected", s.Name())
	}
	account, checkErr := s.Check(ctx, m.client, cred)
	status, msg := StatusConnected, any(nil)
	if checkErr != nil {
		status, msg = StatusError, sanitizeErr(checkErr, secretsOnly(s, cred)).Error()
	}
	if _, err := m.db.ExecContext(ctx, `UPDATE connections SET checked_at = ?, status = ?, error = ?, account = CASE WHEN ? = '' THEN account ELSE ? END WHERE service_id = ?`,
		m.now().UTC().Format(time.RFC3339Nano), status, msg, account, account, id); err != nil {
		return Status{}, err
	}
	return m.statusLocked(ctx, s)
}

// List returns every service and whether it is connected.
func (m *Manager) List(ctx context.Context) ([]Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.services))
	for _, s := range m.services {
		st, err := m.statusLocked(ctx, s)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Manager) statusLocked(ctx context.Context, s Service) (Status, error) {
	st := Status{ID: s.ID(), Name: s.Name(), Description: s.Description(), Scopes: s.Scopes(), Fields: s.Fields(), Tools: []tools.Definition{}}
	for _, t := range s.Tools() {
		st.Tools = append(st.Tools, t.Def)
	}
	var connected, checked string
	var errText sql.NullString
	err := m.db.QueryRowContext(ctx, `SELECT account, connected_at, checked_at, status, error FROM connections WHERE service_id = ?`, s.ID()).
		Scan(&st.Account, &connected, &checked, &st.Status, &errText)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return Status{}, err
	}
	st.Connected = true
	st.ConnectedAt, _ = time.Parse(time.RFC3339Nano, connected)
	st.CheckedAt, _ = time.Parse(time.RFC3339Nano, checked)
	st.Error = errText.String
	if cred, err := m.credential(s.ID()); err == nil {
		st.Values = map[string]string{}
		for _, f := range s.Fields() {
			if v := cred[f.Key]; v != "" {
				if f.Secret {
					v = mask(v)
				}
				st.Values[f.Key] = v
			}
		}
	}
	return st, nil
}

// mask shows only the last four characters of a secret.
func mask(v string) string {
	if len(v) <= 8 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}

func (m *Manager) credential(id string) (Credential, error) {
	raw, err := m.secrets.Read(secretName(id))
	if err != nil {
		return nil, err
	}
	var cred Credential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		return nil, err
	}
	return cred, nil
}

// definitions are a service's tools as the catalog lists them.
func (m *Manager) definitions(s Service) []tools.Definition {
	var defs []tools.Definition
	for _, t := range s.Tools() {
		def := t.Def
		def.Source = "connector:" + s.ID()
		if def.Capability == "" {
			def.Capability = s.ID()
		}
		def.Cues = m.cues[s.ID()]
		defs = append(defs, def)
	}
	return defs
}

// register adds a service's tools to the registry and the catalog.
func (m *Manager) register(s Service) {
	defs := m.definitions(s)
	for i, t := range s.Tools() {
		m.registry.Register(&connectedTool{m: m, service: s, tool: t, def: defs[i]})
	}
	tools.SetConnected(s.ID(), defs)
}

// connectedTool runs a service's tool with the stored credential.
type connectedTool struct {
	m       *Manager
	service Service
	tool    Tool
	def     tools.Definition
}

func (t *connectedTool) ID() string          { return t.def.ID }
func (t *connectedTool) DisplayName() string { return t.def.Name }
func (t *connectedTool) Description() string { return t.def.Description }

func (t *connectedTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	cred, err := t.m.credential(t.service.ID())
	if err != nil {
		return nil, fmt.Errorf("%s is not connected. Connect it in Settings", t.service.Name())
	}
	out, err := t.tool.Run(ctx, t.m.client, cred, args)
	if err != nil {
		return nil, sanitizeErr(err, secretsOnly(t.service, cred))
	}
	if t.tool.Learn != nil && len(args) == 0 {
		// A full read keeps the vocabulary current, such as a new device.
		t.m.mu.Lock()
		t.m.setCuesLocked(context.WithoutCancel(ctx), t.service, t.tool.Learn(out))
		t.m.mu.Unlock()
	}
	return sanitize(out, secretsOnly(t.service, cred)).(map[string]any), nil
}

// secretValues are the values a result must never contain.
// secretsOnly keeps a credential's secret fields, such as a token or
// password. Others, such as an email address or a server, may appear in
// results and stay readable.
func secretsOnly(s Service, cred Credential) Credential {
	out := Credential{}
	for _, f := range s.Fields() {
		if f.Secret && cred[f.Key] != "" {
			out[f.Key] = cred[f.Key]
		}
	}
	return out
}

func secretValues(cred Credential) []string {
	var out []string
	for _, v := range cred {
		if len(v) >= 6 {
			out = append(out, v)
		}
	}
	return out
}

func redact(s string, secrets []string) string {
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}

// sanitize removes any credential value from a result, wherever it is.
func sanitize(v any, cred Credential) any {
	secrets := secretValues(cred)
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return redact(x, secrets)
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, val := range x {
				out[k] = walk(val)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, val := range x {
				out[i] = walk(val)
			}
			return out
		case []map[string]any:
			out := make([]any, len(x))
			for i, val := range x {
				out[i] = walk(val)
			}
			return out
		}
		return v
	}
	if v == nil {
		return map[string]any{}
	}
	return walk(v)
}

func sanitizeErr(err error, cred Credential) error {
	if err == nil {
		return nil
	}
	return errors.New(redact(err.Error(), secretValues(cred)))
}

// ServiceName is how a service is shown, such as "GitHub".
func (m *Manager) ServiceName(id string) string {
	if s, ok := m.services[id]; ok {
		return s.Name()
	}
	return id
}
