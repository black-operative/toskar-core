package gjallarhorn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// KindNtfy is push to phones and computers through ntfy (§10–11), on
// ntfy.sh or a server you run. It works on Android, iPhone, and in a
// browser, and needs no Yeix-hosted service (§40).
const KindNtfy = "ntfy"

// DefaultNtfyServer is the public ntfy server.
const DefaultNtfyServer = "https://ntfy.sh"

// NtfyConfig is where push notifications are published. An access token,
// when the topic needs one, is a secret and is not part of it.
type NtfyConfig struct {
	Server string `json:"server"`
	Topic  string `json:"topic"`
	// Content is full (title and text) or private, which sends only "You
	// have a new Yggdrasil notification" (§11). Private is the default on
	// the public server, where anyone who knows the topic can read it.
	Content string `json:"content,omitempty"`
	// OpenURL is this Yggdrasil's address, such as http://192.168.1.10:7331.
	// When set, tapping a notification opens it there.
	OpenURL string `json:"open_url,omitempty"`
}

var topicRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// publicNtfy reports the public ntfy.sh server.
func publicNtfy(server string) bool {
	u, err := url.Parse(server)
	return err == nil && strings.EqualFold(strings.TrimPrefix(u.Hostname(), "www."), "ntfy.sh")
}

func applyNtfyDefaults(c *NtfyConfig) {
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	if c.Server == "" {
		c.Server = DefaultNtfyServer
	}
	if c.Content == "" {
		c.Content = "full"
		if publicNtfy(c.Server) {
			c.Content = "private"
		}
	}
	c.OpenURL = strings.TrimRight(strings.TrimSpace(c.OpenURL), "/")
}

func validateNtfy(c NtfyConfig) error {
	if err := validateWebhookURL(c.Server); err != nil {
		return fmt.Errorf("the ntfy server: %s", strings.TrimPrefix(err.Error(), "the webhook address "))
	}
	if !topicRe.MatchString(c.Topic) {
		return fmt.Errorf("the topic may use letters, digits, - and _, up to 64 characters")
	}
	if c.Content != "full" && c.Content != "private" {
		return fmt.Errorf("content must be full or private")
	}
	if c.OpenURL != "" {
		if u, err := url.Parse(c.OpenURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("the Toskar address must be an http or https address")
		}
	}
	return nil
}

// ntfyPriority maps severity onto ntfy's priorities (1–5).
func ntfyPriority(severity string) int {
	switch severity {
	case SeverityError:
		return 4
	case SeverityWarning:
		return 3
	default:
		return 2
	}
}

var ntfyTags = map[string]string{SeverityError: "warning", SeverityWarning: "warning", SeveritySuccess: "white_check_mark", SeverityInfo: "bell"}

// ntfyMessage is ntfy's JSON publish body.
type ntfyMessage struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags,omitempty"`
	Click    string   `json:"click,omitempty"`
}

func ntfyMessageFor(c NtfyConfig, n Notification) ntfyMessage {
	m := ntfyMessage{Topic: c.Topic, Title: "Toskar", Message: n.text("notifications:sent.privatePush", nil), Priority: ntfyPriority(n.Severity)}
	if c.Content == "full" {
		m.Title = n.Title
		if n.RepeatCount > 1 {
			m.Title = n.text("notifications:sent.times", map[string]any{"title": n.Title, "count": n.RepeatCount})
		}
		m.Message = n.Body
		if m.Message == "" {
			m.Message = n.Title
		}
		if tag := ntfyTags[n.Severity]; tag != "" {
			m.Tags = []string{tag}
		}
	}
	if c.OpenURL != "" {
		link := n.Link
		if link == "" || !strings.HasPrefix(link, "/") {
			link = "/"
		}
		m.Click = c.OpenURL + link
	}
	return m
}

// sendNtfy publishes a notification to a topic, with token when it needs one.
func sendNtfy(ctx context.Context, client *http.Client, c NtfyConfig, token string, n Notification) error {
	if err := validateNtfy(c); err != nil {
		return PermanentError{Err: err}
	}
	body, _ := json.Marshal(ntfyMessageFor(c, n))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server, bytes.NewReader(body))
	if err != nil {
		return PermanentError{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Toskar-Gjallarhorn/1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the ntfy server: %s", shortNetErr(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		return nil
	case c == http.StatusUnauthorized || c == http.StatusForbidden:
		return permanent("the ntfy server refused to publish to this topic (%d); check the access token", c)
	case c == http.StatusTooManyRequests || c >= 500:
		return fmt.Errorf("the ntfy server answered %d", c)
	default:
		return permanent("the ntfy server answered %d; check the server address and topic", c)
	}
}
