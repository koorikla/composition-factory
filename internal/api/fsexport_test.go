package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// switchToFileSystem flips the served blueprint into FileSystem
// template-source mode through the public full-document PUT — the same
// path the canvas select uses.
func switchToFileSystem(t *testing.T, h http.Handler) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/blueprint", nil))
	if rec.Code != 200 {
		t.Fatalf("GET blueprint: %d %s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	doc["spec"].(map[string]any)["emit"] = map[string]any{"templateSource": "FileSystem"}
	body, _ := json.Marshal(doc)
	req := httptest.NewRequest("PUT", "/api/blueprint", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT blueprint with spec.emit: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"templateSource":"FileSystem"`) {
		t.Errorf("persisted document must carry spec.emit back: %s", rec.Body.String())
	}
}

func TestGenerateFileSystemModeListsTemplateFiles(t *testing.T) {
	h, _, _, _ := testServerParts(t)
	switchToFileSystem(t, h)

	req := httptest.NewRequest("POST", "/api/generate", strings.NewReader(`{"write":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Outputs []struct{ Path, Body string } `json:"outputs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, o := range resp.Outputs {
		paths = append(paths, o.Path)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"/runtime/", "/templates/", "000-context.yaml", "001-main-queue.yaml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("outputs missing %q:\n%s", want, joined)
		}
	}
}

func TestPackageRefusesFileSystemMode(t *testing.T) {
	h, _, _, _ := testServerParts(t)
	switchToFileSystem(t, h)

	for _, path := range []string{"/api/package", "/api/package?format=yaml"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 400 {
			t.Errorf("%s: status %d, want 400 in FileSystem mode: %s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "FileSystem") || !strings.Contains(rec.Body.String(), "Inline") {
			t.Errorf("%s: error should explain the mode and the way out: %s", path, rec.Body.String())
		}
	}
}
