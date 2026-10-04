package mimir

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	// Database drivers for database sources. All are pure Go.
	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Database and API sources read current data from a database query or a web
// API instead of a file. Mimir fetches them again when they are older than
// their refresh interval and a search uses them, or on Reindex.
const (
	// KindDatabase runs a read-only query against SQLite, PostgreSQL, or MySQL.
	KindDatabase Kind = "database"
	// KindAPI fetches JSON, CSV, or text from an HTTP URL.
	KindAPI Kind = "api"
)

// Database drivers.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
)

// Remote says how a database or API source is reached. Connection strings
// and header values are secrets and are never part of it.
type Remote struct {
	Driver string `json:"driver,omitempty"`
	// Database is the SQLite file. A PostgreSQL or MySQL connection string
	// contains credentials, so it is stored as a secret.
	Database string `json:"database,omitempty"`
	Query    string `json:"query,omitempty"`
	URL      string `json:"url,omitempty"`
	// Items is the dotted path to the list in a JSON response, such as
	// "data.products". Empty finds a top-level list.
	Items string `json:"items,omitempty"`
	// HeaderNames lists the request headers that are set, without values.
	HeaderNames []string `json:"header_names,omitempty"`
	// RefreshMinutes is how old the data may get before a search fetches it
	// again.
	RefreshMinutes int `json:"refresh_minutes"`
}

// RemoteInput creates or changes a database or API source.
type RemoteInput struct {
	Driver   string `json:"driver,omitempty"`
	Database string `json:"database,omitempty"`
	// ConnectionString reaches PostgreSQL or MySQL, for example
	// postgres://reader:secret@db.local/shop or reader:secret@tcp(db:3306)/shop.
	// On update, empty keeps the stored one.
	ConnectionString string `json:"connection_string,omitempty"`
	Query            string `json:"query,omitempty"`
	URL              string `json:"url,omitempty"`
	Items            string `json:"items,omitempty"`
	// Headers are sent with API requests, such as Authorization. On update,
	// an empty value keeps the stored one and a missing name removes it.
	Headers        map[string]string `json:"headers,omitempty"`
	RefreshMinutes int               `json:"refresh_minutes,omitempty"`
}

