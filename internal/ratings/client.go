package ratings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Where ratings go and the public summary comes from, unless configuration
// names others.
const (
	DefaultServiceURL = "https://ratings.toskar.ai"
	DefaultSummaryURL = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/ratings/summary.json"
)

// Rating is one submission, in the ratings service's schema version 1.
type Rating struct {
	SchemaVersion int           `json:"schema_version"`
	ClientID      string        `json:"client_id"`
	Model         model         `json:"model"`
	Runtime       runtime       `json:"runtime"`
	Hardware      Hardware      `json:"hardware"`
	Stars         int           `json:"stars"`
	Tags          []string      `json:"tags,omitempty"`
	Observations  *Observations `json:"observations,omitempty"`
	AppVersion    string        `json:"app_version,omitempty"`
	// Language is the language the model was used in, when the person said.
	Language string `json:"language,omitempty"`
}

type model struct {
	ID           string `json:"id"`
	Format       string `json:"format"`
	Quantization string `json:"quantization"`
}

type runtime struct {
	Type    string `json:"type"`
	Backend string `json:"backend"`
}

// Snapshot is the public summary of everyone's ratings: no per-person
// records, and only cohorts with enough ratings.
type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Prior         float64   `json:"prior"`
	Weight        int       `json:"weight"`
	MinRatings    int       `json:"min_ratings"`
	Models        []struct {
		Model        string  `json:"model"`
		Format       string  `json:"format"`
		Quantization string  `json:"quantization"`
		Runtime      string  `json:"runtime"`
		Backend      string  `json:"backend"`
		Cohorts      []Stats `json:"cohorts"`
		// Languages are the configuration's ratings by language.
		Languages []LanguageStats `json:"languages,omitempty"`
	} `json:"models"`
}

// Stats are one cohort's ratings.
type Stats struct {
	Tier          string         `json:"tier"`
	Cohort        string         `json:"cohort,omitempty"`
	Ratings       int            `json:"ratings"`
	Average       float64        `json:"average"`
	WeightedScore float64        `json:"weighted_score"`
	Confidence    string         `json:"confidence"`
	Tags          map[string]int `json:"tags,omitempty"`
	// How the model ran for those who shared it.
	Observed              int      `json:"observed,omitempty"`
	MedianTokensPerSecond *float64 `json:"median_tokens_per_second,omitempty"`
	MedianTTFTMillis      *float64 `json:"median_ttft_ms,omitempty"`
	SuccessfulStartRate   *float64 `json:"successful_start_rate,omitempty"`
	CrashRate             *float64 `json:"crash_rate,omitempty"`
	OutOfMemoryRate       *float64 `json:"out_of_memory_rate,omitempty"`
}

// Client talks to the ratings service.
type Client struct {
	ServiceURL string
	SummaryURL string
	HTTP       *http.Client
	UserAgent  string
}

func (c *Client) service() string {
	if c.ServiceURL != "" {
		return strings.TrimRight(c.ServiceURL, "/")
	}
	return DefaultServiceURL
}

// ServiceHost is where ratings are sent, for What left this computer.
func (c *Client) ServiceHost() string {
	if u, err := url.Parse(c.service()); err == nil && u.Host != "" {
		return u.Host
	}
	return c.service()
}

func (c *Client) do(ctx context.Context, method, url string, body any, header http.Header, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header[k] = v
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		return &StatusError{Code: resp.StatusCode, Message: e.Error}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

// StatusError is an answer from the service that is not a success.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("the ratings service answered %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("the ratings service answered %d", e.Code)
}

// Submit sends a rating, replacing this client's earlier rating of the same
// configuration, and returns its key.
func (c *Client) Submit(ctx context.Context, r Rating) (string, error) {
	var out struct {
		Key string `json:"key"`
	}
	if err := c.do(ctx, http.MethodPost, c.service()+"/v1/ratings", r, nil, &out); err != nil {
		return "", err
	}
	if out.Key == "" {
		return "", errors.New("the ratings service did not return a key")
	}
	return out.Key, nil
}

// Remove deletes a shared rating. One already gone is not an error.
func (c *Client) Remove(ctx context.Context, key, clientID string) error {
	err := c.do(ctx, http.MethodDelete, c.service()+"/v1/ratings/"+url.PathEscape(key), nil, http.Header{"X-Ratings-Client": {clientID}}, nil)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil
	}
	return err
}

// Summary downloads the public summary from the service, or from the public
// dataset when the service cannot be reached, and says which it used.
func (c *Client) Summary(ctx context.Context) (Snapshot, string, error) {
	var snap Snapshot
	first := c.service() + "/v1/aggregates"
	err := c.do(ctx, http.MethodGet, first, nil, nil, &snap)
	if err == nil && snap.SchemaVersion == 1 {
		return snap, first, nil
	}
	second := c.SummaryURL
	if second == "" {
		second = DefaultSummaryURL
	}
	snap = Snapshot{}
	if err2 := c.do(ctx, http.MethodGet, second, nil, nil, &snap); err2 != nil {
		return Snapshot{}, "", errors.Join(err, err2)
	}
	if snap.SchemaVersion != 1 {
		return Snapshot{}, "", fmt.Errorf("the ratings summary is version %d, which this Yggdrasil does not read", snap.SchemaVersion)
	}
	return snap, second, nil
}
