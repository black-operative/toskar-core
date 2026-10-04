package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Every response names the client contract, the version route describes
// it, and a client built for another major version is told so (§68).
func TestClientContractOnResponses(t *testing.T) {
	srv := NewServer(Dependencies{})
	do := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		if header != "" {
			req.Header.Set(contracts.ClientContractHeader, header)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := do("")
	if rec.Code != http.StatusOK || rec.Header().Get(contracts.ContractHeader) != contracts.ContractVersion {
		t.Fatalf("status=%d header=%q", rec.Code, rec.Header().Get(contracts.ContractHeader))
	}
	var v contracts.VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Contract.Version != contracts.ContractVersion || v.Contract.Major != 1 {
		t.Fatalf("version = %+v, %v", v, err)
	}
	if rec := do("1.3"); rec.Code != http.StatusOK {
		t.Fatalf("same major refused: %d", rec.Code)
	}
	rec = do("2.0")
	if rec.Code != http.StatusUpgradeRequired || !strings.Contains(rec.Body.String(), "Update Yggdrasil") || rec.Header().Get(contracts.ContractHeader) == "" {
		t.Fatalf("other major: %d %s", rec.Code, rec.Body)
	}
	if exposed := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(exposed, contracts.ContractHeader) {
		t.Fatalf("browsers cannot read the contract header: %q", exposed)
	}
}

// The contract headers have names from before the Toskar rename, which
// older apps and computers send and read; both work (#237).
func TestClientContractLegacyHeaders(t *testing.T) {
	srv := NewServer(Dependencies{})
	do := func(method string, set map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/version", nil)
		for k, v := range set {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := do(http.MethodGet, nil)
	if rec.Header().Get(contracts.LegacyContractHeader) != contracts.ContractVersion {
		t.Fatalf("old header missing: %q", rec.Header().Get(contracts.LegacyContractHeader))
	}
	if rec := do(http.MethodGet, map[string]string{contracts.LegacyClientContractHeader: "2.0"}); rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("old client header ignored: %d", rec.Code)
	}
	if rec := do(http.MethodGet, map[string]string{contracts.LegacyClientContractHeader: "abc"}); !strings.Contains(rec.Body.String(), contracts.LegacyClientContractHeader) {
		t.Fatalf("message should name the header that was sent: %s", rec.Body)
	}
	// The new name wins when a client sends both.
	if rec := do(http.MethodGet, map[string]string{contracts.ClientContractHeader: "1.0", contracts.LegacyClientContractHeader: "2.0"}); rec.Code != http.StatusOK {
		t.Fatalf("new header should win: %d", rec.Code)
	}
	pre := do(http.MethodOptions, nil)
	allowed, exposed := pre.Header().Get("Access-Control-Allow-Headers"), pre.Header().Get("Access-Control-Expose-Headers")
	for _, h := range []string{contracts.ClientContractHeader, contracts.LegacyClientContractHeader} {
		if !strings.Contains(allowed, h) {
			t.Fatalf("browsers cannot send %s: %q", h, allowed)
		}
	}
	for _, h := range []string{contracts.ContractHeader, contracts.LegacyContractHeader} {
		if !strings.Contains(exposed, h) {
			t.Fatalf("browsers cannot read %s: %q", h, exposed)
		}
	}
}
