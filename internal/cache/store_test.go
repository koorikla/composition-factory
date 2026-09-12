package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	if diff := cmp.Diff(crds, got, cmpopts.IgnoreUnexported(schema.CRD{})); diff != "" {
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
	if diff := cmp.Diff(crdsA, gotA, cmpopts.IgnoreUnexported(schema.CRD{})); diff != "" {
		t.Errorf("provider A clobbered (-want +got):\n%s", diff)
	}

	gotB, err := s.Load(pkgB.Ref)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	if diff := cmp.Diff(crdsB, gotB, cmpopts.IgnoreUnexported(schema.CRD{})); diff != "" {
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

// TestLoadedCRDsCarryTheTreeMemo pins the server path: a CRD decoded from
// crds.json must memoise its schema trees like one built by ParseCRDs,
// otherwise every palette and inspector request rebuilds the tree.
func TestLoadedCRDsCarryTheTreeMemo(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	crds := []schema.CRD{{
		Group: "sqs.aws.m.upbound.io", Kind: "Queue", Plural: "queues", Scope: "Namespaced",
		Versions: []schema.Version{{
			Name: "v1beta1", Served: true, Storage: true,
			Properties: map[string]any{"spec": map[string]any{"properties": map[string]any{
				"forProvider": map[string]any{"properties": map[string]any{
					"region": map[string]any{"type": "string"},
				}},
			}}},
		}},
	}}
	const ref = "ghcr.io/example/provider-x:v1.0.0"
	if err := s.SaveCRDs(ref, "sha256:0000", crds); err != nil {
		t.Fatal(err)
	}
	loaded, err := (&Store{Root: s.Root}).Load(ref) // fresh Store: no memo, real decode
	if err != nil {
		t.Fatal(err)
	}
	first, err := loaded[0].ForProvider()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loaded[0].ForProvider()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || first[0] != second[0] {
		t.Fatalf("ForProvider() on a cache-loaded CRD rebuilt its tree; want the memoised graph")
	}
}

func TestStoreList(t *testing.T) {
	s := New(t.TempDir())
	refs, err := s.List()
	if err != nil {
		t.Fatalf("List on empty cache: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("List on empty cache returned %v, want empty", refs)
	}

	// Missing root dir returns nil, nil
	missingStore := New(filepath.Join(t.TempDir(), "nonexistent"))
	if missingRefs, err := missingStore.List(); err != nil || len(missingRefs) != 0 {
		t.Fatalf("List on nonexistent root: got (%v, %v), want (nil, nil)", missingRefs, err)
	}

	_ = s.SaveCRDs("example.org/b:v1", "sha256:1", nil)
	_ = s.SaveCRDs("example.org/a:v1", "sha256:2", nil)

	// Add a non-directory, an empty directory, and a directory with invalid JSON
	_ = os.WriteFile(filepath.Join(s.Root, "ignored.txt"), []byte("hi"), 0o644)
	_ = os.MkdirAll(filepath.Join(s.Root, "empty-dir"), 0o755)
	corruptDir := filepath.Join(s.Root, "corrupt-dir")
	_ = os.MkdirAll(corruptDir, 0o755)
	_ = os.WriteFile(filepath.Join(corruptDir, "crds.json"), []byte("{bad json"), 0o644)

	refs, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"example.org/a:v1", "example.org/b:v1"}
	if diff := cmp.Diff(want, refs); diff != "" {
		t.Errorf("List mismatch (-want +got):\n%s", diff)
	}
}

// TestStoreListDoesNotUnmarshalCRDs verifies that Store.List extracts provider
// refs without deserializing the CRD schema slice. If Store.List unmarshals
// into the full Entry struct, invalid/unsupported CRD schema payloads cause
// unmarshaling to fail and the provider ref to be silently dropped.
func TestStoreListDoesNotUnmarshalCRDs(t *testing.T) {
	s := New(t.TempDir())

	// Provider 1: crds field contains non-CRD data (array of strings instead of schema.CRD objects).
	// Provider 2: crds field contains a string instead of array.
	// Both are valid JSON and contain a valid "ref" field.
	p1 := filepath.Join(s.Root, "provider-one")
	if err := os.MkdirAll(p1, 0o755); err != nil {
		t.Fatal(err)
	}
	content1 := `{
  "ref": "example.org/provider-one:v1",
  "digest": "sha256:111",
  "crds": ["not-a-crd-schema-object"]
}`
	if err := os.WriteFile(filepath.Join(p1, "crds.json"), []byte(content1), 0o644); err != nil {
		t.Fatal(err)
	}

	p2 := filepath.Join(s.Root, "provider-two")
	if err := os.MkdirAll(p2, 0o755); err != nil {
		t.Fatal(err)
	}
	content2 := `{
  "ref": "example.org/provider-two:v1",
  "digest": "sha256:222",
  "crds": "not-an-array"
}`
	if err := os.WriteFile(filepath.Join(p2, "crds.json"), []byte(content2), 0o644); err != nil {
		t.Fatal(err)
	}

	// Provider 3: ref comes AFTER nested objects and arrays.
	p3 := filepath.Join(s.Root, "provider-three")
	if err := os.MkdirAll(p3, 0o755); err != nil {
		t.Fatal(err)
	}
	content3 := `{
  "meta": {
    "tags": ["alpha", "beta"],
    "nested": {"key": 123}
  },
  "digest": "sha256:333",
  "ref": "example.org/provider-three:v1",
  "crds": []
}`
	if err := os.WriteFile(filepath.Join(p3, "crds.json"), []byte(content3), 0o644); err != nil {
		t.Fatal(err)
	}

	// Provider 4: ref is not a string (should be skipped).
	p4 := filepath.Join(s.Root, "provider-four")
	if err := os.MkdirAll(p4, 0o755); err != nil {
		t.Fatal(err)
	}
	content4 := `{
  "ref": 12345,
  "crds": []
}`
	if err := os.WriteFile(filepath.Join(p4, "crds.json"), []byte(content4), 0o644); err != nil {
		t.Fatal(err)
	}

	// Provider 5: ref is empty string (should be skipped).
	p5 := filepath.Join(s.Root, "provider-five")
	if err := os.MkdirAll(p5, 0o755); err != nil {
		t.Fatal(err)
	}
	content5 := `{
  "ref": "",
  "crds": []
}`
	if err := os.WriteFile(filepath.Join(p5, "crds.json"), []byte(content5), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"example.org/provider-one:v1", "example.org/provider-three:v1", "example.org/provider-two:v1"}
	if diff := cmp.Diff(want, refs); diff != "" {
		t.Fatalf("List mismatch (-want +got):\n%s", diff)
	}
}

func BenchmarkStoreList(b *testing.B) {
	s := New(b.TempDir())

	// Create 10 cached providers, each with multiple CRDs containing realistic schema property trees.
	crds := make([]schema.CRD, 20)
	for i := range crds {
		crds[i] = schema.CRD{
			Group: fmt.Sprintf("group%d.example.org", i),
			Kind:  fmt.Sprintf("Kind%d", i),
			Versions: []schema.Version{{
				Name:   "v1beta1",
				Served: true,
				Properties: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"propA": map[string]any{"type": "string"},
							"propB": map[string]any{"type": "integer"},
							"nested": map[string]any{
								"field1": map[string]any{"type": "string"},
								"field2": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							},
						},
					},
				},
			}},
		}
	}

	for i := 0; i < 10; i++ {
		ref := fmt.Sprintf("example.org/provider-%02d:v1", i)
		if err := s.SaveCRDs(ref, fmt.Sprintf("sha256:%02d", i), crds); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		refs, err := s.List()
		if err != nil {
			b.Fatal(err)
		}
		if len(refs) != 10 {
			b.Fatalf("got %d refs, want 10", len(refs))
		}
	}
}

