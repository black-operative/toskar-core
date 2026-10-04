package remotetools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// fakeTool is a portable tool that upper-cases its input file. Prepare and
// Finish record where they ran.
type fakeTool struct {
	where       string
	state       string
	accelerated bool
	languages   []string
	runs        atomic.Int32
	block       chan struct{}
	cancelled   atomic.Bool
	fail        string
	mu          sync.Mutex
	finished    []string
}

func (f *fakeTool) ID() string          { return "image.generate" }
func (f *fakeTool) DisplayName() string { return "Generate Image" }
func (f *fakeTool) Description() string { return "fake" }
func (f *fakeTool) Available() (bool, string) {
	if f.state == Healthy {
		return true, ""
	}
	return false, "not set up on " + f.where
}
func (f *fakeTool) Provider() Provider {
	return Provider{Tool: f.ID(), Name: "Fake", State: f.state, Accelerated: f.accelerated, Languages: f.languages}
}
func (f *fakeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return Execute(ctx, f, args)
}
func (f *fakeTool) Prepare(_ context.Context, args map[string]any) (Job, error) {
	return Job{Args: args, Files: []File{{Name: "in.txt", Data: []byte("prepared on " + f.where)}}}, nil
}
func (f *fakeTool) Run(ctx context.Context, job Job) (Output, error) {
	f.runs.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			f.cancelled.Store(true)
			return Output{}, ctx.Err()
		}
	}
	if f.fail != "" {
		return Output{}, errors.New(f.fail)
	}
	return Output{Result: map[string]any{"ran_on": f.where}, Files: []File{{Name: "out.txt", Data: []byte(strings.ToUpper(string(job.Files[0].Data)))}}}, nil
}
func (f *fakeTool) Finish(_ context.Context, out Output) (map[string]any, error) {
	f.mu.Lock()
	f.finished = append(f.finished, string(out.Files[0].Data))
	f.mu.Unlock()
	res := map[string]any{"finished_on": f.where}
	for k, v := range out.Result {
		res[k] = v
	}
	return res, nil
}

// httpDoer is a paired computer at an httptest server. The server serves
// Handler at /tools/, as the internal server does under /internal/v1.
type httpDoer struct{ url string }

func (d httpDoer) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(path, "/internal/v1/") {
		return nil, errors.New("not an internal API path: " + path)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.url+strings.TrimPrefix(path, "/internal/v1"), body)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func peerServer(t *testing.T, tool *fakeTool) Peer {
	t.Helper()
	srv := httptest.NewServer(Handler([]Portable{tool}, nil))
	t.Cleanup(srv.Close)
	return Peer{ID: "gpu-box", Name: "Workstation", Online: true, Client: httpDoer{srv.URL}}
}

func network(peers ...Peer) (*Network, *[]string) {
	var sent []string
	return &Network{LocalID: "laptop", LocalName: "Laptop",
		Peers: func(context.Context) []Peer { return peers },
		Sent:  func(_ context.Context, p Peer, tool string) { sent = append(sent, p.Name+":"+tool) }}, &sent
}

// A tool this computer cannot run goes to a paired computer that can: the
// files are read here, the work runs there, and the result is saved here
// (Gungnir §38).
func TestRunsWhereTheProviderIs(t *testing.T) {
	local := &fakeTool{where: "laptop", state: Unavailable}
	remote := &fakeTool{where: "workstation", state: Healthy}
	net, sent := network(peerServer(t, remote))
	proxy := net.Proxy(local)

	res, err := proxy.Execute(context.Background(), map[string]any{"prompt": "a fox"})
	if err != nil {
		t.Fatal(err)
	}
	if res["ran_on"] != "workstation" || res["finished_on"] != "laptop" || res["computer"] != "Workstation" {
		t.Fatalf("result %v", res)
	}
	if local.finished[0] != "PREPARED ON LAPTOP" || local.runs.Load() != 0 || remote.runs.Load() != 1 {
		t.Fatalf("finished %v, runs here %d there %d", local.finished, local.runs.Load(), remote.runs.Load())
	}
	if len(*sent) != 1 || (*sent)[0] != "Workstation:image.generate" {
		t.Fatalf("sent %v", *sent)
	}
	if ok, _ := proxy.Available(); !ok {
		t.Error("the tool is not available though the workstation can run it")
	}
}

