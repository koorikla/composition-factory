package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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