func TestLockFunctionsSupport(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), ".cf.lock")
	l, err := ReadLock(tmp)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	l.Set("xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0", "sha256:prov123")
	l.Set("xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0", "sha256:fn123")
	// Test updating existing function entry
	l.Set("xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0", "sha256:fn123-updated")

	if len(l.Providers) != 1 || l.Providers[0].Ref != "xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0" {
		t.Errorf("Providers: %v", l.Providers)
	}
	if len(l.Functions) != 1 || l.Functions[0].Digest != "sha256:fn123-updated" {
		t.Errorf("Functions: %v", l.Functions)
	}

	if err := l.Write(tmp); err != nil {
		t.Fatalf("Write: %v", err)
	}

	readBack, err := ReadLock(tmp)
	if err != nil {
		t.Fatalf("ReadLock after write: %v", err)
	}
	if diff := cmp.Diff(l, readBack); diff != "" {
		t.Errorf("ReadLock roundtrip diff (-want +got):\n%s", diff)
	}

	// Remove function
	if !readBack.Remove("xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0") {
		t.Errorf("Remove returned false for existing function")
	}
	if len(readBack.Functions) != 0 {
		t.Errorf("Functions after remove: %v", readBack.Functions)
	}
}

func TestLockFindFunction(t *testing.T) {
	var nilLock *Lock
	if _, ok := nilLock.FindFunction("anything"); ok {
		t.Fatal("expected nil Lock to return false")
	}

	l := &Lock{
		Functions: []LockEntry{
			{Ref: "xpkg.crossplane.io/crossplane-contrib/function-patch-and-transform:v0.1.4", Digest: "sha256:pt123"},
			{Ref: "xpkg.upbound.io/crossplane-contrib/function-kcl:v0.11.2", Digest: "sha256:kcl123"},
		},
	}

	// Exact ref match
	entry, ok := l.FindFunction("xpkg.crossplane.io/crossplane-contrib/function-patch-and-transform:v0.1.4")
	if !ok || entry.Digest != "sha256:pt123" {
		t.Errorf("exact ref match failed: got (%+v, %v)", entry, ok)
	}

	// Function name stripped like "function-patch-and-transform"
	entry, ok = l.FindFunction("function-patch-and-transform")
	if !ok || entry.Digest != "sha256:pt123" {
		t.Errorf("function-patch-and-transform match failed: got (%+v, %v)", entry, ok)
	}
	// Also test stripped to base name
	entry, ok = l.FindFunction("patch-and-transform")
	if !ok || entry.Digest != "sha256:pt123" {
		t.Errorf("patch-and-transform match failed: got (%+v, %v)", entry, ok)
	}

	// Prefix normalization like "fn-"
	entry, ok = l.FindFunction("fn-patch-and-transform")
	if !ok || entry.Digest != "sha256:pt123" {
		t.Errorf("fn- prefix match failed: got (%+v, %v)", entry, ok)
	}
	entry, ok = l.FindFunction("fn-kcl")
	if !ok || entry.Digest != "sha256:kcl123" {
		t.Errorf("fn-kcl match failed: got (%+v, %v)", entry, ok)
	}

	// Not found case
	if _, ok := l.FindFunction("function-nonexistent"); ok {
		t.Error("expected function-nonexistent to return false")
	}
	if _, ok := l.FindFunction(""); ok {
		t.Error("expected empty string to return false")
	}
}

