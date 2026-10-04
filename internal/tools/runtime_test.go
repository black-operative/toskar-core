package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
)

func TestCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"web.search": "internet.search", "Files.Read": "filesystem.read", "shell.run": "terminal",
		"internet.open": "internet.open", "git.status": "git.status", "custom.tool": "custom.tool",
	} {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
	if def, ok := Lookup("web.open"); !ok || def.ID != "internet.open" {
		t.Fatal("Lookup resolves aliases")
	}
}

func TestTimeoutsAndErrorKinds(t *testing.T) {
	if Timeout("terminal") != 2*time.Minute || Timeout("internet.search") != 45*time.Second || Timeout("unknown") != defaultTimeout {
		t.Fatal("timeouts")
	}
	cases := map[error]string{
		context.DeadlineExceeded:              ErrKindTimeout,
		context.Canceled:                      ErrKindCancelled,
		ErrNotOffered:                         ErrKindNotOffered,
		errors.New(`tool "x" denied by user`): ErrKindDenied,
		errors.New("query required"):          ErrKindInvalid,
		errors.New("connection reset"):        ErrKindFailed,
	}
	for err, want := range cases {
		if got := ErrorKind(err); got != want {
			t.Errorf("ErrorKind(%v) = %q, want %q", err, got, want)
		}
	}
}

type slowTool struct{}

func (slowTool) ID() string          { return "internet.search" }
func (slowTool) DisplayName() string { return "slow" }
func (slowTool) Description() string { return "slow" }
func (slowTool) Execute(ctx context.Context, _ map[string]any) (map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestExecuteStopsWithTheTurn(t *testing.T) {
	r := NewRegistry(t.TempDir(), events.NewBus(8))
	r.Register(slowTool{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := r.Execute(ctx, "web.search", map[string]any{"query": "x"}, PolicyAllow, "test", nil)
	if ErrorKind(err) != ErrKindCancelled {
		t.Fatalf("err = %v", err)
	}
}
