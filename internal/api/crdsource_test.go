package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testXRCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: xdatabases.platform.example.org
spec:
  group: platform.example.org
  names: {kind: XDatabase, plural: xdatabases}
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                engine: {type: string}
`

func TestAddCRDSource(t *testing.T) {
	h, blueprintPath, _, _ := testServerParts(t)

	body, _ := json.Marshal(map[string]string{"name": "xdatabase", "yaml": testXRCRD})
	req := httptest.NewRequest("POST", "/api/sources/crds", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "XDatabase") {
		t.Errorf("response does not list the scanned kind: %s", rec.Body)
	}

	// the manifest landed next to the blueprint, and the doc declares it
	if _, err := os.Stat(filepath.Join(filepath.Dir(blueprintPath), "crds", "xdatabase.yaml")); err != nil {
		t.Errorf("manifest file not written: %v", err)
	}
	doc, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "crds/xdatabase.yaml") {
		t.Errorf("blueprint does not declare the crds source:\n%s", doc)
	}

	// the palette sees the kind under the file's label
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/kinds", nil))
	if !strings.Contains(rec2.Body.String(), "XDatabase") {
		t.Errorf("/api/kinds does not list XDatabase: %s", rec2.Body)
	}
	if !strings.Contains(rec2.Body.String(), "crds/xdatabase.yaml") {
		t.Errorf("/api/kinds does not group under the crds path: %s", rec2.Body)
	}
}

func TestAddCRDSourceRejectsNonCRDYAML(t *testing.T) {
	h, _, _, _ := testServerParts(t)
	body, _ := json.Marshal(map[string]string{"name": "junk", "yaml": "kind: ConfigMap\napiVersion: v1\n"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sources/crds", strings.NewReader(string(body)))
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "CustomResourceDefinition") {
		t.Errorf("error does not explain what was expected: %s", rec.Body)
	}
}

func TestAddCRDSourceRejectsBadName(t *testing.T) {
	h, _, _, _ := testServerParts(t)
	body, _ := json.Marshal(map[string]string{"name": "../escape", "yaml": testXRCRD})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sources/crds", strings.NewReader(string(body)))
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
}
