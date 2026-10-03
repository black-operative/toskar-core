package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
)

// A bundle carries the runtime summary and profiles for memory problems.
func TestBundleIncludesProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.zip")
	if err := WriteBundle(path, Options{Config: config.Config{LogsDir: dir}}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	for _, name := range []string{"runtime.json", "profiles/goroutines.txt", "profiles/heap.pb.gz"} {
		if files[name] == nil {
			t.Fatalf("bundle lacks %s", name)
		}
	}
	rc, _ := files["runtime.json"].Open()
	var summary map[string]any
	_ = json.NewDecoder(rc).Decode(&summary)
	rc.Close()
	if n, _ := summary["goroutines"].(float64); n < 1 {
		t.Fatalf("runtime.json = %v", summary)
	}
	rc, _ = files["profiles/goroutines.txt"].Open()
	text, _ := io.ReadAll(rc)
	rc.Close()
	if !strings.HasPrefix(string(text), "goroutine profile:") {
		t.Fatalf("goroutines.txt starts %q", string(text[:min(len(text), 40)]))
	}
}

// Live profiles are served only on this computer, and stop with the daemon.
func TestServeProfilesOnLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.5:0", ":0"} {
		if _, err := ServeProfiles(context.Background(), addr); err == nil {
			t.Fatalf("%s was accepted", addr)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	got, err := ServeProfiles(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + got.String() + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "goroutine profile") {
		t.Fatalf("status %d body %.60q", resp.StatusCode, body)
	}
	http.DefaultClient.CloseIdleConnections()
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := http.Get("http://" + got.String() + "/debug/pprof/"); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still serving after the context ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
