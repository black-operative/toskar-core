package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A webhook needs no API key, even with the API on the network: its token
// is its proof. Wrong tokens, big bodies, and calls too soon get their own
// answers (#204).
func TestHookEndpoint(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	var got []byte
	srv := NewServer(Dependencies{
		Config: mgr,
		RunHook: func(_ context.Context, token string, body []byte) (automations.Run, error) {
			switch token {
			case "good":
				got = body
				return automations.Run{ID: "run-1"}, nil
			case "soon":
				return automations.Run{}, contracts.NewError("HOOK_TOO_SOON", nil, errors.New("too soon"))
			}
			return automations.Run{}, errors.New("not found")
		},
	})
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.RemoteAddr = "192.168.1.20:5000"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := post("/hooks/good", `{"order":42}`); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "run-1") || string(got) != `{"order":42}` {
		t.Fatalf("good: %d %s", rec.Code, rec.Body)
	}
	if rec := post("/hooks/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}
	if rec := post("/hooks/soon", ""); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("soon: %d", rec.Code)
	}
	if rec := post("/hooks/good", strings.Repeat("x", maxHookBody+1)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: %d", rec.Code)
	}
	// Making a link is the API's, so it still needs a key on the network.
	if rec := post("/api/v1/automations/a1/hook", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("make link without a key: %d", rec.Code)
	}
}
