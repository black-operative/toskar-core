package internet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/yeixio/toskar-core/internal/netguard"
)

const maxSearchBytes = 1 << 20
const maxPageBytes = 1 << 20
const maxPageRunes = 8000

// DuckDuckGo searches the public HTML results page. No API key.
type DuckDuckGo struct {
	Client *http.Client
}

func (d DuckDuckGo) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 12 * time.Second}
}

func (d DuckDuckGo) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	body, err := get(ctx, d.client(), endpoint, maxSearchBytes)
	if err != nil {
		return nil, err
	}
	results := ParseDuckHTML(body)
	if results == nil {
		results = []Result{}
	}
	if len(results) > 8 {
		results = results[:8]
	}
	return results, nil
}

// ParseDuckHTML extracts result rows from a DuckDuckGo HTML page.
func ParseDuckHTML(page string) []Result {
	node, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil
	}
	var results []Result
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" && classContains(n, "result__a") {
			title := strings.TrimSpace(textOf(n))
			href := attr(n, "href")
			link := unwrapDuckLink(href)
			if title != "" && link != "" {
				results = append(results, Result{Title: title, URL: link, Source: hostOf(link)})
			}
		}
		if n.Type == html.ElementNode && classContains(n, "result__snippet") && len(results) > 0 {
			snippet := strings.TrimSpace(textOf(n))
			if results[len(results)-1].Snippet == "" {
				results[len(results)-1].Snippet = snippet
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return results
}

func unwrapDuckLink(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if uddg := parsed.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			return decoded
		}
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return parsed.String()
	}
	return ""
}

// HTTPFetcher downloads a page and returns readable text. It opens only
// public addresses: a page on this computer or the local network is
// refused, including through a redirect.
type HTTPFetcher struct {
	// Guard checks every connection; nil uses the system's resolver.
	Guard *netguard.Guard
}

func (f HTTPFetcher) client() *http.Client {
	guard := f.Guard
	if guard == nil {
		guard = &netguard.Guard{}
	}
	return guard.Client(15 * time.Second)
}

func (f HTTPFetcher) Open(ctx context.Context, rawURL string) (Page, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Page{}, errString("url must be http or https")
	}
	body, err := get(ctx, f.client(), parsed.String(), maxPageBytes)
	if errors.Is(err, netguard.ErrPrivate) {
		return Page{}, netguard.ErrPrivate
	}
	if err != nil {
		return Page{}, err
	}
	title, content := extractText(body)
	if strings.TrimSpace(content) == "" {
		return Page{}, errString("page had no readable text")
	}
	return Page{
		Title:   title,
		URL:     parsed.String(),
		Content: content,
		Metadata: map[string]any{
			"host": parsed.Host,
		},
	}, nil
}

func extractText(page string) (string, string) {
	node, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return "", ""
	}
	var title string
	var b strings.Builder
	var walk func(*html.Node, bool)
	skipTags := map[string]struct{}{"script": {}, "style": {}, "noscript": {}, "nav": {}, "footer": {}, "svg": {}}
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			if _, drop := skipTags[n.Data]; drop {
				skip = true
			}
			if n.Data == "title" && title == "" {
				title = strings.TrimSpace(textOf(n))
			}
		}
		if n.Type == html.TextNode && !skip {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}
	walk(node, false)
	content := collapseBlank(b.String())
	if utf8.RuneCountInString(content) > maxPageRunes {
		content = string([]rune(content)[:maxPageRunes]) + "\n…"
	}
	if title == "" {
		title = "Untitled page"
	}
	return title, content
}

func get(ctx context.Context, client *http.Client, endpoint string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Toskar/0.1")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", errString(res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func classContains(n *html.Node, class string) bool {
	value := attr(n, "class")
	for _, part := range strings.Fields(value) {
		if part == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func collapseBlank(value string) string {
	lines := strings.Split(value, "\n")
	var kept []string
	blank := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
			continue
		}
		blank = 0
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

type errString string

func (e errString) Error() string { return string(e) }
