// Package soak runs an in-process daemon through many chats, cancellations,
// and event-stream connections, and fails if goroutines, heap, or open files
// keep growing after warm-up (#231). It is slow, so it runs only when asked:
//
//	TOSKAR_SOAK=1 go test ./tests/soak -run TestSoak -v
//	TOSKAR_SOAK=1 TOSKAR_SOAK_ROUNDS=1000 go test ./tests/soak -v -timeout 60m
package soak_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
	"github.com/yeixio/toskar-core/internal/config"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

type sample struct {
	goroutines int
	heap       uint64
	files      int
}

func measure() sample {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	files := -1
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		// Names only: stat-ing each descriptor fails on macOS's /dev/fd.
		if f, err := os.Open(dir); err == nil {
			names, err := f.Readdirnames(-1)
			_ = f.Close()
			if err == nil {
				files = len(names)
				break
			}
		}
	}
	return sample{goroutines: runtime.NumGoroutine(), heap: m.HeapAlloc, files: files}
}

func TestSoak(t *testing.T) {
	if config.Env("SOAK") != "1" {
		t.Skip("set TOSKAR_SOAK=1 to run the soak test")
	}
	// About 2.5 seconds a round with the stub model: the default fits Go's
	// 10-minute test timeout.
	rounds := 120
	if n, err := strconv.Atoi(config.Env("SOAK_ROUNDS")); err == nil && n > 0 {
		rounds = n
	}

	// A throwaway daemon: its own data directory, free ports, no discovery,
	// and the stub model, so nothing reaches a real model or the network.
	t.Setenv("TOSKAR_API_PORT", freePort(t))
	t.Setenv("TOSKAR_INTERNAL_PORT", freePort(t))
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	t.Setenv("TOSKAR_STUB_INFERENCE", "true")
	dir := t.TempDir()
	d, err := app.New(app.Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Config.Get().DataDir; filepath.Clean(got) != filepath.Clean(dir) {
		t.Fatalf("data dir is %q, not the test's %q", got, dir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Start(ctx) }()
	defer func() {
		cancel()
		_ = d.Shutdown(context.Background())
	}()
	base := "http://" + d.Config.Get().APIAddr()
	client := &http.Client{Timeout: 30 * time.Second}
	waitHealthy(t, client, base)

	warmup := rounds / 10
	var before sample
	for i := range rounds {
		if i == warmup {
			client.CloseIdleConnections()
			before = measure()
		}
		round(t, client, base, i)
	}
	client.CloseIdleConnections()
	// Give finished streams and turns a moment to wind down.
	time.Sleep(2 * time.Second)
	after := measure()
	t.Logf("after warm-up: %d goroutines, %d KiB heap, %d files", before.goroutines, before.heap/1024, before.files)
	t.Logf("after %d rounds: %d goroutines, %d KiB heap, %d files", rounds, after.goroutines, after.heap/1024, after.files)

	// Some slack for caches filling and pools settling; a leak grows with
	// the number of rounds and blows through it.
	if grew := after.goroutines - before.goroutines; grew > 25 {
		t.Errorf("goroutines grew by %d over %d rounds", grew, rounds-warmup)
	}
	if after.heap > before.heap*2+32<<20 {
		t.Errorf("heap grew from %d KiB to %d KiB", before.heap/1024, after.heap/1024)
	}
	if before.files >= 0 && after.files-before.files > 20 {
		t.Errorf("open files grew from %d to %d", before.files, after.files)
	}
}

func waitHealthy(t *testing.T, client *http.Client, base string) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		resp, err := client.Get(base + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
	}
	t.Fatal("the daemon never became healthy")
}

// round is one cycle of the work a daemon does for weeks: a chat, a
// streamed chat cancelled by the client, a stopped turn, and an event
// subscriber that comes and goes.
func round(t *testing.T, client *http.Client, base string, i int) {
	t.Helper()
	conv := createConversation(t, client, base, i)

	// A whole answer.
	post(t, client, base+"/api/v1/chat", map[string]any{"conversation_id": conv, "message": fmt.Sprintf("Hello %d", i), "stream": false})

	// A streamed answer the client walks away from after the first event.
	sctx, scancel := context.WithCancel(context.Background())
	body, _ := json.Marshal(map[string]any{"conversation_id": conv, "message": "Tell me a long story", "stream": true})
	req, _ := http.NewRequestWithContext(sctx, http.MethodPost, base+"/api/v1/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := client.Do(req); err == nil {
		_, _ = bufio.NewReader(resp.Body).ReadString('\n')
		scancel()
		_ = resp.Body.Close()
	}
	scancel()

	// Stopping a conversation, running or not.
	post(t, client, base+"/api/v1/chat/stop", map[string]any{"conversation_id": conv})

	// An event subscriber that reads a moment and leaves.
	ectx, ecancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	ereq, _ := http.NewRequestWithContext(ectx, http.MethodGet, base+"/api/v1/events", nil)
	if resp, err := client.Do(ereq); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	ecancel()

	// Every tenth round, delete the chat, as people tidy up.
	if i%10 == 9 {
		dreq, _ := http.NewRequest(http.MethodDelete, base+"/api/v1/conversations/"+conv, nil)
		if resp, err := client.Do(dreq); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
}

func createConversation(t *testing.T, client *http.Client, base string, i int) string {
	t.Helper()
	raw := post(t, client, base+"/api/v1/conversations", map[string]any{"title": fmt.Sprintf("Soak %d", i)})
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
		t.Fatalf("create conversation: %s", strings.TrimSpace(string(raw)))
	}
	return out.ID
}

func post(t *testing.T, client *http.Client, url string, v any) []byte {
	t.Helper()
	body, _ := json.Marshal(v)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		t.Fatalf("POST %s: %d %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw
}
