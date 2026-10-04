package tools

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestConnectedToolsJoinTheCatalogAndProfiles(t *testing.T) {
	t.Cleanup(func() { SetConnected("svc", nil) })
	SetConnected("svc", []Definition{
		{ID: "svc.read", Source: "connector:svc", Risk: RiskRead, DefaultPolicy: PolicyAllow},
		{ID: "svc.write", Source: "connector:svc", Risk: RiskWrite, DefaultPolicy: PolicyAsk},
	})
	if _, ok := Lookup("svc.read"); !ok {
		t.Fatal("connected tool not in the catalog")
	}
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: PolicyAllow},
		{ToolID: "svc.write", Policy: PolicyDeny},
	}}
	got := WithConnected(profile)
	if PolicyForProfile(got, "svc.read") != PolicyAllow {
		t.Error("read tool not allowed by default")
	}
	if PolicyForProfile(got, "svc.write") != PolicyDeny {
		t.Error("the profile's own deny was replaced")
	}
	if len(profile.Tools) != 2 {
		t.Error("the original profile changed")
	}
	ids := map[string]bool{}
	for _, d := range Enabled(got, nil) {
		ids[d.ID] = true
	}
	if !ids["svc.read"] || ids["svc.write"] {
		t.Errorf("enabled = %v", ids)
	}

	SetConnected("svc", nil)
	if _, ok := Lookup("svc.read"); ok {
		t.Fatal("disconnected tool still in the catalog")
	}
	if p := WithConnected(profile); len(p.Tools) != 2 {
		t.Error("tools added with nothing connected")
	}
}
