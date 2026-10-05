package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denagava/blockchain-pow/chain"
)

func TestAPI(t *testing.T) {
	a := &api{bc: chain.New(2), path: filepath.Join(t.TempDir(), "c.json")}
	h := a.routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mine", strings.NewReader(`{"data":"tx1"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("mine: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mine", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty data: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/blocks", nil))
	var blocks []chain.Block
	_ = json.NewDecoder(rec.Body).Decode(&blocks)
	if len(blocks) != 2 {
		t.Errorf("blocks = %d, want 2", len(blocks))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/validate", nil))
	if !strings.Contains(rec.Body.String(), `"is_valid":true`) {
		t.Errorf("validate: %s", rec.Body)
	}
}
