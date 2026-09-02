package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// POST /api/blueprint/import takes raw blueprint YAML (the on-disk DSL
// format), runs it through the exact Load+Validate gate files get, persists
// it and returns the full doc as JSON — the GUI's "import dsl.yaml" button.
func TestImportBlueprintYAML(t *testing.T) {
	srv, _, _, _ := testServerParts(t)
	yaml, err := os.ReadFile("../../testdata/xqueue.cf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(string(yaml)))
	req.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"name":"xqueue"`) {
		t.Fatalf("response is not the imported doc: %.200s", rec.Body)
	}
	// persisted: a GET returns the imported doc
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/blueprint", nil))
	if !strings.Contains(rec2.Body.String(), `"name":"xqueue"`) {
		t.Fatalf("import did not persist: %.200s", rec2.Body)
	}
}

func TestImportRejectsInvalidYAMLVerbatim(t *testing.T) {
	srv, _, _, _ := testServerParts(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader("{not yaml: ["))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestImportRejectsValidYAMLInvalidBlueprint(t *testing.T) {
	srv, _, _, _ := testServerParts(t)
	bad := "apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\nmetadata: {name: x}\nspec:\n  xrd: {group: g, kind: Bad!, plural: bads, version: v1, scope: Namespaced}\n"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(bad))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Kind") {
		t.Fatalf("validation error not verbatim: %s", rec.Body)
	}
}
