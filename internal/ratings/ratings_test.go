package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestIdentify(t *testing.T) {
	cases := []struct {
		m    contracts.Model
		want Identity
	}{
		{contracts.Model{Variant: "Q4_K_M", Source: contracts.ModelSource{URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-q4_k_m.gguf"}},
			Identity{ID: "qwen2.5-coder-7b-instruct", Format: "gguf", Quantization: "Q4_K_M", Runtime: "llamacpp", Backend: "metal"}},
		// Another uploader's conversion is the same model.
		{contracts.Model{Source: contracts.ModelSource{URL: "https://huggingface.co/bartowski/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/Qwen2.5-Coder-7B-Instruct-IQ4_XS.gguf", Format: "gguf"}},
			Identity{ID: "qwen2.5-coder-7b-instruct", Format: "gguf", Quantization: "IQ4_XS", Runtime: "llamacpp", Backend: "metal"}},
		{contracts.Model{Variant: "e2e", Runtime: []string{"llamacpp"}, Source: contracts.ModelSource{URL: "https://huggingface.co/x/Phi-3.5-mini-instruct-GGUF/resolve/main/Phi-3.5-mini-instruct-Q8_0.gguf"}},
			Identity{ID: "phi-3.5-mini-instruct", Format: "gguf", Quantization: "Q8_0", Runtime: "llamacpp", Backend: "metal"}},
	}
	for _, c := range cases {
		got, err := Identify(c.m, "metal")
		if err != nil || got != c.want {
			t.Errorf("Identify(%s) = %+v, %v; want %+v", c.m.Source.URL, got, err, c.want)
		}
	}
	for _, u := range []string{"", "https://example.com/Qwen/Qwen-GGUF/resolve/main/q4_k_m.gguf", "https://huggingface.co/a/b-GGUF/resolve/main/model.gguf"} {
		if _, err := Identify(contracts.Model{Source: contracts.ModelSource{URL: u}}, "cpu"); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Identify(%q) err = %v, want ErrUnknownModel", u, err)
		}
	}
}

const gb = 1 << 30

func TestNormalize(t *testing.T) {
	cases := []struct {
		inv  contracts.HardwareInventory
		want Hardware
	}{
		{contracts.HardwareInventory{OS: "darwin", Arch: "arm64", CPU: contracts.CPUInfo{Model: "Apple M4 Max"},
			Accelerators: []contracts.Accelerator{{Vendor: "Apple", Model: "Apple M4 Max", UnifiedMemory: 36 * gb}}},
			Hardware{"macos", "arm64", "apple", "m4-max", "unified", "32-64"}},
		{contracts.HardwareInventory{OS: "linux", Arch: "amd64", Memory: contracts.MemoryInfo{TotalBytes: 64 * gb},
			Accelerators: []contracts.Accelerator{{Vendor: "NVIDIA", Model: "NVIDIA GeForce RTX 3060", DedicatedVRAM: 12 * gb}, {Vendor: "NVIDIA", Model: "NVIDIA GeForce RTX 4090", DedicatedVRAM: 24564 << 20}}},
			Hardware{"linux", "amd64", "nvidia", "rtx-4090", "dedicated", "16-32"}},
		{contracts.HardwareInventory{OS: "linux", Arch: "amd64",
			Accelerators: []contracts.Accelerator{{Vendor: "AMD", Model: "Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900 XT/7900 XTX] (rev c8)", DedicatedVRAM: 8188 << 20}}},
			Hardware{"linux", "amd64", "amd", "rx-7900-xt", "dedicated", "8-16"}},
		{contracts.HardwareInventory{OS: "windows", Arch: "amd64", CPU: contracts.CPUInfo{Model: "AMD Ryzen 9 7950X"}, Memory: contracts.MemoryInfo{TotalBytes: 32 * gb},
			Accelerators: []contracts.Accelerator{{Vendor: "Generic", Model: "CPU inference"}}},
			Hardware{"windows", "amd64", "cpu", "amd", "system", "32-64"}},
	}
	for _, c := range cases {
		got, ok := Normalize(c.inv)
		if !ok || got != c.want {
			t.Errorf("Normalize = %+v, %v; want %+v", got, ok, c.want)
		}
	}
	if _, ok := Normalize(contracts.HardwareInventory{OS: "freebsd", Arch: "amd64"}); ok {
		t.Error("an unknown platform was normalized")
	}
	if Class("nvidia", "rtx-4090") != "rtx-40" || Class("apple", "m4-max") != "apple-silicon" || Class("amd", "rx-7900-xt") != "rx-7000" {
		t.Error("Class does not match the ratings service")
	}
}

// fakeService is the ratings service.
type fakeService struct {
	mu      sync.Mutex
	posts   []Rating
	deletes []string
	snap    Snapshot
	down    bool
}

func (f *fakeService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/ratings":
		var rt Rating
		_ = json.NewDecoder(r.Body).Decode(&rt)
		f.posts = append(f.posts, rt)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":"k1","created":true}`))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/ratings/"):
		f.deletes = append(f.deletes, strings.TrimPrefix(r.URL.Path, "/v1/ratings/")+" "+r.Header.Get("X-Ratings-Client"))
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/v1/aggregates" || r.URL.Path == "/summary.json":
		_ = json.NewEncoder(w).Encode(f.snap)
	default:
		http.NotFound(w, r)
	}
}

