package profiles

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestValidateProfileRequiresName(t *testing.T) {
	err := Validate(Profile{OrchestratorID: "simple"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateProfileRequiresModelForRequiredRole(t *testing.T) {
	err := Validate(Profile{
		Name:           "Test",
		OrchestratorID: "simple",
		Roles:          []contracts.ModelRole{{Role: "worker", Required: true}},
	})
	if err == nil {
		t.Fatal("expected error for missing model_id")
	}
}

func TestValidateProfileOK(t *testing.T) {
	err := Validate(Profile{
		Name:           "Test",
		OrchestratorID: "simple",
		Roles:          []contracts.ModelRole{{Role: "assistant", ModelID: "m1", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
