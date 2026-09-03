package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func fakeFunctionFetch(ref string) (*xpkg.Package, error) {
	return &xpkg.Package{
		Ref:    ref,
		Digest: "sha256:fnfeed",
		Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: autoreadies.autoready.fn.crossplane.io}
spec:
  group: autoready.fn.crossplane.io
  scope: Namespaced
  names: {kind: AutoReady, plural: autoreadies}
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          apiVersion: {type: string}
          kind: {type: string}
          ignore:
            type: array
            items: {type: string}
`)},
	}, nil
}

func TestFunctionAddCachesAndLocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	cmd := &FunctionAddCmd{
		Ref:      "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     lockPath,
		fetch:    fakeFunctionFetch,
	}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "1 function input schema") {
		t.Errorf("output = %q, want function input schema count", out.String())
	}

	got, err := cache.New(filepath.Join(dir, "cache")).Load(cmd.Ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "AutoReady" {
		t.Fatalf("cached CRDs = %+v, want one AutoReady", got)
	}
	if !got[0].IsFunctionInput() {
		t.Errorf("expected loaded CRD to be Function Input")
	}

	l, err := cache.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if len(l.Functions) != 1 || l.Functions[0].Digest != "sha256:fnfeed" {
		t.Errorf("lock functions = %+v, want the resolved digest pinned", l.Functions)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lockfile not written: %v", err)
	}
}