func TestLockFindProvider(t *testing.T) {
	var nilLock *Lock
	if _, ok := nilLock.FindProvider("anything"); ok {
		t.Fatal("expected nil Lock to return false")
	}

	l := &Lock{
		Providers: []LockEntry{
			{Ref: "xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0", Digest: "sha256:sqs123"},
			{Ref: "xpkg.upbound.io/upbound/provider-aws-s3@sha256:s3digest", Digest: "sha256:s3digest"},
		},
	}

	// Exact match
	entry, ok := l.FindProvider("xpkg.upbound.io/upbound/provider-aws-sqs:v2.7.0")
	if !ok || entry.Digest != "sha256:sqs123" {
		t.Errorf("exact match failed: got (%+v, %v)", entry, ok)
	}

	// Tagged version / without tag (prefix with :)
	entry, ok = l.FindProvider("xpkg.upbound.io/upbound/provider-aws-sqs")
	if !ok || entry.Digest != "sha256:sqs123" {
		t.Errorf("provider prefix match failed: got (%+v, %v)", entry, ok)
	}

	// Digest prefix (prefix with @)
	entry, ok = l.FindProvider("xpkg.upbound.io/upbound/provider-aws-s3")
	if !ok || entry.Digest != "sha256:s3digest" {
		t.Errorf("provider @ digest match failed: got (%+v, %v)", entry, ok)
	}

	// Bare segment match
	entry, ok = l.FindProvider("provider-aws-sqs")
	if !ok || entry.Digest != "sha256:sqs123" {
		t.Errorf("provider bare segment match failed: got (%+v, %v)", entry, ok)
	}

	// Not found case
	if _, ok := l.FindProvider("provider-azure"); ok {
		t.Error("expected provider-azure to return false")
	}
	if _, ok := l.FindProvider(""); ok {
		t.Error("expected empty string to return false")
	}
}

func TestDefaultRoot(t *testing.T) {
	root := DefaultRoot()
	if root == "" {
		t.Fatal("DefaultRoot returned empty string")
	}
}

