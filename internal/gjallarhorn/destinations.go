package gjallarhorn

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Destination kinds (§7).
const (
	KindEmail   = "email"
	KindWebhook = "webhook"
)

// ErrNotFound is returned for an unknown destination.
var ErrNotFound = contracts.NewError("DESTINATION_NOT_FOUND", nil, errors.New("notification destination not found"))

// Secrets stores destination credentials apart from the database (§39).
type Secrets interface {
	Write(name, value string) error
	Read(name string) (string, error)
	Delete(name string) error
}

// EmailConfig is how an email destination reaches your SMTP server (§12).
// The password is a secret and is not part of it.
type EmailConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username,omitempty"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	// TLS is starttls (the default), tls (implicit, usually port 465), or
	// none, which is allowed only for a server on this computer.
	TLS string `json:"tls,omitempty"`
}

// WebhookConfig is where a webhook destination posts (§13). Its signing
// secret is a secret and is not part of it.
type WebhookConfig struct {
	URL string `json:"url"`
}

// Destination is an email address or a webhook that notifications are sent to.
type Destination struct {
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
	Email   *EmailConfig   `json:"email,omitempty"`
	Webhook *WebhookConfig `json:"webhook,omitempty"`
	Ntfy    *NtfyConfig    `json:"ntfy,omitempty"`
	// Categories it receives; empty receives every category (§26).
	Categories []string `json:"categories,omitempty"`
	// MinSeverity is the lowest severity it receives: info (the default),
	// success, warning, or error.
	MinSeverity string `json:"min_severity,omitempty"`
	// Digest, when set, gathers notices into one message a day; errors
	// still go out at once.
	Digest *Digest `json:"digest,omitempty"`
	// HasSecret reports a stored password, signing secret, or access token,
	// never its value.
	HasSecret bool `json:"has_secret"`
}

// DestinationInput creates or changes a destination. On a change, nil
// fields keep their value, and an empty Password keeps the stored one.
type DestinationInput struct {
	Kind        string         `json:"kind,omitempty"`
	Name        *string        `json:"name,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty"`
	Email       *EmailConfig   `json:"email,omitempty"`
	Webhook     *WebhookConfig `json:"webhook,omitempty"`
	Ntfy        *NtfyConfig    `json:"ntfy,omitempty"`
	Categories  *[]string      `json:"categories,omitempty"`
	MinSeverity *string        `json:"min_severity,omitempty"`
	// Digest sets a daily digest; an empty At sends each notice again.
	Digest *Digest `json:"digest,omitempty"`
	// Password is the SMTP password, or the ntfy access token, stored as a
	// secret.
	Password string `json:"password,omitempty"`
}

var severityRank = map[string]int{SeverityInfo: 0, SeveritySuccess: 1, SeverityWarning: 2, SeverityError: 3}

var categories = []string{CategoryAutomation, CategoryApproval, CategoryModel, CategoryTraining, CategoryHealth, CategorySystem}

// Accepts reports whether a destination receives a notification (§26).
func (d Destination) Accepts(n Notification) bool {
	if !d.Enabled {
		return false
	}
	if len(d.Categories) > 0 && !slices.Contains(d.Categories, n.Category) {
		return false
	}
	return severityRank[n.Severity] >= severityRank[d.MinSeverity]
}

func secretName(id string) string { return "notify-" + id }

// channelName is how a destination's deliveries are recorded.
func (d Destination) channelName() string { return d.Kind + ":" + d.ID }

func validate(d Destination) error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("name is required")
	}
	for _, c := range d.Categories {
		if !slices.Contains(categories, c) {
			return fmt.Errorf("unknown category %q", c)
		}
	}
	if _, ok := severityRank[d.MinSeverity]; d.MinSeverity != "" && !ok {
		return fmt.Errorf("min_severity must be info, success, warning, or error")
	}
	if d.Digest != nil {
		if err := d.Digest.Validate(); err != nil {
			return err
		}
	}
	switch d.Kind {
	case KindEmail:
		if d.Email == nil {
			return fmt.Errorf("email settings are required")
		}
		return validateEmail(*d.Email)
	case KindWebhook:
		if d.Webhook == nil {
			return fmt.Errorf("webhook settings are required")
		}
		return validateWebhookURL(d.Webhook.URL)
	case KindNtfy:
		if d.Ntfy == nil {
			return fmt.Errorf("ntfy settings are required")
		}
		return validateNtfy(*d.Ntfy)
	}
	return fmt.Errorf("kind must be email, webhook, or ntfy")
}

