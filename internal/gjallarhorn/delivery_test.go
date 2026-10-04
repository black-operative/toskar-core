package gjallarhorn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Write(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[name] = value
	return nil
}
func (s *memSecrets) Read(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", fmt.Errorf("missing")
	}
	return v, nil
}
func (s *memSecrets) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

type memSettings struct{ m map[string]string }

func (s *memSettings) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := s.m[k]
	return v, ok, nil
}
func (s *memSettings) Set(_ context.Context, k, v string) error {
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[k] = v
	return nil
}

type memEgress struct {
	mu   sync.Mutex
	adds []string
}

func (e *memEgress) Add(_ context.Context, kind, dest, detail string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adds = append(e.adds, kind+" "+dest+" "+detail)
}

// deliveryHub is a hub with secrets, settings, and an egress log, and a
// clock the test moves.
func deliveryHub(t *testing.T) (*Hub, *memEgress, *time.Time) {
	t.Helper()
	hub, _ := newHub(t)
	hub.SetSecrets(&memSecrets{})
	hub.SetSettings(&memSettings{})
	eg := &memEgress{}
	hub.SetEgress(eg)
	clock := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	hub.now = func() time.Time { return clock }
	return hub, eg, &clock
}

func ptr[T any](v T) *T { return &v }

func deliveryOf(t *testing.T, hub *Hub, id string) Delivery {
	t.Helper()
	n, err := hub.Get(context.Background(), id)
	if err != nil || len(n.Deliveries) == 0 {
		t.Fatalf("deliveries of %s: %+v %v", id, n.Deliveries, err)
	}
	return n.Deliveries[len(n.Deliveries)-1]
}

// A webhook gets the notification, signed with its own secret, and the
// delivery is recorded in What left this computer (§13, §63).
func TestWebhookSignedAndRecorded(t *testing.T) {
	var got struct {
		body           []byte
		sig, legacySig string
		id, legacyID   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.sig = r.Header.Get(SignatureHeader)
		got.legacySig = r.Header.Get(LegacySignatureHeader)
		got.id = r.Header.Get(NotificationIDHeader)
		got.legacyID = r.Header.Get(LegacyNotificationIDHeader)
	}))
	defer srv.Close()
	hub, eg, clock := deliveryHub(t)
	ctx := context.Background()
	d, secret, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Hook"), Webhook: &WebhookConfig{URL: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "whsec_") || !d.HasSecret {
		t.Fatalf("secret %q, has %v", secret, d.HasSecret)
	}
	if b, _ := json.Marshal(d); strings.Contains(string(b), secret) {
		t.Fatal("the destination's JSON shows its secret")
	}
	n, _ := hub.Notify(ctx, Request{SourceType: "automation", SourceID: "a1", Category: CategoryAutomation, Severity: SeveritySuccess, Title: "Price dropped", Body: "Now $499."})
	if dl := n.Deliveries[0]; dl.Status != DeliveryPending || dl.DestinationID != d.ID {
		t.Fatalf("queued %+v", dl)
	}
	hub.DeliverDue(ctx)
	if dl := deliveryOf(t, hub, n.ID); dl.Status != DeliveryDelivered || dl.Attempts != 1 {
		t.Fatalf("delivery %+v", dl)
	}
	if want := Sign(secret, *clock, got.body); got.sig != want {
		t.Fatalf("signature %q, want %q", got.sig, want)
	}
	// Receivers written before the Toskar rename check the old names.
	if got.legacySig != got.sig || got.id != n.ID || got.legacyID != n.ID {
		t.Fatalf("legacy signature %q, ids %q / %q, want %q", got.legacySig, got.id, got.legacyID, n.ID)
	}
	var p WebhookPayload
	_ = json.Unmarshal(got.body, &p)
	if p.Version != 1 || p.Title != "Price dropped" || p.Source.ID != "a1" || p.Category != CategoryAutomation {
		t.Fatalf("payload %+v", p)
	}
	if len(eg.adds) != 1 || !strings.HasPrefix(eg.adds[0], "notification ") || !strings.Contains(eg.adds[0], "Price dropped") {
		t.Fatalf("egress %v", eg.adds)
	}
}

