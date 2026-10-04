package connectors

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/server"

	"github.com/yeixio/toskar-core/internal/tools"
)

// fakeSMTP accepts one message at a time and keeps what it was sent.
type fakeSMTP struct {
	mu   sync.Mutex
	from string
	rcpt []string
	data string
	auth string
}

func (f *fakeSMTP) serve(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.handle(conn)
		}
	}()
	return l.Addr().String()
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	say("220 localhost ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		f.mu.Lock()
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-localhost")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			f.auth = strings.TrimSpace(line[len("AUTH PLAIN"):])
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.from = strings.TrimSpace(line[len("MAIL FROM:"):])
			f.rcpt = nil
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			f.rcpt = append(f.rcpt, strings.TrimSpace(line[len("RCPT TO:"):]))
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.data = b.String()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			f.mu.Unlock()
			return
		default:
			say("250 ok")
		}
		f.mu.Unlock()
	}
}

const (
	aliceMail = "From: Alice <alice@example.org>\r\nTo: me@example.org\r\nSubject: Invoice 42\r\n" +
		"Date: Thu, 01 Oct 2026 09:00:00 +0000\r\nMessage-ID: <alice-1@example.org>\r\nContent-Type: text/plain\r\n\r\nPlease pay invoice 42 by Friday.\r\n"
	bobMail = "From: Bob <bob@example.org>\r\nTo: me@example.org\r\nSubject: Quarterly report\r\n" +
		"Date: Fri, 02 Oct 2026 08:00:00 +0000\r\nMessage-ID: <bob-1@example.org>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=XX\r\n\r\n--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Hi,</p><p>The <b>report</b> is attached.</p><script>evil()</script>\r\n--XX\r\nContent-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=report.pdf\r\n\r\nPDFDATA\r\n--XX--\r\n"
)

