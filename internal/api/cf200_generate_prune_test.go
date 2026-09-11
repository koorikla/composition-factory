package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// TestGeneratePrunesOrphanedFiles verifies that POST /api/generate with write:true
// removes stale files in managed directories left by previous generations,
// matching the behavior of cf gen.
func TestGeneratePrunesOrphanedFiles(t *testing.T) {
	h, bpPath, _, outDir := testServerParts(t)

	// Step 1: Initial generation with the seeded blueprint.
	rec := do(t, h, "POST", "/api/generate", `{"write":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("first POST /api/generate: status %d: %s", rec.Code, rec.Body)
	}

	oldXRD := filepath.Join(outDir, "xrds", "xqueues.platform.sparky.ee.yaml")
	oldComp := filepath.Join(outDir, "compositions", "xqueues.platform.sparky.ee.yaml")
	if _, err := os.Stat(oldXRD); err != nil {
		t.Fatalf("expected initial XRD %s to exist: %v", oldXRD, err)
	}
	if _, err := os.Stat(oldComp); err != nil {
		t.Fatalf("expected initial composition %s to exist: %v", oldComp, err)
	}

	// Also place hand-written unmanaged files to ensure unmanaged files are preserved.
	unmanagedRoot := filepath.Join(outDir, "README.md")
	if err := os.WriteFile(unmanagedRoot, []byte("# hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unmanagedSub := filepath.Join(outDir, "custom", "app.yaml")
	if err := os.MkdirAll(filepath.Dir(unmanagedSub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmanagedSub, []byte("hand-written: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed stale files in other managed directories: environmentconfigs, providerconfigs, templates.
	staleEnv := filepath.Join(outDir, "environmentconfigs", "stale-env.yaml")
	if err := os.MkdirAll(filepath.Dir(staleEnv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleEnv, []byte("stale: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stalePC := filepath.Join(outDir, "providerconfigs", "stale-pc.yaml")
	if err := os.WriteFile(stalePC, []byte("stale: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	staleTplDir := filepath.Join(outDir, "templates", "old.group")
	staleTpl := filepath.Join(staleTplDir, "001-queue.yaml")
	if err := os.MkdirAll(staleTplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleTpl, []byte("stale: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 2: Update blueprint's XRD group from platform.sparky.ee to platform.other.ee
	bp := mustLoadBlueprint(t, bpPath)
	bp.Spec.XRD.Group = "platform.other.ee"
	bpBytes, err := json.Marshal(bp)
	if err != nil {
		t.Fatal(err)
	}
	putRec := do(t, h, "PUT", "/api/blueprint", string(bpBytes))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint: status %d: %s", putRec.Code, putRec.Body)
	}

	// Dry run preview with write:false must NOT prune anything
	previewRec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview POST /api/generate: status %d: %s", previewRec.Code, previewRec.Body)
	}
	if _, err := os.Stat(oldXRD); err != nil {
		t.Fatalf("dry-run preview must not prune old XRD: %v", err)
	}
	if _, err := os.Stat(oldComp); err != nil {
		t.Fatalf("dry-run preview must not prune old composition: %v", err)
	}
	if _, err := os.Stat(staleEnv); err != nil {
		t.Fatalf("dry-run preview must not prune stale environmentconfig: %v", err)
	}

	// Step 3: Second generation with write:true
	rec2 := do(t, h, "POST", "/api/generate", `{"write":true}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second POST /api/generate: status %d: %s", rec2.Code, rec2.Body)
	}

	newXRD := filepath.Join(outDir, "xrds", "xqueues.platform.other.ee.yaml")
	newComp := filepath.Join(outDir, "compositions", "xqueues.platform.other.ee.yaml")
	if _, err := os.Stat(newXRD); err != nil {
		t.Fatalf("expected new XRD %s to exist: %v", newXRD, err)
	}
	if _, err := os.Stat(newComp); err != nil {
		t.Fatalf("expected new composition %s to exist: %v", newComp, err)
	}

	// The stale files from the previous generation must be pruned
	for _, stale := range []string{oldXRD, oldComp, staleEnv, stalePC, staleTpl} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("stale file %s was not pruned", stale)
		}
	}

	// Empty parent directories in managed scope (e.g. templates/old.group and templates/) must be pruned
	if _, err := os.Stat(staleTplDir); !os.IsNotExist(err) {
		t.Errorf("stale template directory %s was not pruned", staleTplDir)
	}

	// Unmanaged files must be preserved
	for _, unmanaged := range []string{unmanagedRoot, unmanagedSub} {
		if _, err := os.Stat(unmanaged); err != nil {
			t.Errorf("unmanaged file %s was deleted: %v", unmanaged, err)
		}
	}
}

// TestGeneratePrunesOrphanedFilesLiteralRepro reproduces the exact issue #96 scenario:
// renaming XRD group foo1 -> foo2 prunes bars.foo1.yaml.
func TestGeneratePrunesOrphanedFilesLiteralRepro(t *testing.T) {
	h, _, _, outDir := testServerParts(t)

	bp1 := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "x"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "foo1",
				Kind:    "Bar",
				Plural:  "bars",
				Version: "v1",
				Scope:   "Namespaced",
			},
		},
	}
	bp1Bytes, err := json.Marshal(bp1)
	if err != nil {
		t.Fatal(err)
	}
	putRec1 := do(t, h, "PUT", "/api/blueprint", string(bp1Bytes))
	if putRec1.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint: status %d: %s", putRec1.Code, putRec1.Body)
	}

	rec1 := do(t, h, "POST", "/api/generate", `{"write":true}`)
	if rec1.Code != http.StatusOK {
		t.Fatalf("POST /api/generate 1: status %d: %s", rec1.Code, rec1.Body)
	}

	xrd1 := filepath.Join(outDir, "xrds", "bars.foo1.yaml")
	if _, err := os.Stat(xrd1); err != nil {
		t.Fatalf("expected initial XRD %s to exist: %v", xrd1, err)
	}

	bp2 := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "x"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "foo2",
				Kind:    "Bar",
				Plural:  "bars",
				Version: "v1",
				Scope:   "Namespaced",
			},
		},
	}
	bp2Bytes, err := json.Marshal(bp2)
	if err != nil {
		t.Fatal(err)
	}
	putRec2 := do(t, h, "PUT", "/api/blueprint", string(bp2Bytes))
	if putRec2.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint 2: status %d: %s", putRec2.Code, putRec2.Body)
	}

	rec2 := do(t, h, "POST", "/api/generate", `{"write":true}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /api/generate 2: status %d: %s", rec2.Code, rec2.Body)
	}

	xrd2 := filepath.Join(outDir, "xrds", "bars.foo2.yaml")
	if _, err := os.Stat(xrd2); err != nil {
		t.Fatalf("expected new XRD %s to exist: %v", xrd2, err)
	}

	if _, err := os.Stat(xrd1); !os.IsNotExist(err) {
		t.Errorf("stale XRD %s still exists alongside %s", xrd1, xrd2)
	}
}
