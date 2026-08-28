package cache

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestSaveThenLoadRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	pkg := &xpkg.Package{Ref: "example.org/provider-test:v2", Digest: "sha256:abc123"}
	crds := []schema.CRD{{
		Group: "test.m.example.org", Kind: "Widget", Plural: "widgets",
		Scope: "Namespaced", Categories: []string{"managed"},
		Versions: []schema.Version{{Name: "v1beta1", Served: true, Storage: true}},
	}}
	if err := s.Save(pkg, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load(pkg.Ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if diff := cmp.Diff(crds, got); diff != "" {
		t.Errorf("round trip (-want +got):\n%s", diff)
	}
}

func TestLoadUnknownProviderErrors(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Load("example.org/never-added:v1"); err == nil {
		t.Fatal("want an error loading a provider that was never added, got nil")
	}
}

func TestLockSetIsIdempotentAndSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cf.lock")
	l := &Lock{}
	l.Set("example.org/b:v1", "sha256:bbb")
	l.Set("example.org/a:v1", "sha256:aaa")
	l.Set("example.org/b:v1", "sha256:bbb2") // same ref, new digest -> replace
	if err := l.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := ReadLock(path)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	want := []LockEntry{
		{Ref: "example.org/a:v1", Digest: "sha256:aaa"},
		{Ref: "example.org/b:v1", Digest: "sha256:bbb2"},
	}
	if diff := cmp.Diff(want, got.Providers); diff != "" {
		t.Errorf("lock entries (-want +got):\n%s", diff)
	}
}

func TestReadLockMissingFileIsEmptyNotAnError(t *testing.T) {
	l, err := ReadLock(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("ReadLock on a missing file should succeed: %v", err)
	}
	if len(l.Providers) != 0 {
		t.Errorf("got %d providers, want 0", len(l.Providers))
	}
}