func TestStoreClear(t *testing.T) {
	s := New(t.TempDir())
	pkg := &xpkg.Package{Ref: "example.org/provider-test:v1", Digest: "sha256:abc"}
	if err := s.Save(pkg, []schema.CRD{{Kind: "Widget"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s.mu.RLock()
	if len(s.memo) == 0 {
		s.mu.RUnlock()
		t.Fatal("expected memo to be populated after Save")
	}
	s.mu.RUnlock()

	s.Clear()

	s.mu.RLock()
	if s.memo != nil {
		s.mu.RUnlock()
		t.Fatalf("expected s.memo to be nil after Clear, got %v", s.memo)
	}
	s.mu.RUnlock()

	got, err := s.Load(pkg.Ref)
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "Widget" {
		t.Fatalf("Load returned unexpected CRDs: %v", got)
	}
}

func TestFetchAndSave(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "cache"))
	lockPath := filepath.Join(dir, ".cf.lock")

	validCRDDoc := []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.test.org
spec:
  group: test.org
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`)

	ref := "example.org/provider-test:v1.0.0"
	ctx := context.Background()

	// 1. Success case
	pkg, crds, err := s.FetchAndSave(ctx, lockPath, ref, func(r string) (*xpkg.Package, error) {
		return &xpkg.Package{
			Ref:    r,
			Digest: "sha256:deadbeef",
			Docs:   [][]byte{validCRDDoc},
		}, nil
	})
	if err != nil {
		t.Fatalf("FetchAndSave failed: %v", err)
	}
	if len(crds) != 1 || crds[0].Kind != "Widget" {
		t.Fatalf("unexpected crds: %v", crds)
	}
	if pkg.Digest != "sha256:deadbeef" {
		t.Fatalf("unexpected digest: %s", pkg.Digest)
	}

	// Verify lock file written
	l, err := ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if entry, ok := l.FindProvider(ref); !ok || entry.Digest != "sha256:deadbeef" {
		t.Fatalf("lock missing provider entry: %v", l.Providers)
	}

	// 2. Fetch error
	_, _, err = s.FetchAndSave(ctx, lockPath, "bad/ref", func(string) (*xpkg.Package, error) {
		return nil, fmt.Errorf("fetch error")
	})
	if err == nil {
		t.Fatal("expected error on fetch failure")
	}

	// 3. Parse error
	_, _, err = s.FetchAndSave(ctx, lockPath, "invalid/yaml", func(r string) (*xpkg.Package, error) {
		return &xpkg.Package{
			Ref:    r,
			Digest: "sha256:111",
			Docs:   [][]byte{[]byte(":::invalid yaml")},
		}, nil
	})
	if err == nil {
		t.Fatal("expected error on parse failure")
	}
}

func TestFetchAndSaveShortCircuitsOnCacheHit(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "cache"))
	lockPath := filepath.Join(dir, ".cf.lock")

	ref := "example.org/cached-provider:v1"
	digest := "sha256:cafebabe"
	crds := []schema.CRD{{
		Group: "test.org", Kind: "Widget", Plural: "widgets",
		Scope: "Namespaced", Categories: []string{"managed"},
		Versions: []schema.Version{{
			Name: "v1", Served: true, Storage: true,
			Properties: map[string]any{"type": "object"},
		}},
	}}

	// Pre-populate cache
	if err := s.Save(&xpkg.Package{Ref: ref, Digest: digest}, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fetchCalled := false
	fetch := func(r string) (*xpkg.Package, error) {
		fetchCalled = true
		t.Fatalf("remote fetch invoked on cache hit for %s", r)
		return nil, fmt.Errorf("remote fetch invoked")
	}

	pkg, gotCRDs, err := s.FetchAndSave(context.Background(), lockPath, ref, fetch)
	if err != nil {
		t.Fatalf("FetchAndSave failed: %v", err)
	}
	if fetchCalled {
		t.Fatal("fetch was invoked despite cache hit")
	}
	if pkg.Ref != ref {
		t.Errorf("pkg.Ref = %q, want %q", pkg.Ref, ref)
	}
	if pkg.Digest != digest {
		t.Errorf("pkg.Digest = %q, want %q", pkg.Digest, digest)
	}
	if len(gotCRDs) != 1 || gotCRDs[0].Kind != "Widget" {
		t.Errorf("unexpected CRDs: %+v", gotCRDs)
	}

	// Verify lock pinning
	l, err := ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	entry, ok := l.FindProvider(ref)
	if !ok {
		t.Fatalf("provider %q not found in lockfile", ref)
	}
	if entry.Digest != digest {
		t.Errorf("lock entry digest = %q, want %q", entry.Digest, digest)
	}
}

func TestFetchAndSaveFunctionShortCircuitsOnCacheHit(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "cache"))
	lockPath := filepath.Join(dir, ".cf.lock")

	ref := "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0"
	digest := "sha256:f00dfeed"
	crds := []schema.CRD{{
		Group: "autoready.fn.crossplane.io", Kind: "AutoReady", Plural: "autoreadies",
		Function: true,
	}}

	if err := s.Save(&xpkg.Package{Ref: ref, Digest: digest}, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// nil fetch ensures no attempt is made to call network
	pkg, gotCRDs, err := s.FetchAndSave(context.Background(), lockPath, ref, nil)
	if err != nil {
		t.Fatalf("FetchAndSave failed: %v", err)
	}
	if pkg.Digest != digest {
		t.Errorf("pkg.Digest = %q, want %q", pkg.Digest, digest)
	}
	if len(gotCRDs) != 1 || gotCRDs[0].Kind != "AutoReady" {
		t.Errorf("unexpected CRDs: %+v", gotCRDs)
	}

	l, err := ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	entry, ok := l.FindFunction(ref)
	if !ok {
		t.Fatalf("function %q not pinned in lockfile", ref)
	}
	if entry.Digest != digest {
		t.Errorf("function entry digest = %q, want %q", entry.Digest, digest)
	}
}

func TestFetchAndSaveLockErrorPolicy(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "cache"))

	roDir := filepath.Join(dir, "ro")
	if err := os.MkdirAll(roDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
	unwritableLock := filepath.Join(roDir, ".cf.lock")

	ref := "example.org/cached-provider:v1"
	digest := "sha256:112233"
	crds := []schema.CRD{{Group: "test.org", Kind: "Widget", Plural: "widgets"}}
	if err := s.Save(&xpkg.Package{Ref: ref, Digest: digest}, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Cache hit with unwritable lock must fail with IsLockError
	_, _, err := s.FetchAndSave(context.Background(), unwritableLock, ref, nil)
	if err == nil {
		t.Fatal("expected error on unwritable lock, got nil")
	}
	if !IsLockError(err) {
		t.Errorf("expected LockError, got %T: %v", err, err)
	}

	// Cache miss with unwritable lock must also fail with IsLockError and NOT cache the new package
	uncachedRef := "example.org/uncached-provider:v1"
	validCRDDoc := []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.test.org}
spec:
  group: test.org
  scope: Namespaced
  names: {kind: Widget, plural: widgets}
  versions: [{name: v1, served: true, storage: true}]
`)
	_, _, err = s.FetchAndSave(context.Background(), unwritableLock, uncachedRef, func(r string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: r, Digest: "sha256:9988", Docs: [][]byte{validCRDDoc}}, nil
	})
	if err == nil {
		t.Fatal("expected error on unwritable lock during cache miss, got nil")
	}
	if !IsLockError(err) {
		t.Errorf("expected LockError, got %T: %v", err, err)
	}
	if _, err := s.Load(uncachedRef); err == nil {
		t.Error("package should not have been cached when lock writing failed")
	}
}