// remoteSecret is what a source keeps in the secrets directory.
type remoteSecret struct {
	ConnectionString string            `json:"connection_string,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
}

// Secrets stores credentials outside SQLite. *auth.SecretStore satisfies it.
type Secrets interface {
	Write(name, value string) error
	Read(name string) (string, error)
	Delete(name string) error
}

// SetSecrets lets Mimir store database and API credentials. Without it,
// only sources that need no credentials can be connected.
func (s *Store) SetSecrets(sec Secrets) { s.secrets = sec }

func secretName(id string) string { return "knowledge-" + id }

const (
	defaultRefreshMinutes = 60
	maxRefreshMinutes     = 7 * 24 * 60
	// remoteTimeout bounds one query or request.
	remoteTimeout = 30 * time.Second
)

// buildRemote validates input and splits it into the stored configuration
// and the secret. prev is the current secret when updating.
func buildRemote(kind Kind, in RemoteInput, prev remoteSecret) (Remote, remoteSecret, error) {
	r := Remote{RefreshMinutes: in.RefreshMinutes}
	switch {
	case r.RefreshMinutes == 0:
		r.RefreshMinutes = defaultRefreshMinutes
	case r.RefreshMinutes < 1 || r.RefreshMinutes > maxRefreshMinutes:
		return Remote{}, remoteSecret{}, fmt.Errorf("refresh every 1 minute to 7 days")
	}
	sec := remoteSecret{}
	switch kind {
	case KindDatabase:
		r.Driver = strings.ToLower(strings.TrimSpace(in.Driver))
		r.Query = strings.TrimSpace(in.Query)
		if err := checkReadOnlyQuery(r.Query); err != nil {
			return Remote{}, remoteSecret{}, err
		}
		switch r.Driver {
		case DriverSQLite:
			if strings.TrimSpace(in.Database) == "" {
				return Remote{}, remoteSecret{}, fmt.Errorf("choose the SQLite database file")
			}
			abs, err := filepath.Abs(expandHome(strings.TrimSpace(in.Database)))
			if err != nil {
				return Remote{}, remoteSecret{}, err
			}
			r.Database = abs
		case DriverPostgres, DriverMySQL:
			sec.ConnectionString = strings.TrimSpace(in.ConnectionString)
			if sec.ConnectionString == "" {
				sec.ConnectionString = prev.ConnectionString
			}
			if sec.ConnectionString == "" {
				return Remote{}, remoteSecret{}, fmt.Errorf("enter the connection string")
			}
		default:
			return Remote{}, remoteSecret{}, fmt.Errorf("driver must be sqlite, postgres, or mysql")
		}
	case KindAPI:
		r.URL = strings.TrimSpace(in.URL)
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Remote{}, remoteSecret{}, fmt.Errorf("enter an http or https URL")
		}
		if u.User != nil {
			return Remote{}, remoteSecret{}, fmt.Errorf("put credentials in a header, not in the URL")
		}
		r.Items = strings.Trim(strings.TrimSpace(in.Items), ".")
		for name, value := range in.Headers {
			name = http.CanonicalHeaderKey(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if value == "" {
				value = prev.Headers[name]
			}
			if value == "" {
				continue
			}
			if sec.Headers == nil {
				sec.Headers = map[string]string{}
			}
			sec.Headers[name] = value
			r.HeaderNames = append(r.HeaderNames, name)
		}
		sort.Strings(r.HeaderNames)
	}
	return r, sec, nil
}

var (
	sqlComment   = regexp.MustCompile(`(?s)^\s*(--[^\n]*\n|/\*.*?\*/)`)
	sqlFirstWord = regexp.MustCompile(`^\s*([A-Za-z]+)`)
)

// checkReadOnlyQuery accepts one SELECT, WITH, or VALUES statement. The
// query also runs in a read-only transaction, which is what actually stops
// writes; this check gives a clear message first.
func checkReadOnlyQuery(q string) error {
	if q == "" {
		return fmt.Errorf("enter a SELECT query")
	}
	rest := q
	for {
		loc := sqlComment.FindStringIndex(rest)
		if loc == nil {
			break
		}
		rest = rest[loc[1]:]
	}
	m := sqlFirstWord.FindStringSubmatch(rest)
	if m == nil {
		return fmt.Errorf("enter a SELECT query")
	}
	switch strings.ToUpper(m[1]) {
	case "SELECT", "WITH", "VALUES":
	default:
		return fmt.Errorf("only SELECT queries can be connected; Yggdrasil reads the database and never changes it")
	}
	if i := strings.Index(strings.TrimRight(strings.TrimSpace(q), ";"), ";"); i >= 0 {
		return fmt.Errorf("enter one query, without other statements after it")
	}
	return nil
}

// fetchRemote reads a database or API source.
func (s *Store) fetchRemote(ctx context.Context, src Source) ([]document, error) {
	if src.Remote == nil {
		return nil, fmt.Errorf("the source has no connection settings")
	}
	sec, err := s.readSecret(src.ID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	var docs []document
	switch src.Kind {
	case KindDatabase:
		docs, err = queryDatabase(ctx, src.Name, *src.Remote, sec)
	case KindAPI:
		docs, err = fetchAPI(ctx, src.Name, *src.Remote, sec)
	default:
		err = fmt.Errorf("%s is not a database or API source", src.Kind)
	}
	return docs, redact(err, sec)
}

func (s *Store) readSecret(id string) (remoteSecret, error) {
	var sec remoteSecret
	if s.secrets == nil {
		return sec, nil
	}
	raw, err := s.secrets.Read(secretName(id))
	if err != nil || raw == "" {
		return sec, nil
	}
	if err := json.Unmarshal([]byte(raw), &sec); err != nil {
		return sec, fmt.Errorf("the stored credentials are damaged; enter them again")
	}
	return sec, nil
}

func (s *Store) writeSecret(id string, sec remoteSecret) error {
	if sec.ConnectionString == "" && len(sec.Headers) == 0 {
		if s.secrets != nil {
			_ = s.secrets.Delete(secretName(id))
		}
		return nil
	}
	if s.secrets == nil {
		return fmt.Errorf("credentials cannot be stored here")
	}
	raw, _ := json.Marshal(sec)
	return s.secrets.Write(secretName(id), string(raw))
}

// redact removes credentials that a driver or server echoed into an error.
func redact(err error, sec remoteSecret) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	secrets := []string{sec.ConnectionString}
	if u, perr := url.Parse(sec.ConnectionString); perr == nil && u.User != nil {
		if p, ok := u.User.Password(); ok {
			secrets = append(secrets, p)
		}
	}
	if cfg, perr := mysql.ParseDSN(sec.ConnectionString); perr == nil {
		secrets = append(secrets, cfg.Passwd)
	}
	for _, v := range sec.Headers {
		secrets = append(secrets, v)
	}
	for _, v := range secrets {
		if len(v) >= 4 {
			msg = strings.ReplaceAll(msg, v, "[hidden]")
		}
	}
	return errors.New(msg)
}

func queryDatabase(ctx context.Context, name string, r Remote, sec remoteSecret) ([]document, error) {
	driver, dsn := "", ""
	readOnlyTx := true
	switch r.Driver {
	case DriverSQLite:
		// Opened read-only, so even a query that slipped past the check
		// cannot change the file.
		driver, dsn, readOnlyTx = "sqlite", "file:"+filepath.ToSlash(r.Database)+"?mode=ro", false
	case DriverPostgres:
		driver, dsn = "pgx", sec.ConnectionString
	case DriverMySQL:
		driver, dsn = "mysql", sec.ConnectionString
	default:
		return nil, fmt.Errorf("unknown driver %q", r.Driver)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnlyTx})
	if err != nil {
		return nil, fmt.Errorf("cannot connect: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, r.Query)
	if err != nil {
		return nil, fmt.Errorf("the query failed: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	doc := document{Name: name, Header: cols}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if len(doc.Rows) >= maxTableRows {
			return nil, fmt.Errorf("the query returns more than %d rows; narrow it with WHERE or LIMIT", maxTableRows)
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = cellText(v)
		}
		doc.Rows = append(doc.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("the query failed: %w", err)
	}
	return []document{doc}, nil
}

// cellText renders a database value as table text.
func cellText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return fmt.Sprintf("(%d bytes of binary data)", len(x))
	case string:
		return x
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(x)
	}
}

var apiClient = &http.Client{Timeout: remoteTimeout}

func fetchAPI(ctx context.Context, name string, r Remote, sec remoteSecret) ([]document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/csv;q=0.9, text/plain;q=0.8, */*;q=0.5")
	req.Header.Set("User-Agent", "Toskar-Mimir")
	for k, v := range sec.Headers {
		req.Header.Set(k, v)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		// url.Error repeats the method and URL; the host is enough.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("cannot reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxTextBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("%s answered %s: %s", req.URL.Host, resp.Status, snippet)
	}
	if len(body) > MaxTextBytes {
		return nil, fmt.Errorf("the response is larger than %d MB", MaxTextBytes>>20)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	trimmed := strings.TrimSpace(string(body))
	switch {
	case strings.Contains(mediaType, "json") || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
		return apiJSON(name, r.Items, body)
	case mediaType == "text/csv" || strings.HasSuffix(strings.ToLower(req.URL.Path), ".csv"):
		doc, err := parseTable(name, body, ',')
		return []document{doc}, err
	case strings.Contains(mediaType, "html"):
		return []document{{Name: name, Text: stripHTML(string(body))}}, nil
	default:
		if !utf8.Valid(body) {
			return nil, fmt.Errorf("the response is not text (%s)", mediaType)
		}
		return []document{{Name: name, Text: string(body)}}, nil
	}
}

// apiJSON turns a JSON response into a table when it holds a list of
// objects, and into text otherwise.
func apiJSON(name, items string, body []byte) ([]document, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("the response is not valid JSON: %w", err)
	}
	if items != "" {
		for _, key := range strings.Split(items, ".") {
			obj, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%q is not in the response", items)
			}
			if v, ok = obj[key]; !ok {
				return nil, fmt.Errorf("%q is not in the response", items)
			}
		}
	} else if obj, ok := v.(map[string]any); ok {
		// {"data": [...], "next": ...}: use the one list of objects.
		var found any
		for _, val := range obj {
			if objectList(val) != nil {
				if found != nil {
					found = nil
					break
				}
				found = val
			}
		}
		if found != nil {
			v = found
		}
	}
	if objs := objectList(v); objs != nil {
		return []document{objectsTable(name, objs)}, nil
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	return []document{{Name: name, Text: string(pretty)}}, nil
}

// objectList returns v as a list of objects, or nil.
func objectList(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		out = append(out, obj)
	}
	return out
}

// remoteDue reports whether a database or API source should be fetched
// again: its last fetch is older than its refresh interval.
func remoteDue(r *Remote, lastFetch string, now time.Time) bool {
	if r == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, lastFetch)
	if err != nil {
		return true
	}
	minutes := r.RefreshMinutes
	if minutes <= 0 {
		minutes = defaultRefreshMinutes
	}
	return now.Sub(t) >= time.Duration(minutes)*time.Minute
}
