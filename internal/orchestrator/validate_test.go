package orchestrator

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestSimpleValidateProfile(t *testing.T) {
	o := simple.New()
	if err := o.ValidateProfile(context.Background(), contracts.AIProfile{
		Name: "x", Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := o.ValidateProfile(context.Background(), contracts.AIProfile{Name: "x"}); err == nil {
		t.Fatal("expected error")
	}
}