func TestPlacement(t *testing.T) {
	remote := &fakeTool{where: "workstation", state: Healthy, accelerated: true}
	peer := peerServer(t, remote)
	ctx := context.Background()
	run := func(ctx context.Context, local *fakeTool) string {
		t.Helper()
		net, _ := network(peer)
		res, err := net.Proxy(local).Execute(ctx, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		return res["ran_on"].(string)
	}
	// An image goes to the computer whose GPU makes it.
	if got := run(ctx, &fakeTool{where: "laptop", state: Healthy}); got != "workstation" {
		t.Errorf("accelerated peer: ran on %s", got)
	}
	// With both accelerated, nothing has to travel.
	if got := run(ctx, &fakeTool{where: "laptop", state: Healthy, accelerated: true}); got != "laptop" {
		t.Errorf("both accelerated: ran on %s", got)
	}
	// The profile keeps it here, or away from a computer.
	if got := run(WithPolicy(ctx, contracts.NodePolicy{Mode: "prefer_local"}), &fakeTool{where: "laptop", state: Healthy}); got != "laptop" {
		t.Errorf("prefer local: ran on %s", got)
	}
	if got := run(WithPolicy(ctx, contracts.NodePolicy{Remote: "off"}), &fakeTool{where: "laptop", state: Healthy}); got != "laptop" {
		t.Errorf("remote off: ran on %s", got)
	}
	if got := run(WithPolicy(ctx, contracts.NodePolicy{DeniedNodes: []string{"gpu-box"}}), &fakeTool{where: "laptop", state: Healthy}); got != "laptop" {
		t.Errorf("denied: ran on %s", got)
	}
	net, _ := network(peer)
	if _, err := net.Proxy(&fakeTool{where: "laptop", state: Unavailable}).Execute(WithPolicy(ctx, contracts.NodePolicy{Remote: "off"}), nil); err == nil || !strings.Contains(err.Error(), "not set up on laptop") {
		t.Errorf("remote off with nothing here: %v", err)
	}
}

// A computer that cannot be reached is skipped for one that can; the
// tool's own error is not retried elsewhere.
func TestFallbackAndErrors(t *testing.T) {
	gone := Peer{ID: "gone", Name: "Gone", Online: true, Client: httpDoer{"http://127.0.0.1:1"}}
	local := &fakeTool{where: "laptop", state: Healthy}
	net, _ := network(gone)
	net.known = map[string]peerProviders{"gone": {at: time.Now(), providers: []Provider{{Tool: "image.generate", State: Healthy, Accelerated: true}}}}
	res, err := net.Proxy(local).Execute(context.Background(), map[string]any{})
	if err != nil || res["ran_on"] != "laptop" {
		t.Fatalf("fallback: %v %v", res, err)
	}

	remote := &fakeTool{where: "workstation", state: Healthy, fail: "the prompt is longer than 2000 characters"}
	net, _ = network(peerServer(t, remote))
	_, err = net.Proxy(&fakeTool{where: "laptop", state: Healthy}).Execute(WithPolicy(context.Background(), contracts.NodePolicy{PreferredNodes: []string{"gpu-box"}}), map[string]any{})
	var re *RemoteError
	if !errors.As(err, &re) || re.NotReady || !strings.Contains(err.Error(), "longer than 2000") {
		t.Fatalf("tool error: %v", err)
	}
}

// Stop reaches the other computer: its work is cancelled.
func TestCancelStopsTheRemoteWork(t *testing.T) {
	remote := &fakeTool{where: "workstation", state: Healthy, block: make(chan struct{})}
	net, _ := network(peerServer(t, remote))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := net.Proxy(&fakeTool{where: "laptop", state: Unavailable}).Execute(ctx, map[string]any{})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for remote.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call: %v", err)
	}
	for !remote.cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !remote.cancelled.Load() {
		t.Fatal("the work kept running on the other computer")
	}
}

// Diagnostics lists each computer's providers, an offline one, and one too
// old to run tools for others (§16).
func TestStatus(t *testing.T) {
	remote := &fakeTool{where: "workstation", state: Installing}
	old := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(old.Close)
	net, _ := network(peerServer(t, remote),
		Peer{ID: "old", Name: "Old Mac", Online: true, Client: httpDoer{old.URL}},
		Peer{ID: "off", Name: "Away", Online: false})
	st := net.Status(context.Background(), []Portable{&fakeTool{where: "laptop", state: Healthy}})
	if len(st) != 4 || !st[0].Local || st[0].Providers[0].State != Healthy {
		t.Fatalf("status %+v", st)
	}
	if st[1].Providers[0].State != Installing || st[2].Note != ErrOldPeer.Error() || st[3].Note != "offline" {
		t.Fatalf("peers %+v", st[1:])
	}
}

func TestHandlerRefusesWhatItCannotRun(t *testing.T) {
	srv := httptest.NewServer(Handler([]Portable{&fakeTool{where: "workstation", state: Unavailable}}, nil))
	t.Cleanup(srv.Close)
	for _, tool := range []string{"image.generate", "terminal"} {
		_, err := RunOn(context.Background(), httpDoer{srv.URL}, tool, Job{})
		var re *RemoteError
		if !errors.As(err, &re) || !re.NotReady {
			t.Errorf("%s: %v", tool, err)
		}
	}
}

// A call in a language goes to a computer whose provider works in it
// (multilingual spec §20); when none does, it runs where it would anyway.
func TestPlacementByLanguage(t *testing.T) {
	remote := &fakeTool{where: "workstation", state: Healthy, languages: []string{"en", "de"}}
	peer := peerServer(t, remote)
	run := func(args map[string]any) string {
		t.Helper()
		net, _ := network(peer)
		res, err := net.Proxy(&fakeTool{where: "laptop", state: Healthy, languages: []string{"en"}}).Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		return res["ran_on"].(string)
	}
	if got := run(map[string]any{"language": "en"}); got != "laptop" {
		t.Errorf("English: ran on %s", got)
	}
	if got := run(map[string]any{"language": "de-AT"}); got != "workstation" {
		t.Errorf("German asked for: ran on %s", got)
	}
	if got := run(map[string]any{"text": "Kannst du mir bitte erklären, wie das funktioniert?"}); got != "workstation" {
		t.Errorf("German text: ran on %s", got)
	}
	if got := run(map[string]any{"language": "ja"}); got != "laptop" {
		t.Errorf("no one speaks Japanese: ran on %s", got)
	}
}

func TestProviderSpeaks(t *testing.T) {
	p := Provider{Languages: []string{"en", "pt"}}
	if !p.Speaks("pt-BR") || !p.Speaks("") || p.Speaks("de") {
		t.Error("Speaks with a language list")
	}
	if !(Provider{}).Speaks("de") {
		t.Error("a provider without languages works in any")
	}
	if got := CallLanguage(map[string]any{"text": "ok"}); got != "" {
		t.Errorf("short text = %q", got)
	}
}
