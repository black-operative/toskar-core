package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// The web UI is a single-page app: files that exist are served as they are,
// and any other path gets index.html so the app can route it. Files inside a
// folder, like /assets/app.js, must be found on every OS. On Windows the lookup
// once used backslashes, which an fs.FS rejects, so the scripts and styles came
// back as the start page and the UI stayed blank.
func TestSPAFallbackServesFilesInFolders(t *testing.T) {
	root := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html>app")},
		"favicon.svg":      {Data: []byte("<svg/>")},
		"assets/app.js":    {Data: []byte("console.log('app')")},
		"assets/app.css":   {Data: []byte("body{}")},
		"assets/img/a.png": {Data: []byte("png")},
	}
	h := spaFallback(root, http.FileServer(http.FS(root)))

	tests := []struct {
		name string
		path string
		want string
	}{
		{"root", "/", "<!doctype html>app"},
		{"file in the top folder", "/favicon.svg", "<svg/>"},
		{"script in a folder", "/assets/app.js", "console.log('app')"},
		{"style in a folder", "/assets/app.css", "body{}"},
		{"file two folders deep", "/assets/img/a.png", "png"},
		{"client-side route", "/settings", "<!doctype html>app"},
		{"missing file in a folder", "/assets/missing.js", "<!doctype html>app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Body.String(); got != tt.want {
				t.Fatalf("GET %s: body %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
