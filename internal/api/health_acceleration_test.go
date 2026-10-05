package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Health carries the acceleration summary, and stays "ok" on the CPU.
func TestHealthReportsAcceleration(t *testing.T) {
	srv := NewServer(Dependencies{Acceleration: func(context.Context) string { return contracts.AccelerationCPUExpected }})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Host = "127.0.0.1:7331"
	srv.Handler().ServeHTTP(rec, req)
	var got contracts.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Acceleration != contracts.AccelerationCPUExpected {
		t.Fatalf("health: %d %s", rec.Code, rec.Body.String())
	}
}
