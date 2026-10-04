package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/yeixio/toskar-core/internal/netguard"
)

// maxDownload is the largest file brought into a chat.
const maxDownload = 25 << 20

// client fetches downloads. Its dialer refuses private addresses too, so a
// redirect or a changed DNS answer cannot reach the local network.
func (m *Manager) client() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		a, err := netip.ParseAddr(host)
		if err == nil && netguard.Private(a) && (m.Guard.Allow == nil || !m.Guard.Allow(host)) {
			return ErrPrivate
		}
		return nil
	}}
	return &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return m.Guard.CheckURL(req.Context(), req.URL.String())
		}}
}

// Download fetches a file the page links to, with the page's cookies, and
// returns its name and bytes.
func (m *Manager) Download(ctx context.Context, key, raw string) (string, []byte, error) {
	if err := m.Guard.CheckURL(ctx, raw); err != nil {
		return "", nil, err
	}
	var cookies []*network.Cookie
	var agent string
	_ = m.run(ctx, key, 15*time.Second, func(ctx context.Context) error {
		_ = chromedp.Run(ctx, chromedp.Evaluate(`navigator.userAgent`, &agent))
		return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().WithURLs([]string{raw}).Do(ctx)
			return err
		}))
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", nil, err
	}
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	if agent != "" {
		req.Header.Set("User-Agent", agent)
	}
	m.opened(ctx, raw)
	resp, err := m.client().Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivate) {
			return "", nil, ErrPrivate
		}
		return "", nil, fmt.Errorf("the file could not be downloaded: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("the file could not be downloaded: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxDownload {
		return "", nil, fmt.Errorf("the file is larger than %d MB", maxDownload>>20)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) > maxDownload {
		return "", nil, fmt.Errorf("the file is larger than %d MB", maxDownload>>20)
	}
	return fileName(resp, raw), data, nil
}

// fileName is the name a server gives a download, or the address's last
// part.
func fileName(resp *http.Response, raw string) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return path.Base(params["filename"])
	}
	if u, err := url.Parse(raw); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" && base != "" {
			return base
		}
	}
	name := "download"
	if exts, _ := mime.ExtensionsByType(strings.Split(resp.Header.Get("Content-Type"), ";")[0]); len(exts) > 0 {
		name += exts[0]
	}
	return name
}
