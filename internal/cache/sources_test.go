package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

const testCRDManifest = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: xwidgets.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XWidget
    plural: xwidgets
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size:
                  type: integer
`

func TestLoadSourcesCRDsRelativeAndAbsolute(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	// 1. Relative path to blueprintDir
	relSubdir := filepath.Join(bpDir, "crds")
	if err := os.MkdirAll(relSubdir, 0o755); err != nil {
		t.Fatal(err)
	}
	relFilePath := filepath.Join(relSubdir, "widget.yaml")
	if err := os.WriteFile(relFilePath, []byte(testCRDManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	bpRel := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "crds/widget.yaml"},
			},
		},
	}
	gotRel, err := LoadSources(store, bpRel, bpDir)
	if err != nil {
		t.Fatalf("LoadSources (relative path) failed: %v", err)
	}
	if len(gotRel) != 1 || gotRel[0].Kind != "XWidget" {
		t.Fatalf("LoadSources (relative path) unexpected result: %v", gotRel)
	}

	// 2. Absolute path
	absDir := t.TempDir()
	absFilePath := filepath.Join(absDir, "widget-abs.yaml")
	if err := os.WriteFile(absFilePath, []byte(testCRDManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	bpAbs := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: absFilePath},
			},
		},
	}
	gotAbs, err := LoadSources(store, bpAbs, bpDir)
	if err != nil {
		t.Fatalf("LoadSources (absolute path) failed: %v", err)
	}
	if len(gotAbs) != 1 || gotAbs[0].Kind != "XWidget" {
		t.Fatalf("LoadSources (absolute path) unexpected result: %v", gotAbs)
	}
}

func TestLoadSourcesProviderWithoutLock(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	providerRef := "xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0"
	crdList := []schema.CRD{
		{Group: "s3.aws.upbound.io", Kind: "Bucket", Plural: "buckets"},
	}
	if err := store.SaveCRDs(providerRef, "sha256:s3digest", crdList); err != nil {
		t.Fatal(err)
	}

	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: providerRef},
			},
		},
	}

	got, err := LoadSources(store, bp, bpDir)
	if err != nil {
		t.Fatalf("LoadSources failed: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "Bucket" {
		t.Fatalf("unexpected CRDs loaded: %v", got)
	}
}

func TestLoadSourcesProviderWithLock(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	lockedRef := "xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0"
	crdList := []schema.CRD{
		{Group: "s3.aws.upbound.io", Kind: "Bucket", Plural: "buckets"},
	}
	if err := store.SaveCRDs(lockedRef, "sha256:s3digest", crdList); err != nil {
		t.Fatal(err)
	}

	lock := &Lock{
		Providers: []LockEntry{
			{Ref: lockedRef, Digest: "sha256:s3digest"},
		},
	}
	if err := lock.Write(filepath.Join(bpDir, ".cf.lock")); err != nil {
		t.Fatal(err)
	}

	// 1. Blueprint uses bare provider name matching lock entry
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "provider-aws-s3"},
			},
		},
	}
	got, err := LoadSources(store, bp, bpDir)
	if err != nil {
		t.Fatalf("LoadSources with lock resolution failed: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "Bucket" {
		t.Fatalf("unexpected CRDs loaded: %v", got)
	}

	// 2. Fallback when locked ref is not in cache, but raw s.Provider is
	fallbackRef := "provider-fallback"
	fallbackCRDs := []schema.CRD{
		{Group: "example.org", Kind: "FallbackKind", Plural: "fallbackkinds"},
	}
	if err := store.SaveCRDs(fallbackRef, "sha256:fb", fallbackCRDs); err != nil {
		t.Fatal(err)
	}

	lock.Providers = append(lock.Providers, LockEntry{
		Ref:    "xpkg.upbound.io/upbound/provider-fallback:v2.0.0", // not in cache
		Digest: "sha256:notcached",
	})
	if err := lock.Write(filepath.Join(bpDir, ".cf.lock")); err != nil {
		t.Fatal(err)
	}

	bpFallback := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: fallbackRef},
			},
		},
	}
	gotFallback, err := LoadSources(store, bpFallback, bpDir)
	if err != nil {
		t.Fatalf("LoadSources fallback failed: %v", err)
	}
	if len(gotFallback) != 1 || gotFallback[0].Kind != "FallbackKind" {
		t.Fatalf("unexpected CRDs loaded from fallback: %v", gotFallback)
	}
}

func TestLoadSourcesPipelineAndLockFunctions(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	fnRef := "xpkg.crossplane.io/crossplane-contrib/function-patch-and-transform:v0.1.4"
	fnCRDs := []schema.CRD{
		{Group: "pt.fn.crossplane.io", Kind: "Input", Plural: "inputs"},
	}
	if err := store.SaveCRDs(fnRef, "sha256:fn", fnCRDs); err != nil {
		t.Fatal(err)
	}

	lock := &Lock{
		Functions: []LockEntry{
			{Ref: fnRef, Digest: "sha256:fn"},
		},
	}
	if err := lock.Write(filepath.Join(bpDir, ".cf.lock")); err != nil {
		t.Fatal(err)
	}

	// Pipeline step references function by name
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Pipeline: []blueprint.PipelineStep{
				{
					Name:        "patch-and-transform",
					FunctionRef: "function-patch-and-transform",
				},
			},
		},
	}

	got, err := LoadSources(store, bp, bpDir)
	if err != nil {
		t.Fatalf("LoadSources with pipeline function failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 function CRD, got %d", len(got))
	}

	// Pipeline step with explicit Package ref
	bpPkg := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Pipeline: []blueprint.PipelineStep{
				{
					Name:    "pt",
					Package: fnRef,
				},
			},
		},
	}
	gotPkg, err := LoadSources(store, bpPkg, bpDir)
	if err != nil {
		t.Fatalf("LoadSources with explicit package failed: %v", err)
	}
	if len(gotPkg) != 1 {
		t.Fatalf("expected 1 function CRD, got %d", len(gotPkg))
	}
}

func TestLoadSourcesFunctionDeduplication(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	fnRef1 := "xpkg.crossplane.io/crossplane-contrib/function-patch-and-transform:v0.1.4"
	fnCRDs1 := []schema.CRD{
		{Group: "pt.fn.crossplane.io", Kind: "Input", Plural: "inputs"},
	}
	if err := store.SaveCRDs(fnRef1, "sha256:fn1", fnCRDs1); err != nil {
		t.Fatal(err)
	}

	fnRef2 := "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.2.0"
	fnCRDs2 := []schema.CRD{
		{Group: "ar.fn.crossplane.io", Kind: "ReadyInput", Plural: "readyinputs"},
	}
	if err := store.SaveCRDs(fnRef2, "sha256:fn2", fnCRDs2); err != nil {
		t.Fatal(err)
	}

	lock := &Lock{
		Functions: []LockEntry{
			{Ref: fnRef1, Digest: "sha256:fn1"},
			{Ref: fnRef1, Digest: "sha256:fn1"}, // duplicate entry in lock
			{Ref: fnRef2, Digest: "sha256:fn2"},
		},
	}
	if err := lock.Write(filepath.Join(bpDir, ".cf.lock")); err != nil {
		t.Fatal(err)
	}

	// Multiple pipeline steps referencing same function fnRef1 (one by Package, one by FunctionRef)
	// and fnRef2 in lockfile.
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Pipeline: []blueprint.PipelineStep{
				{
					Name:    "pt1",
					Package: fnRef1,
				},
				{
					Name:        "pt2",
					FunctionRef: "function-patch-and-transform",
				},
			},
		},
	}

	got, err := LoadSources(store, bp, bpDir)
	if err != nil {
		t.Fatalf("LoadSources failed: %v", err)
	}
	// fnRef1 should be loaded once, fnRef2 should be loaded once -> exactly 2 CRDs
	if len(got) != 2 {
		t.Fatalf("expected 2 unique function CRDs, got %d", len(got))
	}
}

func TestLoadSourcesErrors(t *testing.T) {
	bpDir := t.TempDir()
	store := New(t.TempDir())

	// 1. Missing CRD file
	bpMissingCRD := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "nonexistent.yaml"},
			},
		},
	}
	if _, err := LoadSources(store, bpMissingCRD, bpDir); err == nil {
		t.Error("expected error when CRD file does not exist, got nil")
	}

	// 2. Corrupt CRD file
	corruptFile := filepath.Join(bpDir, "corrupt.yaml")
	if err := os.WriteFile(corruptFile, []byte("::not-yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	bpCorruptCRD := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "corrupt.yaml"},
			},
		},
	}
	if _, err := LoadSources(store, bpCorruptCRD, bpDir); err == nil {
		t.Error("expected error when CRD file contains invalid YAML, got nil")
	}

	// 3. Provider missing from cache (without lock)
	bpMissingProvider := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "example.org/missing-provider:v1"},
			},
		},
	}
	if _, err := LoadSources(store, bpMissingProvider, bpDir); err == nil {
		t.Error("expected error when provider is missing from cache, got nil")
	}

	// 4. Provider missing from cache (with lock entry pointing to uncached ref)
	lock := &Lock{
		Providers: []LockEntry{
			{Ref: "example.org/missing-provider:v2", Digest: "sha256:xxx"},
		},
	}
	if err := lock.Write(filepath.Join(bpDir, ".cf.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSources(store, bpMissingProvider, bpDir); err == nil {
		t.Error("expected error when locked provider is missing from cache, got nil")
	}
}

func TestLoadSources_CorruptLockfile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	if err := os.WriteFile(lockPath, []byte("<<<<<<< HEAD\ncorrupt json\n=======\n>>>>>>> branch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0"},
			},
		},
	}

	store := New(t.TempDir())
	if err := store.SaveCRDs("xpkg.upbound.io/upbound/provider-aws-sqs:v1.0.0", "sha256:sqs", nil); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSources(store, bp, dir)
	if err == nil {
		t.Fatal("LoadSources = nil, want error on corrupt .cf.lock")
	}
	if !strings.Contains(err.Error(), "parse") || !strings.Contains(err.Error(), ".cf.lock") {
		t.Fatalf("LoadSources err = %q, want lockfile parse error naming .cf.lock", err)
	}
}