// A failed delivery is retried on its own after 1, 5, and 30 minutes, then
// given up; a permanent failure is not retried (§36–38).
func TestRetriesAndPermanentFailure(t *testing.T) {
	status := http.StatusBadGateway
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
	}))
	defer srv.Close()
	hub, _, clock := deliveryHub(t)
	ctx := context.Background()
	if _, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Hook"), Webhook: &WebhookConfig{URL: srv.URL}}); err != nil {
		t.Fatal(err)
	}
	n, _ := hub.Notify(ctx, Request{Title: "Report ready", Severity: SeverityInfo})
	start := *clock
	for i, wait := range []time.Duration{0, time.Minute, 5 * time.Minute, 30 * time.Minute} {
		*clock = clock.Add(wait)
		hub.DeliverDue(ctx)
		dl := deliveryOf(t, hub, n.ID)
		if dl.Attempts != i+1 {
			t.Fatalf("attempt %d: %+v", i+1, dl)
		}
		if i < 3 && (dl.Status != DeliveryPending || dl.NextAttemptAt == nil) {
			t.Fatalf("attempt %d not rescheduled: %+v", i+1, dl)
		}
	}
	if dl := deliveryOf(t, hub, n.ID); dl.Status != DeliveryFailed || !strings.Contains(dl.Error, "502") {
		t.Fatalf("after 4 attempts: %+v", dl)
	}
	// Nothing more happens once it has failed.
	*clock = start.Add(24 * time.Hour)
	hub.DeliverDue(ctx)
	if calls != 4 {
		t.Fatalf("calls = %d", calls)
	}

	status = http.StatusNotFound
	n2, _ := hub.Notify(ctx, Request{Title: "Another"})
	hub.DeliverDue(ctx)
	if dl := deliveryOf(t, hub, n2.ID); dl.Status != DeliveryFailed || dl.Attempts != 1 || !strings.Contains(dl.Error, "404") {
		t.Fatalf("permanent failure retried: %+v", dl)
	}
}

func TestWebhookAddress(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://example.com/hook":      true,
		"http://example.com/hook":       false,
		"http://192.168.1.20:8123/hook": true,
		"http://localhost:9000":         true,
		"http://nas.local/hook":         true,
		"ftp://example.com":             false,
		"https://user:pw@example.com":   false,
		"not a url":                     false,
	} {
		if err := validateWebhookURL(url); (err == nil) != ok {
			t.Errorf("%s: %v", url, err)
		}
	}
}

// fakeSMTP is a minimal SMTP server: it accepts one message, or refuses the
// sign-in.
func fakeSMTP(t *testing.T, refuseAuth bool) (port int, got *string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	msg := ""
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				say := func(s string) { fmt.Fprintf(c, "%s\r\n", s) }
				say("220 fake ESMTP")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"):
						say("250-fake")
						say("250 AUTH PLAIN")
					case strings.HasPrefix(cmd, "AUTH"):
						if refuseAuth {
							say("535 5.7.8 bad credentials")
						} else {
							say("235 ok")
						}
					case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
						say("250 ok")
					case cmd == "DATA":
						say("354 go")
						var b strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil || l == ".\r\n" {
								break
							}
							b.WriteString(l)
						}
						mu.Lock()
						msg = b.String()
						mu.Unlock()
						say("250 queued")
					case cmd == "QUIT":
						say("221 bye")
						return
					default:
						say("250 ok")
					}
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, &msg
}

// Email goes through your own SMTP server; a refused sign-in is permanent
// and says what to fix (§12, §38).
func TestEmailDelivery(t *testing.T) {
	port, got := fakeSMTP(t, false)
	hub, eg, _ := deliveryHub(t)
	ctx := context.Background()
	cfg := &EmailConfig{Host: "127.0.0.1", Port: port, Username: "me", From: "Yggdrasil <ygg@example.com>", To: []string{"me@example.com"}, TLS: "none"}
	d, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindEmail, Name: ptr("Me"), Email: cfg, Password: "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(d); strings.Contains(string(b), "hunter2") {
		t.Fatal("the password is in the destination's JSON")
	}
	if err := hub.TestDestination(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*got, "Subject: Yggdrasil: Test from Yggdrasil") || !strings.Contains(*got, "To: me@example.com") {
		t.Fatalf("message:\n%s", *got)
	}
	if len(eg.adds) != 1 || !strings.Contains(eg.adds[0], fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatalf("egress %v", eg.adds)
	}

	badPort, _ := fakeSMTP(t, true)
	cfg.Port = badPort
	if _, err := hub.UpdateDestination(ctx, d.ID, DestinationInput{Email: cfg}); err != nil {
		t.Fatal(err)
	}
	err = hub.TestDestination(ctx, d.ID)
	if !IsPermanent(err) || !strings.Contains(err.Error(), "username and password") {
		t.Fatalf("refused sign-in: %v", err)
	}
}

