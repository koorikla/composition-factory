package api

import (
	"bytes"
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
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(testBlueprintYAML))
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

// The "input yaml" half of the packaging loop: a package.yaml stream (the
// yaml form of cf package) imports back — the blueprint is recovered from
// the Configuration meta's embedded-source annotation.
func TestImportPackageYAMLRecoversBlueprint(t *testing.T) {
	srv, _, _, _ := testServerParts(t)

	// export the current doc as package.yaml ...
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest("GET", "/api/package?format=yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status %d: %s", rec.Code, rec.Body)
	}
	stream := rec.Body.String()

	// ... and import it back through the blueprint gate
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(stream))
	req.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("import: status %d: %s", rec2.Code, rec2.Body)
	}
	if !strings.Contains(rec2.Body.String(), `"kind":"Blueprint"`) {
		t.Fatalf("import did not recover a blueprint: %s", rec2.Body)
	}
}

// A Configuration doc without the embedded-source annotation names exactly
// what is missing instead of a generic parse error.
func TestImportPackageWithoutAnnotationExplains(t *testing.T) {
	srv, _, _, _ := testServerParts(t)
	doc := "apiVersion: meta.pkg.crossplane.io/v1\nkind: Configuration\nmetadata:\n  name: bare\n"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(doc))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "factory.crossplane.io/blueprint") {
		t.Fatalf("error does not name the missing annotation: %s", rec.Body)
	}
}

func TestImportRejectsUnknownFields(t *testing.T) {
	srv, _, _, _ := testServerParts(t)
	bad := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: x}
spec:
  xrd:
    group: platform.example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    status:
      someUnknownKey: true
`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(bad))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "status") {
		t.Fatalf("expected error naming unknown field, got: %s", rec.Body)
	}
}

// POST /api/blueprint/import rejects a request body exceeding the 4 MiB limit
// with HTTP 413 and does not modify or persist the truncated document (CF-204).
func TestImportBlueprintRejectsBodyExceedingLimit(t *testing.T) {
	srv, blueprintPath, _, _ := testServerParts(t)

	origDiskBytes, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatalf("read original blueprint: %v", err)
	}

	var b strings.Builder
	b.WriteString(`apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-bp
spec:
  xrd:
    group: platform.example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName:
        type: string
        description: `)
	b.WriteString(strings.Repeat("a", 5<<20))
	b.WriteString("END-MARKER\n")
	b.WriteString("  resources: []\n")
	bigYAML := b.String()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(bigYAML))
	req.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %.200s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "4 MiB") {
		t.Errorf("expected error message to mention '4 MiB', got: %s", rec.Body)
	}

	persistedBytes, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatalf("read blueprint after import attempt: %v", err)
	}
	if !bytes.Equal(persistedBytes, origDiskBytes) {
		t.Errorf("blueprint on disk was modified on rejected import: got %d bytes, want %d bytes", len(persistedBytes), len(origDiskBytes))
	}
}

// A document within the 4 MiB limit (e.g. 3 MiB) imports intact.
func TestImportBlueprintWithinLimit(t *testing.T) {
	srv, blueprintPath, _, _ := testServerParts(t)

	var b strings.Builder
	b.WriteString(`apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-bp
spec:
  xrd:
    group: platform.example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName:
        type: string
        description: `)
	b.WriteString(strings.Repeat("a", 3<<20))
	b.WriteString("END-MARKER\n")
	b.WriteString("  resources: []\n")
	yaml3MiB := b.String()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(yaml3MiB))
	req.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "END-MARKER") {
		t.Fatalf("response missing END-MARKER")
	}

	persistedBytes, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatalf("read blueprint after import: %v", err)
	}
	if !bytes.Contains(persistedBytes, []byte("END-MARKER")) {
		t.Fatalf("persisted blueprint missing END-MARKER")
	}
	if !bytes.Contains(persistedBytes, []byte("resources: []")) {
		t.Fatalf("persisted blueprint missing resources: []")
	}
}

func TestImportRejectsMultiDocumentYAML(t *testing.T) {
	srv, _, _, _ := testServerParts(t)

	twoDocs := testBlueprintYAML + "\n---\n" + testBlueprintYAML
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(twoDocs))
	req.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two docs: status %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "blueprint must contain exactly one YAML document") {
		t.Fatalf("unexpected error response: %s", rec.Body)
	}

	trailingGarbage := testBlueprintYAML + "\n---\n{{{{ not yaml at all"
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/blueprint/import", strings.NewReader(trailingGarbage))
	req2.Header.Set("Content-Type", "application/yaml")
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("trailing garbage: status %d, want 400: %s", rec2.Code, rec2.Body)
	}
	if !strings.Contains(rec2.Body.String(), "blueprint must contain exactly one YAML document") {
		t.Fatalf("unexpected error response: %s", rec2.Body)
	}
}