// mailServer is an IMAP server with an inbox of three messages (one read),
// and Drafts, Sent, and Archive folders.
func mailServer(t *testing.T) (Credential, *fakeSMTP) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := server.New(memory.New())
	s.AllowInsecureAuth = true
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { s.Close() })
	c, err := client.Dial(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer logout(c)
	if err := c.Login("username", "password"); err != nil {
		t.Fatal(err)
	}
	for _, box := range []string{"Drafts", "Sent", "Archive"} {
		if err := c.Create(box); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []string{aliceMail, bobMail} {
		if err := c.Append("INBOX", nil, time.Now(), bytes.NewBufferString(m)); err != nil {
			t.Fatal(err)
		}
	}
	smtpSrv := &fakeSMTP{}
	return Credential{"address": "me@example.org", "username": "username", "password": "password", "name": "Me",
		"imap": l.Addr().String(), "smtp": smtpSrv.serve(t)}, smtpSrv
}

func runTool(t *testing.T, svc Service, id string, cred Credential, args map[string]any) (map[string]any, error) {
	t.Helper()
	for _, tool := range svc.Tools() {
		if tool.Def.ID == id {
			return tool.Run(context.Background(), nil, cred, args)
		}
	}
	t.Fatalf("no tool %s", id)
	return nil, nil
}

func messages(t *testing.T, res map[string]any) []map[string]any {
	t.Helper()
	rows, _ := res["messages"].([]map[string]any)
	return rows
}

// boxState counts a folder's messages and those marked deleted.
func boxState(t *testing.T, cred Credential, box string) (total, deleted, unseen int) {
	t.Helper()
	c, err := client.Dial(cred["imap"])
	if err != nil {
		t.Fatal(err)
	}
	defer logout(c)
	_ = c.Login("username", "password")
	mb, err := c.Select(box, true)
	if err != nil {
		t.Fatal(err)
	}
	set := new(imap.SeqSet)
	set.AddRange(1, mb.Messages)
	ch := make(chan *imap.Message, 10)
	if mb.Messages > 0 {
		if err := c.Fetch(set, []imap.FetchItem{imap.FetchFlags}, ch); err != nil {
			t.Fatal(err)
		}
	} else {
		close(ch)
	}
	for m := range ch {
		total++
		if hasFlag(m.Flags, imap.DeletedFlag) {
			deleted++
		}
		if !hasFlag(m.Flags, imap.SeenFlag) {
			unseen++
		}
	}
	return
}

func TestEmailSearchAndRead(t *testing.T) {
	cred, _ := mailServer(t)
	e := Email{}
	if account, err := e.Check(context.Background(), nil, cred); err != nil || account != "me@example.org" {
		t.Fatalf("check: %q %v", account, err)
	}
	res, err := runTool(t, e, "email.search", cred, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	rows := messages(t, res)
	if len(rows) != 3 || rows[0]["subject"] != "Quarterly report" || rows[1]["from"] != "Alice <alice@example.org>" || res["note"] == nil {
		t.Fatalf("search %v", rows)
	}
	for args, want := range map[string]int{`from`: 1, `unread`: 2, `query`: 1} {
		a := map[string]any{"from": "alice"}
		switch args {
		case "unread":
			a = map[string]any{"unread_only": true}
		case "query":
			a = map[string]any{"query": "Invoice"}
		}
		res, err := runTool(t, e, "email.search", cred, a)
		if err != nil || len(messages(t, res)) != want {
			t.Errorf("%s: %v %v", args, messages(t, res), err)
		}
	}
	bob := rows[0]["id"].(string)
	res, err = runTool(t, e, "email.read", cred, map[string]any{"id": bob})
	if err != nil {
		t.Fatal(err)
	}
	text := res["text"].(string)
	if !strings.Contains(text, "The report is attached.") || strings.Contains(text, "evil") || strings.Contains(text, "<p>") {
		t.Fatalf("text %q", text)
	}
	if files, _ := res["attachments"].([]string); len(files) != 1 || files[0] != "report.pdf" {
		t.Fatalf("attachments %v", res["attachments"])
	}
	// Reading did not mark it read.
	if _, _, unseen := boxState(t, cred, "INBOX"); unseen != 2 {
		t.Fatalf("unseen after reading: %d", unseen)
	}
	if _, err := runTool(t, e, "email.read", cred, map[string]any{"id": "INBOX"}); err == nil {
		t.Error("a bad id was accepted")
	}
}

// A reply goes to the sender, threads under the message, and is kept in
// Sent; a draft goes to Drafts.
func TestEmailSendAndDraft(t *testing.T) {
	cred, smtpSrv := mailServer(t)
	e := Email{}
	res, _ := runTool(t, e, "email.search", cred, map[string]any{"from": "alice"})
	alice := messages(t, res)[0]["id"].(string)
	out, err := runTool(t, e, "email.send", cred, map[string]any{"reply_to": alice, "body": "Paid today.\nThanks!"})
	if err != nil {
		t.Fatal(err)
	}
	smtpSrv.mu.Lock()
	data, rcpt, from := smtpSrv.data, smtpSrv.rcpt, smtpSrv.from
	smtpSrv.mu.Unlock()
	if out["sent"] != true || from != "<me@example.org>" || len(rcpt) != 1 || rcpt[0] != "<alice@example.org>" {
		t.Fatalf("envelope %v %q %v", out, from, rcpt)
	}
	for _, want := range []string{"Subject: Re: Invoice 42", "In-Reply-To: <alice-1@example.org>", "References: <alice-1@example.org>", `From: "Me" <me@example.org>`, "Paid today.\r\nThanks!"} {
		if !strings.Contains(data, want) {
			t.Errorf("message lacks %q:\n%s", want, data)
		}
	}
	if total, _, _ := boxState(t, cred, "Sent"); total != 1 {
		t.Errorf("sent copies: %d", total)
	}
	if _, err := runTool(t, e, "email.draft", cred, map[string]any{"to": "carol@example.org", "subject": "Lunch?", "body": "Friday at noon?"}); err != nil {
		t.Fatal(err)
	}
	if total, _, _ := boxState(t, cred, "Drafts"); total != 1 {
		t.Errorf("drafts: %d", total)
	}
	for _, bad := range []map[string]any{{"to": "not an address", "body": "x"}, {"to": "a@b.c"}, {"body": "x"}} {
		if _, err := runTool(t, e, "email.send", cred, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

// Archiving moves a message without expunging the folder, so nothing else
// marked deleted is lost.
func TestEmailArchiveNeverExpunges(t *testing.T) {
	cred, _ := mailServer(t)
	e := Email{}
	res, _ := runTool(t, e, "email.search", cred, map[string]any{"from": "alice"})
	alice := messages(t, res)[0]["id"].(string)
	out, err := runTool(t, e, "email.archive", cred, map[string]any{"id": alice})
	if err != nil || out["folder"] != "Archive" {
		t.Fatalf("archive %v %v", out, err)
	}
	if total, _, _ := boxState(t, cred, "Archive"); total != 1 {
		t.Fatalf("archived: %d", total)
	}
	if total, deleted, _ := boxState(t, cred, "INBOX"); total != 3 || deleted != 1 {
		t.Fatalf("inbox after archive: %d messages, %d marked deleted", total, deleted)
	}
}

func TestEmailPolicies(t *testing.T) {
	want := map[string]string{"email.search": tools.PolicyAllow, "email.read": tools.PolicyAllow, "email.draft": tools.PolicyAsk,
		"email.send": tools.PolicyAsk, "email.archive": tools.PolicyAsk}
	for _, tool := range (Email{}).Tools() {
		if want[tool.Def.ID] != tool.Def.DefaultPolicy {
			t.Errorf("%s: %s", tool.Def.ID, tool.Def.DefaultPolicy)
		}
		delete(want, tool.Def.ID)
	}
	if len(want) != 0 {
		t.Errorf("missing %v", want)
	}
	if !tlsFor("127.0.0.1").InsecureSkipVerify || tlsFor("imap.fastmail.com").InsecureSkipVerify {
		t.Error("certificate checks are skipped for a remote server")
	}
	if _, _, err := hostPort("https://imap.example.com", "993"); err == nil {
		t.Error("a URL was accepted as a server")
	}
}
