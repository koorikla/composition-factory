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
	// Properties is the actual payload the cache exists to preserve: nested
	// several levels deep, with both a nested map and a nested slice. Every
	// numeric literal below is a float64 (never an int), because decoding
	// arbitrary JSON into map[string]any always produces float64 for numbers
	// — using an int here would make cmp.Diff report a mismatch that has
	// nothing to do with a real bug in Save/Load.
	crds := []schema.CRD{{
		Group: "test.m.example.org", Kind: "Widget", Plural: "widgets",
		Scope: "Namespaced", Categories: []string{"managed"},
		Versions: []schema.Version{{
			Name: "v1beta1", Served: true, Storage: true,
			Properties: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spec": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"forProvider": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"region": map[string]any{
										"type":      "string",
										"maxLength": float64(63),
									},
									"tags": map[string]any{
										"type":  "array",
										"items": map[string]any{"type": "string"},
									},
								},
								"required": []any{"region"},
							},
						},
					},
				},
				"oneOf": []any{
					map[string]any{"required": []any{"spec"}},
					map[string]any{"type": "null"},
				},
			},
		}},
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

// TestSlugIsCollisionFreeAndStable covers the failure mode where a
// private-registry port number collides with a path segment: mapping '/',
// ':' and '@' all to the same separator would flatten "registry:5000/repo"
// and "registry/5000/repo" to the same directory name, and one provider's
// cached CRDs would silently overwrite another's.
func TestSlugIsCollisionFreeAndStable(t *testing.T) {
	refA := "registry:5000/repo"
	refB := "registry/5000/repo"

	slugA := slug(refA)
	slugB := slug(refB)
	if slugA == slugB {
		t.Fatalf("slug collision: slug(%q) == slug(%q) == %q", refA, refB, slugA)
	}

	if got := slug(refA); got != slugA {
		t.Errorf("slug(%q) not stable: got %q, want %q", refA, got, slugA)
	}
}

// TestSaveWithColludingRefsDoesNotClobberCache exercises the same collision
// through the public Save/Load API rather than the internal slug() helper,
// since that is the actual observable failure the fix must prevent.
func TestSaveWithColludingRefsDoesNotClobberCache(t *testing.T) {
	s := New(t.TempDir())
	pkgA := &xpkg.Package{Ref: "registry:5000/repo", Digest: "sha256:aaa"}
	pkgB := &xpkg.Package{Ref: "registry/5000/repo", Digest: "sha256:bbb"}
	crdsA := []schema.CRD{{Kind: "WidgetA"}}
	crdsB := []schema.CRD{{Kind: "WidgetB"}}

	if err := s.Save(pkgA, crdsA); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	if err := s.Save(pkgB, crdsB); err != nil {
		t.Fatalf("Save B: %v", err)
	}

	gotA, err := s.Load(pkgA.Ref)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	if diff := cmp.Diff(crdsA, gotA); diff != "" {
		t.Errorf("provider A clobbered (-want +got):\n%s", diff)
	}

	gotB, err := s.Load(pkgB.Ref)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	if diff := cmp.Diff(crdsB, gotB); diff != "" {
		t.Errorf("provider B clobbered (-want +got):\n%s", diff)
	}
}

func TestSaveThenLoadDigestRoundTrips(t *testing.T) {
	s := New(t.TempDir())
	pkg := &xpkg.Package{Ref: "example.org/provider-test:v2", Digest: "sha256:abc123"}
	if err := s.Save(pkg, []schema.CRD{{Kind: "Widget"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.LoadDigest(pkg.Ref)
	if err != nil {
		t.Fatalf("LoadDigest: %v", err)
	}
	if got != pkg.Digest {
		t.Errorf("LoadDigest = %q, want %q", got, pkg.Digest)
	}
}

func TestLoadDigestUnknownProviderErrors(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.LoadDigest("example.org/never-added:v1"); err == nil {
		t.Fatal("want an error loading the digest for a provider that was never added, got nil")
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

// TestDeleteEvictsExactlyTheGivenRef: after a Delete the ref is no longer
// loadable, a sibling entry survives untouched, and deleting an absent ref is
// a no-op rather than an error (the caller's intent is already satisfied).
func TestDeleteEvictsExactlyTheGivenRef(t *testing.T) {
	s := New(t.TempDir())
	keep := &xpkg.Package{Ref: "example.org/provider-keep:v1", Digest: "sha256:keep"}
	drop := &xpkg.Package{Ref: "example.org/provider-drop:v1", Digest: "sha256:drop"}
	for _, pkg := range []*xpkg.Package{keep, drop} {
		if err := s.Save(pkg, []schema.CRD{{Kind: "Widget"}}); err != nil {
			t.Fatalf("Save %s: %v", pkg.Ref, err)
		}
	}

	if err := s.Delete(drop.Ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load(drop.Ref); err == nil {
		t.Error("deleted ref is still loadable")
	}
	if _, err := s.Load(keep.Ref); err != nil {
		t.Errorf("sibling entry was evicted too: %v", err)
	}
	if err := s.Delete("example.org/never-added:v1"); err != nil {
		t.Errorf("Delete of an absent ref = %v, want nil (no-op)", err)
	}
}

// TestLockRemoveReportsPresence: Remove drops exactly the named pin and
// reports whether one was there, so a caller can skip rewriting an unchanged
// lockfile.
func TestLockRemoveReportsPresence(t *testing.T) {
	l := &Lock{}
	l.Set("example.org/a:v1", "sha256:aaa")
	l.Set("example.org/b:v1", "sha256:bbb")

	if !l.Remove("example.org/a:v1") {
		t.Error("Remove of a present ref = false, want true")
	}
	if l.Remove("example.org/a:v1") {
		t.Error("second Remove of the same ref = true, want false")
	}
	want := []LockEntry{{Ref: "example.org/b:v1", Digest: "sha256:bbb"}}
	if diff := cmp.Diff(want, l.Providers); diff != "" {
		t.Errorf("lock entries after Remove (-want +got):\n%s", diff)
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
