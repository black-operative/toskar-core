package contracts

import (
	"strings"
	"testing"
)

func TestClientContract(t *testing.T) {
	if info := CurrentContract(); info.Version != ContractVersion || info.Major != 1 {
		t.Fatalf("info = %+v", info)
	}
	for _, ok := range []string{"", "1", "1.0", "1.7"} {
		if err := CheckClientContract(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for v, want := range map[string]string{"2.0": "Update Toskar", "0.9": "must look like", "abc": "must look like"} {
		if err := CheckClientContract(v); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", v, err)
		}
	}
}
