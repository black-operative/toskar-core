package diagnostics

import (
	"archive/zip"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	runpprof "runtime/pprof"
	"time"
)

// writeProfiles adds what diagnosing memory growth needs to a bundle
// (#231): a runtime summary, the goroutines grouped by stack, and a heap
// profile. They hold function names, counts, and sizes; no prompts, files,
// or credentials.
func writeProfiles(zw *zip.Writer) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	summary := map[string]any{
		"goroutines":      runtime.NumGoroutine(),
		"heap_alloc":      m.HeapAlloc,
		"heap_sys":        m.HeapSys,
		"heap_objects":    m.HeapObjects,
		"sys":             m.Sys,
		"num_gc":          m.NumGC,
		"pause_total_ms":  m.PauseTotalNs / 1e6,
		"go_version":      runtime.Version(),
		"gomaxprocs":      runtime.GOMAXPROCS(0),
		"collected_at":    time.Now().UTC().Format(time.RFC3339),
		"profiles_folder": "profiles/ (goroutines.txt; heap.pb.gz for go tool pprof)",
	}
	if err := writeJSON(zw, "runtime.json", summary); err != nil {
		return err
	}
	// debug=1 groups identical stacks with a count, without argument values.
	for _, p := range []struct {
		name, file string
		debug      int
	}{
		{"goroutine", "profiles/goroutines.txt", 1},
		{"heap", "profiles/heap.pb.gz", 0},
	} {
		w, err := zw.Create(p.file)
		if err != nil {
			return err
		}
		if prof := runpprof.Lookup(p.name); prof != nil {
			if err := prof.WriteTo(w, p.debug); err != nil {
				return err
			}
		}
	}
	return nil
}

// ServeProfiles serves Go's live profiles (net/http/pprof) on addr for
// diagnosing a running daemon, such as TOSKAR_PPROF=127.0.0.1:6060. It
// refuses any address that isn't on this computer, since profiles show the
// program's internals. The server stops when ctx ends.
func ServeProfiles(ctx context.Context, addr string) (net.Addr, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("profiles address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("profiles are served only on this computer (127.0.0.1 or ::1), not %q", host)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	return ln.Addr(), nil
}
