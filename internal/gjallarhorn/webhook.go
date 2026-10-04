package gjallarhorn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/locale"
)

// Webhook request headers (§13).
const (
	// SignatureHeader is "t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">",
	// keyed with the destination's signing secret.
	SignatureHeader      = "Yggdrasil-Signature"
	NotificationIDHeader = "Yggdrasil-Notification-Id"
)

// WebhookPayload is the JSON a webhook receives (§13). Fields are only added.
type WebhookPayload struct {
	Version        int       `json:"version"`
	NotificationID string    `json:"notification_id"`
	CreatedAt      time.Time `json:"created_at"`
	Category       string    `json:"category"`
	Severity       string    `json:"severity"`
	// Title and Body are written in Language, the App language.
	Title string `json:"title"`
	Body  string `json:"body"`
	// Language is the BCP 47 tag Title and Body are in, such as "de".
	Language string `json:"language,omitempty"`
	// Message is the title and body as catalog keys with their values
	// (i18n/locales/<language>/notifications.json), for receivers that
	// write the notice in their own language.
	Message     *locale.Message `json:"message,omitempty"`
	Link        string          `json:"link,omitempty"`
	RepeatCount int             `json:"repeat_count,omitempty"`
	Source      struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
	} `json:"source"`
}

// PermanentError is a delivery failure that retrying cannot fix, such as
// bad credentials or an address that does not exist (§38). Its message says
// what to change.
type PermanentError struct{ Err error }

func (e PermanentError) Error() string { return e.Err.Error() }
func (e PermanentError) Unwrap() error { return e.Err }

func permanent(format string, args ...any) error {
	return PermanentError{Err: fmt.Errorf(format, args...)}
}

// IsPermanent reports a failure that should not be retried.
func IsPermanent(err error) bool {
	var p PermanentError
	return errors.As(err, &p)
}

// validateWebhookURL requires HTTPS, except for an address on this computer
// or the local network (§13, §39).
func validateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("the webhook address is not a valid URL")
	}
	if u.User != nil {
		return fmt.Errorf("put credentials in the receiving service, not in the webhook address")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if localHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("the webhook address must use https, unless it is on this computer or the local network")
	}
	return fmt.Errorf("the webhook address must start with https://")
}

// localHost reports a host on this computer or the local network.
func localHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// Sign returns the signature header value for body at t.
func Sign(secret string, t time.Time, body []byte) string {
	stamp := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// webhookClient does not follow redirects: a webhook that moved should be
// updated, not silently sent elsewhere.
var webhookClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func payloadFor(n Notification) WebhookPayload {
	p := WebhookPayload{Version: 1, NotificationID: n.ID, CreatedAt: n.CreatedAt, Category: n.Category, Severity: n.Severity,
		Title: n.Title, Body: n.Body, Language: n.lang, Message: n.Message, Link: n.Link}
	if p.Language == "" {
		p.Language = locale.Source
	}
	if n.RepeatCount > 1 {
		p.RepeatCount = n.RepeatCount
	}
	p.Source.Type, p.Source.ID = n.SourceType, n.SourceID
	return p
}

// sendWebhook posts a notification to a webhook, signed with secret.
func sendWebhook(ctx context.Context, client *http.Client, cfg WebhookConfig, secret string, n Notification, now time.Time) error {
	if err := validateWebhookURL(cfg.URL); err != nil {
		return PermanentError{Err: err}
	}
	if secret == "" {
		return permanent("the webhook has no signing secret; rotate it to make one")
	}
	body, _ := json.Marshal(payloadFor(n))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return PermanentError{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Toskar-Gjallarhorn/1")
	req.Header.Set(SignatureHeader, Sign(secret, now, body))
	req.Header.Set(NotificationIDHeader, n.ID)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the webhook: %s", shortNetErr(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		return nil
	case c >= 300 && c < 400:
		return permanent("the webhook answered %d and redirects elsewhere; update its address", c)
	case c == http.StatusRequestTimeout || c == http.StatusTooManyRequests || c >= 500:
		return fmt.Errorf("the webhook answered %d", c)
	default:
		return permanent("the webhook answered %d; check its address and that it accepts these requests", c)
	}
}

// shortNetErr keeps a network error readable, without the full URL.
func shortNetErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}