func newService(t *testing.T, srv *httptest.Server) (*Service, *repositories.SettingsRepo, *[]string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := repositories.NewSettingsRepo(db.SQL)
	var records []string
	s := &Service{
		DB: db.SQL, Settings: settings,
		Client: &Client{ServiceURL: srv.URL, SummaryURL: srv.URL + "/summary.json", HTTP: srv.Client()},
		Hardware: func(context.Context) (contracts.HardwareInventory, error) {
			return contracts.HardwareInventory{OS: "darwin", Arch: "arm64",
				Accelerators: []contracts.Accelerator{{Vendor: "Apple", Model: "Apple M4 Max", UnifiedMemory: 36 * gb}}}, nil
		},
		Models: func(context.Context) ([]contracts.Model, error) {
			return []contracts.Model{
				{ID: "qwen2.5-coder-7b-q4", Variant: "Q4_K_M", Installed: true, Source: contracts.ModelSource{URL: "https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-q4_k_m.gguf"}},
				{ID: "local-file", Installed: true, Source: contracts.ModelSource{URL: "https://example.com/m.gguf"}},
			}, nil
		},
		Record:     func(_ context.Context, dest, detail string) { records = append(records, dest+": "+detail) },
		AppVersion: "0.9.0",
	}
	return s, settings, &records
}

func TestRateShareAndWithdraw(t *testing.T) {
	fake := &fakeService{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, _, records := newService(t, srv)
	ctx := context.Background()

	v, err := s.Get(ctx, "qwen2.5-coder-7b-q4")
	if err != nil || !v.Rateable || v.Shares == nil || v.Shares.Hardware.Key() != "apple:m4-max:unified:32-64" || v.Stars != 0 {
		t.Fatalf("Get = %+v, %v", v, err)
	}
	// Kept on this computer: nothing is sent.
	if _, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 4, Tags: []string{"fast"}}); err != nil {
		t.Fatal(err)
	}
	if len(fake.posts) != 0 || len(*records) != 0 {
		t.Fatalf("a private rating was sent: %v %v", fake.posts, *records)
	}
	if _, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 7}); !errors.Is(err, ErrStars) {
		t.Fatalf("7 stars err = %v", err)
	}
	v, err = s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 5, Tags: []string{"fast", "stable"}, Share: true})
	if err != nil || !v.Shared || v.Stars != 5 {
		t.Fatalf("share = %+v, %v", v, err)
	}
	p := fake.posts[0]
	if p.Model.ID != "qwen2.5-coder-7b-instruct" || p.Runtime.Backend != "metal" || p.Hardware.Family != "m4-max" || len(p.ClientID) != 32 || p.AppVersion != "0.9.0" {
		t.Fatalf("posted %+v", p)
	}
	if len(*records) != 1 || !strings.Contains((*records)[0], "5-star rating of qwen2.5-coder-7b-instruct") {
		t.Fatalf("records = %v", *records)
	}
	// Keeping it private again withdraws it.
	v, err = s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 3})
	if err != nil || v.Shared || len(fake.deletes) != 1 || fake.deletes[0] != "k1 "+p.ClientID {
		t.Fatalf("withdraw = %+v, %v, %v", v, err, fake.deletes)
	}
	// A model that cannot be compared is rated here only.
	v, _ = s.Get(ctx, "local-file")
	if v.Rateable || v.Reason == "" {
		t.Fatalf("local-file = %+v", v)
	}
	if _, err := s.Put(ctx, "local-file", Input{Stars: 2, Share: true}); !errors.Is(err, ErrNotShareable) {
		t.Fatalf("share local-file err = %v", err)
	}
	// The service down: saved here, and the error says so.
	if _, err := s.Put(ctx, "qwen2.5-coder-7b-q4", Input{Stars: 4, Share: true}); err != nil {
		t.Fatal(err)
	}
	fake.down = true
	var se *ShareError
	if err := s.Delete(ctx, "qwen2.5-coder-7b-q4"); !errors.As(err, &se) {
		t.Fatalf("delete with the service down err = %v", err)
	}
	fake.down = false
	if err := s.Delete(ctx, "qwen2.5-coder-7b-q4"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(ctx, "qwen2.5-coder-7b-q4"); v.Stars != 0 {
		t.Fatalf("deleted rating remains: %+v", v)
	}
}

