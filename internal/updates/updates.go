// Package updates tells people who installed Toskar from a download when a
// newer version is out (yeixio/toskar-apps#90). Once a day it reads a small
// public file on toskar.ai naming the latest release; it sends nothing but
// the request. App Store builds and the copy the desktop app runs update
// themselves, so they don't check.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the file naming the latest Toskar Core release.
const DefaultURL = "https://toskar.ai/releases/latest.json"

// Setting is the setting that turns the check off (on by default).
const Setting = "update_check"

const (
	firstCheckDelay = time.Minute
	checkInterval   = 24 * time.Hour
	requestTimeout  = 15 * time.Second
	maxBody         = 64 << 10
)

// Release is what toskar.ai says about the latest release.
type Release struct {
	Version     string `json:"version"`
	PublishedAt string `json:"published_at,omitempty"`
	Prerelease  bool   `json:"prerelease,omitempty"`
	NotesURL    string `json:"notes_url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

// Status is what the app shows: whether a newer version is out.
type Status struct {
	// Supported is false for a build that doesn't check: the App Store
	// edition, the desktop app's copy, and development builds.
	Supported bool `json:"supported"`
	// Enabled is true when this build checks and the setting is on.
	Enabled bool   `json:"enabled"`
	Current string `json:"current"`
	// Latest is the release toskar.ai named at the last successful check.
	Latest *Release `json:"latest,omitempty"`
	// Available is true when Latest is newer than Current.
	Available bool       `json:"available"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

// Checker reads the latest release once a day and remembers the answer.
type Checker struct {
	URL     string
	Client  *http.Client
	Current string
	// Supported reports whether this build checks at all. Nil means it does.
	Supported func() bool
	// On reports whether the setting is on. Nil means it is.
	On func(ctx context.Context) bool
	// Sent records the request in What left this computer.
	Sent func(ctx context.Context, host, detail string)
	Now  func() time.Time

	mu        sync.Mutex
	latest    *Release
	checkedAt time.Time
}

// Run checks a minute after start, then once a day, until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	wait := firstCheckDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = checkInterval
		if !c.enabled(ctx) {
			continue
		}
		_ = c.Check(ctx)
	}
}

// Check reads the latest release now.
func (c *Checker) Check(ctx context.Context) error {
	target := c.URL
	if target == "" {
		target = DefaultURL
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.Sent != nil {
		if u, err := url.Parse(target); err == nil {
			c.Sent(ctx, u.Host, "the latest version number")
		}
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update check: %s answered %s", target, resp.Status)
	}
	var latest Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&latest); err != nil {
		return fmt.Errorf("update check: %w", err)
	}
	if _, ok := parse(latest.Version); !ok {
		return fmt.Errorf("update check: %q is not a version", latest.Version)
	}
	c.mu.Lock()
	c.latest, c.checkedAt = &latest, c.now()
	c.mu.Unlock()
	return nil
}

// Status is the result of the last check, for a build that checks.
func (c *Checker) Status(ctx context.Context) Status {
	s := Status{Current: c.Current, Supported: c.supported()}
	s.Enabled = s.Supported && (c.On == nil || c.On(ctx))
	c.mu.Lock()
	latest, checked := c.latest, c.checkedAt
	c.mu.Unlock()
	if !s.Enabled || latest == nil {
		return s
	}
	s.Latest = latest
	s.CheckedAt = &checked
	s.Available = Newer(latest.Version, c.Current) && (!latest.Prerelease || isPrerelease(c.Current))
	return s
}

func (c *Checker) supported() bool { return c.Supported == nil || c.Supported() }

func (c *Checker) enabled(ctx context.Context) bool {
	return c.supported() && (c.On == nil || c.On(ctx))
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Checks reports whether a build at this version checks at all: release
// builds do; development builds (0.1.0-dev) don't.
func Checks(current string) bool {
	_, ok := parse(current)
	return ok && !strings.Contains(current, "-dev")
}

// version is a parsed MAJOR.MINOR.PATCH with an optional prerelease.
type version struct {
	nums [3]int
	pre  string
}

func parse(v string) (version, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var out version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		out.nums[i] = n
	}
	out.pre = pre
	return out, true
}

func isPrerelease(v string) bool {
	p, ok := parse(v)
	return ok && p.pre != ""
}

// Newer reports whether a is a later version than b, as semver orders them:
// 1.7.0 after 1.6.1, and a release after its own prerelease (1.7.0 after
// 1.7.0-beta.1). An unreadable version is never newer.
func Newer(a, b string) bool {
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range va.nums {
		if va.nums[i] != vb.nums[i] {
			return va.nums[i] > vb.nums[i]
		}
	}
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	default:
		return va.pre > vb.pre
	}
}
