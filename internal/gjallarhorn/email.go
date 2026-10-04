package gjallarhorn

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

func validateEmail(c EmailConfig) error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("the SMTP server is required")
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("the SMTP port must be from 1 to 65535")
	}
	switch c.TLS {
	case "", "starttls", "tls":
	case "none":
		// A password sent without TLS can be read on the network.
		if !loopback(c.Host) {
			return fmt.Errorf("an SMTP server without TLS is allowed only on this computer")
		}
	default:
		return fmt.Errorf("tls must be starttls, tls, or none")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("the sender %q is not an email address", c.From)
	}
	if len(c.To) == 0 {
		return fmt.Errorf("add at least one recipient")
	}
	if len(c.To) > 20 {
		return fmt.Errorf("an email destination can have up to 20 recipients")
	}
	for _, to := range c.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("the recipient %q is not an email address", to)
		}
	}
	return nil
}

func loopback(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// smtpDial is the network dial, replaceable in tests.
var smtpDial = func(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// emailTLSConfig is the TLS configuration for a server, replaceable in tests.
var emailTLSConfig = func(host string) *tls.Config { return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12} }

// sendEmail sends a notification through the destination's SMTP server (§12).
func sendEmail(ctx context.Context, cfg EmailConfig, password string, n Notification, now time.Time) error {
	if err := validateEmail(cfg); err != nil {
		return PermanentError{Err: err}
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := smtpDial(ctx, addr)
	if err != nil {
		return fmt.Errorf("could not reach the SMTP server: %s", shortNetErr(err))
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if cfg.TLS == "tls" {
		tconn := tls.Client(conn, emailTLSConfig(cfg.Host))
		if err := tconn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return tlsFailure(err)
		}
		conn = tconn
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return smtpFailure(err)
	}
	defer c.Close()
	if cfg.TLS == "" || cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return permanent("the SMTP server does not offer STARTTLS; choose TLS, or a server that supports it")
		}
		if err := c.StartTLS(emailTLSConfig(cfg.Host)); err != nil {
			return tlsFailure(err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return permanent("the SMTP server does not accept a sign-in; clear the username, or use another server")
		}
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, password, cfg.Host)); err != nil {
			return smtpFailure(err)
		}
	}
	from, _ := mail.ParseAddress(cfg.From)
	if err := c.Mail(from.Address); err != nil {
		return smtpFailure(err)
	}
	for _, to := range cfg.To {
		a, _ := mail.ParseAddress(to)
		if err := c.Rcpt(a.Address); err != nil {
			return smtpFailure(err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return smtpFailure(err)
	}
	if _, err := w.Write(emailMessage(cfg, n, now)); err != nil {
		return smtpFailure(err)
	}
	if err := w.Close(); err != nil {
		return smtpFailure(err)
	}
	_ = c.Quit()
	return nil
}

// emailMessage is a plain text email for a notification.
func emailMessage(cfg EmailConfig, n Notification, now time.Time) []byte {
	var b bytes.Buffer
	subject := n.Title
	if n.RepeatCount > 1 {
		subject = n.text("notifications:sent.times", map[string]any{"title": n.Title, "count": n.RepeatCount})
	}
	host := "yggdrasil.local"
	if from, err := mail.ParseAddress(cfg.From); err == nil {
		if i := strings.LastIndexByte(from.Address, '@'); i >= 0 {
			host = from.Address[i+1:]
		}
	}
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", cfg.From)
	header("To", strings.Join(cfg.To, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", "Toskar: "+subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+n.ID+"@"+host+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	text := n.Title + "\n\n"
	if n.Body != "" {
		text += n.Body + "\n\n"
	}
	// The date is written the same way in every language: Go has no
	// localized month names.
	text += fmt.Sprintf("%s · %s · %s\n", n.text("notifications:categories."+n.Category, nil),
		n.text("notifications:sent.severities."+n.Severity, nil), n.CreatedAt.Local().Format("2006-01-02 15:04"))
	text += "\n" + n.text("notifications:sent.footer", nil) + "\n"
	_, _ = qp.Write([]byte(strings.ReplaceAll(text, "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// smtpFailure sorts an SMTP error: 5xx replies are permanent (bad sign-in,
// unknown recipient), 4xx and network errors are retried.
func smtpFailure(err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		switch {
		case tp.Code == 530 || tp.Code == 534 || tp.Code == 535:
			return permanent("the SMTP server did not accept the username and password (%d)", tp.Code)
		case tp.Code >= 500:
			return permanent("the SMTP server refused the message: %d %s", tp.Code, tp.Msg)
		default:
			return fmt.Errorf("the SMTP server is busy: %d %s", tp.Code, tp.Msg)
		}
	}
	if strings.Contains(err.Error(), "unencrypted connection") {
		return permanent("the password would be sent without encryption; use STARTTLS or TLS")
	}
	return fmt.Errorf("the SMTP connection failed: %s", shortNetErr(err))
}

func tlsFailure(err error) error {
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return permanent("the SMTP server's certificate is not trusted: %s", shortNetErr(cert.Err))
	}
	return fmt.Errorf("the TLS connection to the SMTP server failed: %s", shortNetErr(err))
}
