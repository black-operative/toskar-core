package hfclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/internal/models/hfclient"
)

func TestSearchFiltersGGUF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":        "org/Cool-Model-GGUF",
				"downloads": 1000,
				"tags":      []string{"gguf", "text-generation"},
				"siblings": []map[string]any{
					{"rfilename": "Cool-Model-Q4_K_M.gguf", "size": 4_000_000_000},
					{"rfilename": "Cool-Model-Q8_0.gguf", "size": 8_000_000_000},
				},
			},
		})
	}))
	defer srv.Close()

	c := hfclient.New()
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()

	out, err := c.Search(context.Background(), "cool", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1, got %d", len(out))
	}
	if out[0].Variant != "Q4_K_M" {
		t.Fatalf("preferred quant, got %s", out[0].Variant)
	}
	if out[0].SourceURL == "" {
		t.Fatal("expected source url")
	}
}
