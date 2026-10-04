package gjallarhorn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Push goes to a topic on an ntfy server with the notification's priority,
// and opens Yggdrasil when tapped (§10).
func TestNtfyPush(t *testing.T) {
	var got ntfyMessage
	var auth string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	hub, eg, _ := deliveryHub(t)
	ctx := context.Background()
	d, secret, err := hub.CreateDestination(ctx, DestinationInput{Kind: KindNtfy, Name: ptr("Phone"), Password: "tk_123",
		Ntfy: &NtfyConfig{Server: srv.URL + "/", Topic: "yggdrasil-home", OpenURL: "http://192.168.1.10:7331/"}})
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" || !d.HasSecret || d.Ntfy.Content != "full" || d.Ntfy.Server != srv.URL {
		t.Fatalf("destination %+v secret %q", d.Ntfy, secret)
	}
	n, _ := hub.Notify(ctx, Request{Title: "Backup failed", Body: "The disk is full.", Severity: SeverityError, Link: "/automations?id=a1"})
	hub.DeliverDue(ctx)
	if dl := deliveryOf(t, hub, n.ID); dl.Status != DeliveryDelivered {
		t.Fatalf("delivery %+v", dl)
	}
	if got.Topic != "yggdrasil-home" || got.Title != "Backup failed" || got.Message != "The disk is full." || got.Priority != 4 ||
		got.Click != "http://192.168.1.10:7331/automations?id=a1" {
		t.Fatalf("published %+v", got)
	}
	if auth != "Bearer tk_123" {
		t.Fatalf("authorization %q", auth)
	}
	if len(eg.adds) != 1 || !strings.Contains(eg.adds[0], "Phone: Backup failed") {
		t.Fatalf("egress %v", eg.adds)
	}

	status = http.StatusForbidden
	err = hub.TestDestination(ctx, d.ID)
	if !IsPermanent(err) || !strings.Contains(err.Error(), "access token") {
		t.Fatalf("refused: %v", err)
	}
}

// On the public server, where anyone who knows a topic can read it, only a
// generic notice is sent unless you choose full content (§11).
func TestNtfyPrivateOnPublicServer(t *testing.T) {
	c := NtfyConfig{Topic: "yggdrasil-x"}
	applyNtfyDefaults(&c)
	if c.Server != DefaultNtfyServer || c.Content != "private" {
		t.Fatalf("defaults %+v", c)
	}
	m := ntfyMessageFor(c, Notification{Title: "Your bank balance", Body: "secret", Severity: SeverityInfo})
	if m.Title != "Toskar" || strings.Contains(m.Message, "secret") || m.Priority != 2 {
		t.Fatalf("private message %+v", m)
	}
	own := NtfyConfig{Server: "https://ntfy.example.com", Topic: "t"}
	applyNtfyDefaults(&own)
	if own.Content != "full" {
		t.Fatalf("own server defaults to %q", own.Content)
	}
	for _, bad := range []NtfyConfig{
		{Server: "http://ntfy.example.com", Topic: "t", Content: "full"},
		{Server: "https://ntfy.sh", Topic: "has space", Content: "full"},
		{Server: "https://ntfy.sh", Topic: "t", Content: "loud"},
		{Server: "https://ntfy.sh", Topic: "t", Content: "full", OpenURL: "ftp://x"},
	} {
		if err := validateNtfy(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}