func TestSlugEmptyFallback(t *testing.T) {
	// ref where stripped segment is empty should fallback to "ref-<hash>"
	res := slug("///")
	if !strings.HasPrefix(res, "ref-") {
		t.Errorf("slug(\"///\") = %q, want prefix \"ref-\"", res)
	}
}

func TestReadLockInvalidJSONErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.lock")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(path); err == nil {
		t.Fatal("expected error reading invalid json lockfile, got nil")
	}
}

func TestSanitizeSlugSegment(t *testing.T) {
	if got := sanitizeSlugSegment("a/b:c@d!e_1.2-3"); got != "a_b_c_d_e_1.2-3" {
		t.Errorf("sanitizeSlugSegment = %q, want %q", got, "a_b_c_d_e_1.2-3")
	}
}

func TestPinLock(t *testing.T) {
	s := New(t.TempDir())

	// Empty lockPath is a no-op
	if err := s.PinLock("", "example.org/test:v1", "sha256:123"); err != nil {
		t.Fatalf("PinLock with empty lockPath: %v", err)
	}

	lockPath := filepath.Join(t.TempDir(), ".cf.lock")
	if err := s.PinLock(lockPath, "example.org/provider-test:v1", "sha256:abc"); err != nil {
		t.Fatalf("PinLock provider: %v", err)
	}
	if err := s.PinLock(lockPath, "xpkg.crossplane.io/crossplane-contrib/function-test:v1", "sha256:def"); err != nil {
		t.Fatalf("PinLock function: %v", err)
	}

	l, err := ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	p, ok := l.FindProvider("example.org/provider-test:v1")
	if !ok || p.Digest != "sha256:abc" {
		t.Errorf("provider entry = %+v, ok = %v", p, ok)
	}
	f, ok := l.FindFunction("xpkg.crossplane.io/crossplane-contrib/function-test:v1")
	if !ok || f.Digest != "sha256:def" {
		t.Errorf("function entry = %+v, ok = %v", f, ok)
	}
}

