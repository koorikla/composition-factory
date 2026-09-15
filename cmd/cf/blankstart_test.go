package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/index"
)

// A missing blueprint file scaffolds a blank, valid document — the "start
// with an empty canvas" path — and an existing file is never touched.
func TestEnsureBlueprintScaffoldsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.cf.yaml")
	created, err := ensureBlueprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("created = false for a missing file")
	}
	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("scaffold does not load back: %v", err)
	}
	if len(b.Spec.Resources) != 0 || len(b.Spec.Sources) != 0 {
		t.Fatalf("scaffold is not blank: %+v", b.Spec)
	}

	// second call: the existing file is left exactly as it is
	if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err = ensureBlueprint(path)
	if err != nil || created {
		t.Fatalf("existing file touched: created=%v err=%v", created, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "sentinel" {
		t.Fatal("existing file rewritten")
	}
}

// A source whose schemas are not cached must not kill startup: the server
// comes up with a partial index (native kinds intact), the missing ref is
// excluded from Providers, and the runtime auto-sync repairs it later.
func TestBuildAPIOptionsSurvivesUncachedSource(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	// append a second source nobody cached
	body, err := os.ReadFile(bp)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(body),
		"- provider: example.org/provider-test:v2",
		"- provider: example.org/provider-test:v2\n    - provider: example.org/provider-missing:v9", 1)
	if patched == string(body) {
		t.Fatal("fixture patch did not apply")
	}
	if err := os.WriteFile(bp, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	o, err := buildAPIOptions(bp, cacheDir, dir, filepath.Join(dir, ".cf.lock"), nil, false)
	if err != nil {
		t.Fatalf("startup died on an uncached source: %v", err)
	}
	for _, p := range o.Providers {
		if p == "example.org/provider-missing:v9" {
			t.Fatal("missing source claimed as a loaded provider")
		}
	}
	// the cached source still made it in
	found := false
	for _, p := range o.Providers {
		if p == "example.org/provider-test:v2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cached source lost: %v", o.Providers)
	}
}

// TestBlankBlueprintRetainsCachedProvidersAcrossSaves verifies CF-345 from the CLI wiring:
// when started with a blank blueprint and cached providers, saving the blueprint
// (PUT /api/blueprint) does NOT discard the cached providers or their kinds.
func TestBlankBlueprintRetainsCachedProvidersAcrossSaves(t *testing.T) {
	dir, _, cacheDir := seed(t)
	blankPath := filepath.Join(dir, "blank.cf.yaml")
	created, err := ensureBlueprint(blankPath)
	if err != nil || !created {
		t.Fatalf("ensureBlueprint failed: created=%v err=%v", created, err)
	}

	o, err := buildAPIOptions(blankPath, cacheDir, dir, filepath.Join(dir, ".cf.lock"), nil, false)
	if err != nil {
		t.Fatalf("buildAPIOptions: %v", err)
	}
	if len(o.CachedProviders) == 0 {
		t.Fatalf("expected CachedProviders to be populated, got empty")
	}

	h, err := api.New(o)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	// 1. Initial kinds should include the cached provider's kinds
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/kinds: %d", rec.Code)
	}
	var kindsResp struct{ Kinds []index.Kind }
	if err := json.NewDecoder(rec.Body).Decode(&kindsResp); err != nil {
		t.Fatalf("decode kinds: %v", err)
	}
	foundTestProviderKind := false
	for _, k := range kindsResp.Kinds {
		if k.Provider == "example.org/provider-test:v2" {
			foundTestProviderKind = true
			break
		}
	}
	if !foundTestProviderKind {
		t.Fatalf("expected example.org/provider-test:v2 kind in kinds, got %d kinds", len(kindsResp.Kinds))
	}

	// 2. Fetch current blueprint and PUT it back unchanged
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/blueprint", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/blueprint: %d", rec.Code)
	}
	docBytes, _ := io.ReadAll(rec.Body)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/blueprint", bytes.NewReader(docBytes))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint: %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. Kinds must STILL include the cached provider
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/kinds after save: %d", rec.Code)
	}
	var kindsRespAfter struct{ Kinds []index.Kind }
	if err := json.NewDecoder(rec.Body).Decode(&kindsRespAfter); err != nil {
		t.Fatalf("decode kinds after save: %v", err)
	}
	foundAfter := false
	for _, k := range kindsRespAfter.Kinds {
		if k.Provider == "example.org/provider-test:v2" {
			foundAfter = true
			break
		}
	}
	if !foundAfter {
		t.Fatalf("cached provider example.org/provider-test:v2 was dropped after PUT! got %d kinds", len(kindsRespAfter.Kinds))
	}

	// 4. /api/providers must STILL list the provider
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/providers after save: %d", rec.Code)
	}
	var provsResp struct {
		Providers []struct {
			Ref string `json:"ref"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&provsResp); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	foundProv := false
	for _, p := range provsResp.Providers {
		if p.Ref == "example.org/provider-test:v2" {
			foundProv = true
			break
		}
	}
	if !foundProv {
		t.Fatalf("cached provider example.org/provider-test:v2 missing from /api/providers after save")
	}
}
