package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/training"
)

func TestTrainingRoutes(t *testing.T) {
	srv := NewServer(Dependencies{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/v1/training/ais", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unbound training: %d", rec.Code)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info := &models.TrainingInfo{BaseRepo: "Qwen/Qwen2.5-0.5B-Instruct", Architecture: "qwen2", License: "Apache-2.0",
		HiddenSize: 896, Layers: 24, VocabSize: 151936, BaseBytes: 988097824}
	catalog := map[string]models.CatalogEntry{"small-q4": {ID: "small-q4", DisplayName: "Small", Training: info}}
	svc := training.NewService(training.Deps{
		Repo:      training.NewRepo(db.SQL),
		Knowledge: mimir.NewStore(db.SQL, t.TempDir()),
		Catalog:   func(id string) (models.CatalogEntry, bool) { e, ok := catalog[id]; return e, ok },
		Nodes:     func(context.Context) ([]training.Node, error) { return nil, nil },
	})
	srv.BindTraining(svc, func() []models.CatalogEntry { return []models.CatalogEntry{catalog["small-q4"]} })

	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest(method, path, strings.NewReader(body)))
		return rec
	}
	rec = do(http.MethodPost, "/api/v1/training/classify", `{"filename":"stock.csv","text":"sku,price\nA1,9.99\n"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"use":"knowledge"`) {
		t.Fatalf("classify: %d %s", rec.Code, rec.Body)
	}
	rec = do(http.MethodPost, "/api/v1/training/classify", `{"filename":"stock.csv","text":"sku,price\nA1,9.99\n","use":"training"}`)
	if !strings.Contains(rec.Body.String(), `"error":"no training examples`) {
		t.Fatalf("classify: %d %s", rec.Code, rec.Body)
	}
	rec = do(http.MethodPost, "/api/v1/training/ais", `{"name":"Tire Bot","base_model_id":"small-q4"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var ai training.SpecializedAI
	_ = json.Unmarshal(rec.Body.Bytes(), &ai)

	if rec = do(http.MethodPost, "/api/v1/training/ais", `{"name":"x","base_model_id":"missing"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown base: %d", rec.Code)
	}
	if rec = do(http.MethodGet, "/api/v1/training/ais/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown ai: %d", rec.Code)
	}
	// Not enough examples yet: training is refused with 409 and the reason.
	rec = do(http.MethodPost, "/api/v1/training/ais/"+ai.ID+"/train", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "at least 10") {
		t.Fatalf("train without examples: %d %s", rec.Code, rec.Body)
	}
	if rec = do(http.MethodPost, "/api/v1/training/ais/"+ai.ID+"/revisions/1/deploy", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deploy missing revision: %d %s", rec.Code, rec.Body)
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if rec = do(m, "/api/v1/training/ais/"+ai.ID+"/revisions/1/export", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s export of a missing revision: %d %s", m, rec.Code, rec.Body)
		}
	}
	if rec = do(http.MethodGet, "/api/v1/training/ais/"+ai.ID+"/revisions/1/export/file", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("download of a missing export: %d %s", rec.Code, rec.Body)
	}
	if rec = do(http.MethodPost, "/api/v1/training/ais/"+ai.ID+"/revisions/zero/deploy", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad revision: %d", rec.Code)
	}
	rec = do(http.MethodGet, "/api/v1/training/base-models?goal=tire+shop", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"model_id":"small-q4"`) {
		t.Fatalf("base models: %d %s", rec.Code, rec.Body)
	}
}