func TestEmailSettings(t *testing.T) {
	ok := EmailConfig{Host: "smtp.example.com", Port: 587, From: "a@example.com", To: []string{"b@example.com"}, TLS: "starttls"}
	if err := validateEmail(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []EmailConfig{
		{Host: "smtp.example.com", From: "a@example.com", To: []string{"b@example.com"}, TLS: "none"},
		{Host: "smtp.example.com", From: "nope", To: []string{"b@example.com"}},
		{Host: "smtp.example.com", From: "a@example.com"},
		{From: "a@example.com", To: []string{"b@example.com"}},
	} {
		if err := validateEmail(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// Quiet hours hold info and success outside the app until they end; errors
// still go out (§24–25).
func TestQuietHours(t *testing.T) {
	q := QuietHours{Enabled: true, Start: "22:00", End: "07:00", TimeZone: "Europe/Berlin", Allow: "errors"}
	night := time.Date(2026, 10, 2, 0, 30, 0, 0, time.UTC) // 02:30 in Berlin
	until, held := q.HeldUntil(SeveritySuccess, night)
	if !held || !until.Equal(time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("held=%v until %v", held, until)
	}
	if _, held := q.HeldUntil(SeverityError, night); held {
		t.Fatal("an error was held")
	}
	if _, held := q.HeldUntil(SeverityInfo, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)); held {
		t.Fatal("held at noon")
	}
	q.Allow = "nothing"
	if _, held := q.HeldUntil(SeverityError, night); !held {
		t.Fatal("allow nothing let an error through")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	hub, _, clock := deliveryHub(t)
	ctx := context.Background()
	desktop := &fakeChannel{name: "desktop"}
	hub.channels["desktop"] = desktop
	if _, err := hub.SetQuietHours(ctx, QuietHours{Enabled: true, Start: "22:00", End: "07:00", TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	_, _, _ = hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Hook"), Webhook: &WebhookConfig{URL: srv.URL}})
	*clock = time.Date(2026, 10, 2, 23, 0, 0, 0, time.UTC)
	n, _ := hub.Notify(ctx, Request{Title: "Nightly backup done", Severity: SeveritySuccess, Channels: []string{"desktop"}})
	for _, d := range n.Deliveries {
		if d.Status != DeliveryHeld || d.NextAttemptAt == nil || d.NextAttemptAt.Hour() != 7 {
			t.Fatalf("not held until 07:00: %+v", d)
		}
	}
	hub.DeliverDue(ctx)
	if len(desktop.got) != 0 {
		t.Fatal("delivered during quiet hours")
	}
	*clock = time.Date(2026, 10, 3, 7, 1, 0, 0, time.UTC)
	hub.DeliverDue(ctx)
	got, _ := hub.Get(ctx, n.ID)
	for _, d := range got.Deliveries {
		if d.Status != DeliveryDelivered {
			t.Fatalf("after quiet hours: %+v", d)
		}
	}
	if len(desktop.got) != 1 {
		t.Fatal("the desktop notice was not sent after quiet hours")
	}
}

// A destination receives only its categories and severities (§26), and
// repeats are counted on one notification (§22).
func TestPreferencesAndRepeats(t *testing.T) {
	hub, _, _ := deliveryHub(t)
	ctx := context.Background()
	d, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Failures"), Webhook: &WebhookConfig{URL: "https://example.com/h"},
		Categories: &[]string{CategoryAutomation}, MinSeverity: ptr(SeverityError)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		n    Notification
		want bool
	}{
		{Notification{Category: CategoryAutomation, Severity: SeverityError}, true},
		{Notification{Category: CategoryAutomation, Severity: SeveritySuccess}, false},
		{Notification{Category: CategoryModel, Severity: SeverityError}, false},
	} {
		if d.Accepts(tc.n) != tc.want {
			t.Errorf("%+v accepted = %v", tc.n, !tc.want)
		}
	}
	if _, _, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("x"), Webhook: &WebhookConfig{URL: "https://e.com"}, Categories: &[]string{"weather"}}); err == nil {
		t.Fatal("an unknown category was accepted")
	}

	for i := 0; i < 3; i++ {
		_, _ = hub.Notify(ctx, Request{Title: "Daily report failed", Severity: SeverityError, Category: CategoryAutomation, DedupeKey: "auto:1"})
	}
	list, _, _ := hub.List(ctx, false, 0, CategoryAutomation)
	if len(list) != 1 || list[0].RepeatCount != 3 {
		t.Fatalf("list = %+v", list)
	}
	if other, _, _ := hub.List(ctx, false, 0, CategoryModel); len(other) != 0 {
		t.Fatalf("category filter: %+v", other)
	}
}

// Removing a destination cancels what was waiting for it and its secret.
func TestDeleteDestination(t *testing.T) {
	hub, _, _ := deliveryHub(t)
	ctx := context.Background()
	d, _, _ := hub.CreateDestination(ctx, DestinationInput{Kind: KindWebhook, Name: ptr("Hook"), Webhook: &WebhookConfig{URL: "https://example.com/h"}})
	n, _ := hub.Notify(ctx, Request{Title: "Waiting"})
	if err := hub.DeleteDestination(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if dl := deliveryOf(t, hub, n.ID); dl.Status != DeliveryCancelled {
		t.Fatalf("delivery %+v", dl)
	}
	if _, err := hub.secrets.Read(secretName(d.ID)); err == nil {
		t.Fatal("the secret was kept")
	}
	if err := hub.DeleteDestination(ctx, d.ID); err != ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
}
