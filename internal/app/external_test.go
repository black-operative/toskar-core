package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestExternalModelsAreListedButNeverOffline(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
	}))
	defer srv.Close()
	a := &App{External: external.New(external.Config{BaseURL: srv.URL})}
	list, err := a.externalModels(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "ext:gpt-4o-mini" || !list[0].Installed || list[0].Status != "external" {
		t.Fatalf("models = %+v, %v", list, err)
	}
	// Reused for a minute.
	if _, _ = a.externalModels(context.Background()); calls != 1 {
		t.Fatalf("listed %d times", calls)
	}
	// No server: nothing, and nothing asked.
	if list, err := (&App{External: external.New(external.Config{})}).externalModels(context.Background()); list != nil || err != nil {
		t.Fatalf("unset: %v %v", list, err)
	}

	online := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "ask"}}}
	offline := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "deny"}}}
	if err := externalAllowed(online, false); err != nil {
		t.Fatalf("online profile: %v", err)
	}
	if code, _ := contracts.ErrorCode(externalAllowed(offline, false)); code != "EXTERNAL_OFFLINE_PROFILE" {
		t.Fatalf("offline profile: %s", code)
	}
	if code, _ := contracts.ErrorCode(externalAllowed(online, true)); code != "EXTERNAL_LOCAL_ONLY" {
		t.Fatalf("local-only material: %s", code)
	}
	// A profile without web search at all is offline too.
	if externalAllowed(contracts.AIProfile{}, false) == nil {
		t.Fatal("a profile without web search sent work out")
	}
}
