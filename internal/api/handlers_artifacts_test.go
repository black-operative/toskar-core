package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/store"
)

func TestArtifactRoutes(t *testing.T) {
	srv := NewServer(Dependencies{})
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files := artifacts.NewStore(db.SQL, t.TempDir())
	srv.BindArtifacts(files)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}

	rec := do(http.MethodPost, "/api/v1/artifacts", `{"name":"notes.md","text":"# Plan\n\nShip it."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var a artifacts.Artifact
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if a.Name != "notes.md" || a.Producer != "user" {
		t.Fatalf("uploaded %+v", a)
	}

	rec = do(http.MethodGet, "/api/v1/artifacts/"+a.ID+"/content", "")
	if rec.Body.String() != "# Plan\n\nShip it." ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("content: %d %v %q", rec.Code, rec.Header(), rec.Body)
	}

	// HTML never renders on this origin, even when asked to show inline.
	html, _ := files.Save(t.Context(), artifacts.Input{Name: "page.html", Producer: "assistant", Data: []byte("<script>alert(1)</script>")})
	rec = do(http.MethodGet, "/api/v1/artifacts/"+html.ID+"/content?inline=1", "")
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("html disposition = %q", rec.Header().Get("Content-Disposition"))
	}

	if rec = do(http.MethodPost, "/api/v1/artifacts", `{"name":"movie.mp4","content_base64":"AAAA"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "can't read movie.mp4") {
		t.Fatalf("unsupported: %d %s", rec.Code, rec.Body)
	}
	bad := base64.StdEncoding.EncodeToString([]byte("not a pdf"))
	if rec = do(http.MethodPost, "/api/v1/artifacts", `{"name":"scan.pdf","content_base64":"`+bad+`"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "can't read scan.pdf") {
		t.Fatalf("unreadable: %d %s", rec.Code, rec.Body)
	}

	if rec = do(http.MethodDelete, "/api/v1/artifacts/"+a.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec = do(http.MethodGet, "/api/v1/artifacts/"+a.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("after delete: %d", rec.Code)
	}
}
