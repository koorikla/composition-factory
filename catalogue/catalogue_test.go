package catalogue

import "testing"

// TestEmbeddedCatalogueHasUpjetFamilyServices pins the fix for this
// catalogue's "searching for provider-aws-rds finds nothing" gap: the upjet
// provider families (provider-upjet-aws, provider-upjet-gcp, ...) publish
// per-service ghcr.io/crossplane-contrib/provider-<cloud>-<service> images
// with no GitHub repository of their own, so scripts/build-catalogue has to
// synthesize an entry for each one (see scripts/build-catalogue/family.go)
// rather than only ever enumerating GitHub repositories.
//
// These three are pinned directly, by name, with a non-empty Ref: all three
// are established, actively published AWS/GCP service packages (this
// project's own testdata and README install
// ghcr.io/crossplane-contrib/provider-aws-sqs — a sibling of
// provider-aws-rds and provider-aws-s3 from the very same family) stable
// enough not to disappear from crossplane-contrib's catalogue between
// regenerations, unlike a specific version tag, which this test
// deliberately does not pin.
func TestEmbeddedCatalogueHasUpjetFamilyServices(t *testing.T) {
	entries, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	byName := make(map[string]Provider, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	for _, name := range []string{"provider-aws-rds", "provider-aws-s3", "provider-gcp-storage"} {
		e, ok := byName[name]
		if !ok {
			t.Errorf("%s is missing from the embedded catalogue entirely", name)
			continue
		}
		if e.Ref == "" {
			t.Errorf("%s has an empty Ref — want a resolved, pullable ghcr.io image reference", name)
		}
	}
}

// TestEmbeddedCatalogueIsValid is the gate scripts/build-catalogue's own
// output has to clear: the file actually committed at
// catalogue/providers.json parses, is non-empty, and is sorted by Name with
// no duplicates. This is what "Tests: ... catalogue validity
// (parses/sorted/non-empty)" means in practice — running it here, against
// the real embedded file, is what makes a regenerated-but-broken
// providers.json fail `go test ./...` instead of only failing at request
// time.
func TestEmbeddedCatalogueIsValid(t *testing.T) {
	entries, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("catalogue/providers.json is empty")
	}
	if err := Validate(entries); err != nil {
		t.Fatalf("embedded catalogue is invalid: %v", err)
	}
}

// TestValidateCatchesEachInvariant pins each of Validate's three checks
// (empty, blank name, sort/duplicate order) against small hand-built slices
// — a network-free complement to TestEmbeddedCatalogueIsValid, which only
// ever exercises the "everything is fine" path against the real file.
func TestValidateCatchesEachInvariant(t *testing.T) {
	cases := []struct {
		name    string
		entries []Provider
		wantErr bool
	}{
		{"empty", nil, true},
		{"blank name", []Provider{{Name: ""}}, true},
		{"out of order", []Provider{{Name: "b"}, {Name: "a"}}, true},
		{"duplicate name", []Provider{{Name: "a"}, {Name: "a"}}, true},
		{"valid", []Provider{{Name: "a"}, {Name: "b"}, {Name: "c"}}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.entries)
			if tt.wantErr && err == nil {
				t.Errorf("Validate(%+v) = nil, want an error", tt.entries)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate(%+v) = %v, want nil", tt.entries, err)
			}
		})
	}
}
