package internet

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/internal/netguard"
)

func TestParseDuckHTML(t *testing.T) {
	page := `<html><body>
<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fweather.example%2Fjuneau">Juneau Weather</a>
<a class="result__snippet">Current conditions in Juneau.</a>
</body></html>`
	results := ParseDuckHTML(page)
	if len(results) != 1 {
		t.Fatalf("results %#v", results)
	}
	if results[0].URL != "https://weather.example/juneau" || !strings.Contains(results[0].Snippet, "Current") {
		t.Fatalf("result %#v", results[0])
	}
}

func TestExtractTextSkipsNavigation(t *testing.T) {
	page := `<html><head><title>Docs</title></head><body><nav>Home</nav><main><p>Install the package.</p></main><script>junk()</script></body></html>`
	title, content := extractText(page)
	if title != "Docs" || !strings.Contains(content, "Install the package") || strings.Contains(content, "junk") || strings.Contains(content, "Home") {
		t.Fatalf("title=%q content=%q", title, content)
	}
}

func TestOpenRefusesThisComputer(t *testing.T) {
	var hit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		_, _ = io.WriteString(w, "<html><body><p>secret</p></body></html>")
	}))
	defer server.Close()
	for _, raw := range []string{server.URL + "/api/v1/health", "http://localhost:7331/", "http://169.254.169.254/latest/meta-data/"} {
		_, err := HTTPFetcher{}.Open(context.Background(), raw)
		if !errors.Is(err, netguard.ErrPrivate) || err.Error() != netguard.ErrPrivate.Error() {
			t.Fatalf("Open(%s) err = %v, want %v", raw, err, netguard.ErrPrivate)
		}
	}
	if hit.Load() {
		t.Fatal("the local server was reached")
	}
}

func TestOpenReadsPublicPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html><head><title>News</title></head><body><p>Today's story.</p></body></html>")
	}))
	defer server.Close()
	guard := &netguard.Guard{
		Resolver: publicResolver{},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	page, err := HTTPFetcher{Guard: guard}.Open(context.Background(), "http://news.example/today")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "News" || !strings.Contains(page.Content, "Today's story.") {
		t.Fatalf("page %#v", page)
	}
}

// publicResolver answers every name with a public documentation address.
type publicResolver struct{}

func (publicResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
}
