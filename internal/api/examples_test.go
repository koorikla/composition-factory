package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGetExamples(t *testing.T) {
	h := testHandler(t)
	rec := do(t, h, "GET", "/api/examples", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/examples code = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Examples []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			Tags          []string `json:"tags"`
			ResourceCount int      `json:"resourceCount"`
			Sources       []string `json:"sources"`
			YAML          string   `json:"yaml"`
		} `json:"examples"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(body.Examples) < 3 {
		t.Errorf("got %d examples, want at least 3", len(body.Examples))
	}

	foundIRSA := false
	foundRDS := false
	foundApp := false
	for _, ex := range body.Examples {
		if ex.ID == "irsa" {
			foundIRSA = true
		}
		if ex.ID == "rds-postgres" {
			foundRDS = true
		}
		if ex.ID == "k8s-app" {
			foundApp = true
		}
		if ex.YAML == "" {
			t.Errorf("example %q has empty YAML", ex.ID)
		}
		if ex.ResourceCount == 0 {
			t.Errorf("example %q has ResourceCount 0", ex.ID)
		}
	}

	if !foundIRSA || !foundRDS || !foundApp {
		t.Errorf("missing expected examples: foundIRSA=%v, foundRDS=%v, foundApp=%v", foundIRSA, foundRDS, foundApp)
	}
}

func TestGetExampleByID(t *testing.T) {
	h := testHandler(t)

	rec := do(t, h, "GET", "/api/examples/irsa", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/examples/irsa code = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Example struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			YAML string `json:"yaml"`
		} `json:"example"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if body.Example.ID != "irsa" {
		t.Errorf("example ID = %q, want \"irsa\"", body.Example.ID)
	}

	// 404 for unknown
	rec404 := do(t, h, "GET", "/api/examples/unknown-xyz", "")
	if rec404.Code != http.StatusNotFound {
		t.Errorf("GET /api/examples/unknown-xyz code = %d, want %d", rec404.Code, http.StatusNotFound)
	}
}
