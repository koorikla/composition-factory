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

// fakeFetch stands in for the network.
func fakeFetch(ref string) (*xpkg.Package, error) {
	return &xpkg.Package{
		Ref:    ref,
		Digest: "sha256:deadbeef",
		Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Widget, plural: widgets, categories: [managed]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)},
	}, nil
}

func TestProviderAddCachesAndLocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".cf.lock")
	cmd := &ProviderAddCmd{
		Ref:      "example.org/provider-test:v2",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     lockPath,
		fetch:    fakeFetch,
	}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "1 managed resource") {
		t.Errorf("output = %q, want a managed-resource count", out.String())
	}

	got, err := cache.New(filepath.Join(dir, "cache")).Load(cmd.Ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "Widget" {
		t.Fatalf("cached CRDs = %+v, want one Widget", got)
	}

	l, err := cache.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if len(l.Providers) != 1 || l.Providers[0].Digest != "sha256:deadbeef" {
		t.Errorf("lock = %+v, want the resolved digest pinned", l.Providers)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lockfile not written: %v", err)
	}
}

func TestProviderAddReportsZeroManagedResources(t *testing.T) {
	dir := t.TempDir()
	cmd := &ProviderAddCmd{
		Ref:      "example.org/family:v2",
		CacheDir: filepath.Join(dir, "cache"),
		Lock:     filepath.Join(dir, ".cf.lock"),
		fetch: func(ref string) (*xpkg.Package, error) {
			return &xpkg.Package{Ref: ref, Digest: "sha256:f00", Docs: [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: providerconfigs.test.example.org}
spec:
  group: test.example.org
  scope: Cluster
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)}}, nil
		},
	}
	var out bytes.Buffer
	if err := cmd.Run(&out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// A family package legitimately has none; say so rather than looking broken.
	if !strings.Contains(out.String(), "0 managed resources") {
		t.Errorf("output = %q, want it to report zero managed resources", out.String())
	}
}