func TestAsk(t *testing.T) {
	srv := httptest.NewServer(&fakeService{})
	defer srv.Close()
	s, settings, _ := newService(t, srv)
	ctx := context.Background()
	add := func(day string, n int) {
		for i := range n {
			if _, err := s.DB.Exec(`INSERT INTO generation_metrics (id, model_id, created_at) VALUES (?, 'qwen2.5-coder-7b-q4', ?)`, fmt.Sprintf("%s-%d", day, i), day+"T10:00:00Z"); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("2026-09-01", 9)
	if v, _ := s.Get(ctx, "qwen2.5-coder-7b-q4"); v.Ask {
		t.Fatal("asked after one day")
	}
	add("2026-09-02", 1)
	if v, _ := s.Get(ctx, "qwen2.5-coder-7b-q4"); !v.Ask {
		t.Fatal("not asked after 10 replies on 2 days")
	}
	_ = settings.SetBool(ctx, SettingAsk, false)
	if v, _ := s.Get(ctx, "qwen2.5-coder-7b-q4"); v.Ask {
		t.Fatal("asked with asking off")
	}
	_ = settings.SetBool(ctx, SettingAsk, true)
	if err := s.Dismiss(ctx, "qwen2.5-coder-7b-q4"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(ctx, "qwen2.5-coder-7b-q4"); v.Ask {
		t.Fatal("asked after dismissing")
	}
}

func TestCommunity(t *testing.T) {
	fake := &fakeService{}
	if err := json.Unmarshal([]byte(`{"schema_version":1,"generated_at":"2026-10-01T04:17:00Z","prior":3.5,"weight":5,"min_ratings":3,"models":[
		{"model":"qwen2.5-coder-7b-instruct","format":"gguf","quantization":"Q4_K_M","runtime":"llamacpp","backend":"metal","cohorts":[
			{"tier":"class","cohort":"apple:apple-silicon:32-64","ratings":12,"average":4.5,"weighted_score":4.2,"confidence":"community"},
			{"tier":"backend","cohort":"metal:32-64","ratings":20,"average":4.4,"weighted_score":4.2,"confidence":"community"},
			{"tier":"global","ratings":40,"average":4.1,"weighted_score":4.0,"confidence":"community"}]},
		{"model":"qwen2.5-coder-7b-instruct","format":"gguf","quantization":"Q4_K_M","runtime":"llamacpp","backend":"cpu","cohorts":[
			{"tier":"global","ratings":3,"average":2,"weighted_score":3,"confidence":"early"}]}]}`), &fake.snap); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, settings, records := newService(t, srv)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	// Off by default: nothing is downloaded.
	c, err := s.Community(ctx)
	if err != nil || c.Enabled || len(*records) != 0 {
		t.Fatalf("Community off = %+v, %v, %v", c, err, *records)
	}
	_ = settings.SetBool(ctx, SettingShow, true)
	c, err = s.Community(ctx)
	if err != nil || !c.Enabled || c.FetchedAt == nil {
		t.Fatalf("Community = %+v, %v", c, err)
	}
	mc := c.Models["qwen2.5-coder-7b-q4"]
	if mc.Similar == nil || mc.Similar.Tier != "class" || mc.Similar.Ratings != 12 || mc.Overall == nil || mc.Overall.Ratings != 40 {
		t.Fatalf("model = %+v", mc)
	}
	if len(*records) != 1 {
		t.Fatalf("records = %v", *records)
	}
	// Within a day, the kept summary is used; when the service is down
	// after that, the dataset is.
	_, _ = s.Community(ctx)
	if len(*records) != 1 {
		t.Fatalf("downloaded again within a day: %v", *records)
	}
	now = now.Add(25 * time.Hour)
	c, err = s.Community(ctx)
	if err != nil || !strings.HasSuffix(c.Source, "/v1/aggregates") {
		t.Fatalf("refresh = %+v, %v", c, err)
	}
	fake.down = true
	now = now.Add(25 * time.Hour)
	c, err = s.Community(ctx)
	if err != nil || c.Error == "" || c.Models["qwen2.5-coder-7b-q4"].Overall == nil {
		t.Fatalf("offline = %+v, %v", c, err)
	}
}

func TestObservations(t *testing.T) {
	fake := &fakeService{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, _, records := newService(t, srv)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	s.LocalNode = func() string { return "here" }
	s.OutOfMemory = func(text string) bool { return strings.Contains(text, "out of memory") }
	const id = "qwen2.5-coder-7b-q4"

	// Nothing measured yet.
	if v, _ := s.Get(ctx, id); v.Observations != nil {
		t.Fatalf("observations before any use: %+v", v.Observations)
	}
	add := func(n int, tps, ttft float64, total int, steps string, at time.Time) {
		for i := range n {
			if _, err := s.DB.Exec(`INSERT INTO generation_metrics (id, model_id, eval_tok_per_sec, ttft_ms, total_tokens, role_steps_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				fmt.Sprintf("%v-%v-%d", tps, at.Unix(), i), id, tps, ttft, total, steps, at.Format(time.RFC3339)); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(2, 40, 300, 2000, `[]`, now.Add(-time.Hour))
	add(1, 50, 500, 9000, `[{"node_id":"here"}]`, now.Add(-time.Hour))
	// On a paired computer, and too old: left out.
	add(5, 5, 9000, 200000, `[{"node_id":"elsewhere"}]`, now.Add(-time.Hour))
	add(5, 1, 1, 1, `[]`, now.Add(-40*24*time.Hour))
	s.RecordStart(ctx, id, nil)
	s.RecordStart(ctx, id, nil)
	s.RecordStart(ctx, id, pluginapi.LoadFailed(errors.New("llama-server: out of memory")))
	// Not the model's fault: left out.
	s.RecordStart(ctx, id, context.Canceled)
	s.RecordStart(ctx, id, errors.New("llama-server not installed"))
	s.RecordCrash(ctx, id, false)

	v, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := Observations{TokensPerSecond: 40, TTFTMillis: 300, Starts: 3, StartFailures: 1, Crashed: true, OutOfMemory: true, ContextBand: "8-32k"}
	if v.Observations == nil || *v.Observations != want {
		t.Fatalf("observations = %+v, want %+v", v.Observations, want)
	}

	// Shared without observations, nothing about how it runs is sent.
	if _, err := s.Put(ctx, id, Input{Stars: 4, Share: true}); err != nil {
		t.Fatal(err)
	}
	if fake.posts[0].Observations != nil {
		t.Fatalf("observations sent without being chosen: %+v", fake.posts[0].Observations)
	}
	v, err = s.Put(ctx, id, Input{Stars: 4, Share: true, Observations: true})
	if err != nil || !v.ShareObservations {
		t.Fatalf("share observations = %+v, %v", v, err)
	}
	if got := fake.posts[1].Observations; got == nil || *got != want {
		t.Fatalf("sent observations = %+v", got)
	}
	last := (*records)[len(*records)-1]
	if !strings.Contains(last, "40.0 tokens/s") || !strings.Contains(last, "2 of 3 starts worked") {
		t.Fatalf("record = %q", last)
	}
	// Keeping it private turns off sharing observations too.
	if v, _ := s.Put(ctx, id, Input{Stars: 4, Observations: true}); v.ShareObservations {
		t.Fatal("observations shared on a private rating")
	}
}

func TestSignals(t *testing.T) {
	fake := &fakeService{}
	if err := json.Unmarshal([]byte(`{"schema_version":1,"generated_at":"2026-10-01T04:17:00Z","prior":3.5,"weight":5,"min_ratings":3,"models":[
		{"model":"qwen2.5-coder-7b-instruct","format":"gguf","quantization":"Q4_K_M","runtime":"llamacpp","backend":"metal","cohorts":[
			{"tier":"class","cohort":"apple:apple-silicon:32-64","ratings":4,"average":4.5,"weighted_score":4.1,"confidence":"early"},
			{"tier":"global","ratings":40,"average":4.1,"weighted_score":4.0,"confidence":"community"}]}]}`), &fake.snap); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, settings, records := newService(t, srv)
	ctx := context.Background()

	// Off: none, and nothing is downloaded to find them.
	if sig, err := s.Signals(ctx); err != nil || sig != nil {
		t.Fatalf("signals with ratings off = %v, %v", sig, err)
	}
	_ = settings.SetBool(ctx, SettingShow, true)
	// On but no summary kept yet: still nothing downloaded.
	if sig, _ := s.Signals(ctx); len(sig) != 0 || len(*records) != 0 {
		t.Fatalf("signals before a summary = %v, records %v", sig, *records)
	}
	if _, err := s.Community(ctx); err != nil {
		t.Fatal(err)
	}
	sig, err := s.Signals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Early ratings from similar hardware count half: (4.1 - 3.5) × 0.5.
	got := sig["qwen2.5-coder-7b-q4"]
	if !got.Similar || got.Ratings != 4 || math.Abs(got.Signal-0.3) > 1e-9 {
		t.Fatalf("signal = %+v", got)
	}
	if _, ok := sig["local-file"]; ok {
		t.Fatal("a model that cannot be compared has a signal")
	}
}
