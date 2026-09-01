package catalogue

import "testing"

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