func TestLockFindProviderPrecedence(t *testing.T) {
	l := &Lock{
		Providers: []LockEntry{
			{Ref: "ghcr.io/crossplane-contrib/provider-aws:v0.40.0", Digest: "sha256:contrib"},
			{Ref: "xpkg.upbound.io/upbound/provider-aws:v1.0.0", Digest: "sha256:upbound"},
		},
	}
	entry, ok := l.FindProvider("xpkg.upbound.io/upbound/provider-aws:v1.0.0")
	if !ok {
		t.Fatal("expected to find provider")
	}
	if entry.Digest != "sha256:upbound" {
		t.Fatalf("expected digest sha256:upbound, got %s (returned ref: %s)", entry.Digest, entry.Ref)
	}
}

func TestLockFindFunctionPrecedence(t *testing.T) {
	l := &Lock{
		Functions: []LockEntry{
			{Ref: "internal.registry/custom/function-auto-ready:v0.1.0", Digest: "sha256:custom"},
			{Ref: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0", Digest: "sha256:official"},
		},
	}
	entry, ok := l.FindFunction("xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0")
	if !ok {
		t.Fatal("expected to find function")
	}
	if entry.Digest != "sha256:official" {
		t.Fatalf("expected digest sha256:official, got %s (returned ref: %s)", entry.Digest, entry.Ref)
	}
}

func TestLockFindProviderQualifiedRepoMismatch(t *testing.T) {
	l := &Lock{
		Providers: []LockEntry{
			{Ref: "ghcr.io/crossplane-contrib/provider-aws:v0.40.0", Digest: "sha256:contrib"},
		},
	}
	if _, ok := l.FindProvider("xpkg.upbound.io/upbound/provider-aws:v1.0.0"); ok {
		t.Fatal("expected qualified ref from different repository to return false, but matched")
	}
	if _, ok := l.FindProvider("xpkg.upbound.io/upbound/provider-aws"); ok {
		t.Fatal("expected untagged qualified ref from different repository to return false, but matched")
	}
	if entry, ok := l.FindProvider("provider-aws"); !ok || entry.Digest != "sha256:contrib" {
		t.Fatalf("expected bare segment fallback to find provider, got (%+v, %v)", entry, ok)
	}
}

func TestLockFindFunctionQualifiedRepoMismatch(t *testing.T) {
	l := &Lock{
		Functions: []LockEntry{
			{Ref: "internal.registry/custom/function-auto-ready:v0.1.0", Digest: "sha256:custom"},
		},
	}
	if _, ok := l.FindFunction("xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"); ok {
		t.Fatal("expected qualified ref from different repository to return false, but matched")
	}
	if _, ok := l.FindFunction("xpkg.upbound.io/crossplane-contrib/function-auto-ready"); ok {
		t.Fatal("expected untagged qualified ref from different repository to return false, but matched")
	}
	if entry, ok := l.FindFunction("function-auto-ready"); !ok || entry.Digest != "sha256:custom" {
		t.Fatalf("expected unqualified fallback to find function, got (%+v, %v)", entry, ok)
	}
	if entry, ok := l.FindFunction("auto-ready"); !ok || entry.Digest != "sha256:custom" {
		t.Fatalf("expected unqualified bare fallback to find function, got (%+v, %v)", entry, ok)
	}
}

func TestStoreLoadUncachedFunctionRefError(t *testing.T) {
	s := New(t.TempDir())
	ref := "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
	_, err := s.Load(ref)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "run: cf function add") {
		t.Errorf("got error %q, expected suggestion 'run: cf function add'", err.Error())
	}
	if strings.Contains(err.Error(), "provider ") {
		t.Errorf("function ref was called provider in error: %q", err.Error())
	}
}