// SetSecrets gives the hub a place to keep destination credentials.
func (h *Hub) SetSecrets(s Secrets) { h.secrets = s }

// Destinations lists every destination.
func (h *Hub) Destinations(ctx context.Context) ([]Destination, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, kind, name, enabled, config_json, COALESCE(categories_json, ''), COALESCE(min_severity, ''), COALESCE(digest_json, '') FROM notification_destinations ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Destination{}
	for rows.Next() {
		d, err := h.scanDestination(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Destination returns one destination.
func (h *Hub) Destination(ctx context.Context, id string) (Destination, error) {
	d, err := h.scanDestination(h.db.QueryRowContext(ctx, `SELECT id, kind, name, enabled, config_json, COALESCE(categories_json, ''), COALESCE(min_severity, ''), COALESCE(digest_json, '') FROM notification_destinations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Destination{}, ErrNotFound
	}
	return d, err
}

func (h *Hub) scanDestination(row interface{ Scan(...any) error }) (Destination, error) {
	var d Destination
	var enabled int
	var config, cats, digest string
	if err := row.Scan(&d.ID, &d.Kind, &d.Name, &enabled, &config, &cats, &d.MinSeverity, &digest); err != nil {
		return Destination{}, err
	}
	if digest != "" {
		d.Digest = &Digest{}
		if json.Unmarshal([]byte(digest), d.Digest) != nil || d.Digest.At == "" {
			d.Digest = nil
		}
	}
	d.Enabled = enabled == 1
	switch d.Kind {
	case KindEmail:
		d.Email = &EmailConfig{}
		_ = json.Unmarshal([]byte(config), d.Email)
	case KindWebhook:
		d.Webhook = &WebhookConfig{}
		_ = json.Unmarshal([]byte(config), d.Webhook)
	case KindNtfy:
		d.Ntfy = &NtfyConfig{}
		_ = json.Unmarshal([]byte(config), d.Ntfy)
	}
	if cats != "" {
		_ = json.Unmarshal([]byte(cats), &d.Categories)
	}
	if h.secrets != nil {
		if v, err := h.secrets.Read(secretName(d.ID)); err == nil && v != "" {
			d.HasSecret = true
		}
	}
	return d, nil
}

// CreateDestination stores a destination. For a webhook it returns the
// signing secret, which is shown this once (§13).
func (h *Hub) CreateDestination(ctx context.Context, in DestinationInput) (Destination, string, error) {
	d := Destination{ID: uuid.NewString(), Kind: in.Kind, Enabled: true, Email: in.Email, Webhook: in.Webhook, Ntfy: in.Ntfy}
	applyInput(&d, in)
	if err := validate(d); err != nil {
		return Destination{}, "", err
	}
	if h.secrets == nil {
		return Destination{}, "", fmt.Errorf("secrets are not available")
	}
	secret := ""
	switch d.Kind {
	case KindWebhook:
		secret = newSecret()
		if err := h.secrets.Write(secretName(d.ID), secret); err != nil {
			return Destination{}, "", err
		}
	case KindEmail, KindNtfy:
		if in.Password != "" {
			if err := h.secrets.Write(secretName(d.ID), in.Password); err != nil {
				return Destination{}, "", err
			}
		}
	}
	if err := h.saveDestination(ctx, d, true); err != nil {
		_ = h.secrets.Delete(secretName(d.ID))
		return Destination{}, "", err
	}
	got, err := h.Destination(ctx, d.ID)
	return got, secret, err
}

// UpdateDestination changes a destination.
func (h *Hub) UpdateDestination(ctx context.Context, id string, in DestinationInput) (Destination, error) {
	d, err := h.Destination(ctx, id)
	if err != nil {
		return Destination{}, err
	}
	if in.Email != nil && d.Kind == KindEmail {
		d.Email = in.Email
	}
	if in.Webhook != nil && d.Kind == KindWebhook {
		d.Webhook = in.Webhook
	}
	if in.Ntfy != nil && d.Kind == KindNtfy {
		d.Ntfy = in.Ntfy
	}
	applyInput(&d, in)
	if err := validate(d); err != nil {
		return Destination{}, err
	}
	if in.Password != "" && (d.Kind == KindEmail || d.Kind == KindNtfy) && h.secrets != nil {
		if err := h.secrets.Write(secretName(d.ID), in.Password); err != nil {
			return Destination{}, err
		}
	}
	if err := h.saveDestination(ctx, d, false); err != nil {
		return Destination{}, err
	}
	return h.Destination(ctx, id)
}

// RotateSecret replaces a webhook's signing secret and returns the new one.
func (h *Hub) RotateSecret(ctx context.Context, id string) (string, error) {
	d, err := h.Destination(ctx, id)
	if err != nil {
		return "", err
	}
	if d.Kind != KindWebhook {
		return "", fmt.Errorf("only webhooks have a signing secret")
	}
	secret := newSecret()
	return secret, h.secrets.Write(secretName(id), secret)
}

// DeleteDestination removes a destination, its stored secret, and its
// waiting deliveries.
func (h *Hub) DeleteDestination(ctx context.Context, id string) error {
	res, err := h.db.ExecContext(ctx, `DELETE FROM notification_destinations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, _ = h.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = ?, next_attempt_at = NULL, error = 'The destination was removed.' WHERE destination_id = ? AND status IN (?, ?, ?)`,
		DeliveryCancelled, id, DeliveryPending, DeliveryHeld, DeliveryDigest)
	if h.secrets != nil {
		_ = h.secrets.Delete(secretName(id))
	}
	return nil
}

func applyInput(d *Destination, in DestinationInput) {
	if in.Name != nil {
		d.Name = strings.TrimSpace(*in.Name)
	}
	if in.Enabled != nil {
		d.Enabled = *in.Enabled
	}
	if in.Categories != nil {
		d.Categories = *in.Categories
	}
	if in.MinSeverity != nil {
		d.MinSeverity = *in.MinSeverity
	}
	if in.Digest != nil {
		d.Digest = in.Digest
		if d.Digest.At == "" {
			d.Digest = nil
		} else if d.Digest.TimeZone == "" {
			d.Digest.TimeZone = "UTC"
		}
	}
	if d.Ntfy != nil {
		applyNtfyDefaults(d.Ntfy)
	}
	if d.Email != nil {
		if d.Email.TLS == "" {
			d.Email.TLS = "starttls"
		}
		if d.Email.Port == 0 {
			d.Email.Port = 587
			if d.Email.TLS == "tls" {
				d.Email.Port = 465
			}
		}
	}
}

func (h *Hub) saveDestination(ctx context.Context, d Destination, create bool) error {
	var config []byte
	switch d.Kind {
	case KindEmail:
		config, _ = json.Marshal(d.Email)
	case KindWebhook:
		config, _ = json.Marshal(d.Webhook)
	case KindNtfy:
		config, _ = json.Marshal(d.Ntfy)
	}
	var cats any
	if len(d.Categories) > 0 {
		b, _ := json.Marshal(d.Categories)
		cats = string(b)
	}
	enabled := 0
	if d.Enabled {
		enabled = 1
	}
	var digest any
	if d.Digest != nil {
		b, _ := json.Marshal(d.Digest)
		digest = string(b)
	}
	now := ts(h.now())
	if create {
		_, err := h.db.ExecContext(ctx, `INSERT INTO notification_destinations (id, kind, name, enabled, config_json, categories_json, min_severity, digest_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, d.ID, d.Kind, d.Name, enabled, string(config), cats, nullable(d.MinSeverity), digest, now, now)
		return err
	}
	_, err := h.db.ExecContext(ctx, `UPDATE notification_destinations SET name = ?, enabled = ?, config_json = ?, categories_json = ?, min_severity = ?, digest_json = ?, updated_at = ? WHERE id = ?`,
		d.Name, enabled, string(config), cats, nullable(d.MinSeverity), digest, now, d.ID)
	return err
}

func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}
