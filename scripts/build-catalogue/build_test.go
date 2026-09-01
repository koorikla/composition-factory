package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/koorikla/compositionfactory/catalogue"
)

func TestLatestStableTagPicksTheHighestStrictSemver(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{"simple ascending", []string{"v0.9.2", "v0.10.0", "v0.11.0"}, "v0.11.0"},
		{"unordered input", []string{"v0.11.0", "v0.1.0", "v0.9.2"}, "v0.11.0"},
		{
			"pseudo-versions excluded",
			[]string{"v0.9.2", "v0.0.0-20251028114116-30cc3a089783", "v0.0.0-20250909143331-9a9327a5d338"},
			"v0.9.2",
		},
		{"prerelease excluded", []string{"v1.0.0", "v1.1.0-rc1"}, "v1.0.0"},
		{"only pseudo-versions", []string{"v0.0.0-20251028114116-30cc3a089783"}, ""},
		{"empty", nil, ""},
		{"double digit minor beats single digit major.minor lexically", []string{"v0.9.0", "v0.10.0"}, "v0.10.0"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := latestStableTag(tt.tags); got != tt.want {
				t.Errorf("latestStableTag(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestBuildRefEmptyTagYieldsEmptyRef(t *testing.T) {
	r := repo{Name: "function-go-templating", Owner: "crossplane-contrib"}
	if got := buildRef(r, ""); got != "" {
		t.Errorf("buildRef with no resolved tag = %q, want empty — label, don't hide", got)
	}
	want := "ghcr.io/crossplane-contrib/function-go-templating:v0.11.0"
	if got := buildRef(r, "v0.11.0"); got != want {
		t.Errorf("buildRef = %q, want %q", got, want)
	}
}

func TestLicenseOrDefaultsToNOASSERTION(t *testing.T) {
	if got := licenseOr(""); got != "NOASSERTION" {
		t.Errorf("licenseOr(\"\") = %q, want NOASSERTION", got)
	}
	if got := licenseOr("Apache-2.0"); got != "Apache-2.0" {
		t.Errorf("licenseOr(Apache-2.0) = %q, want it passed through unchanged", got)
	}
}

// TestBuildCatalogueLabelsUnresolvedRepositoriesRatherThanDroppingThem is
// the policy test: a repo with no resolvable ghcr.io tag still gets a
// Provider entry, with Ref == "".
func TestBuildCatalogueLabelsUnresolvedRepositoriesRatherThanDroppingThem(t *testing.T) {
	repos := []repo{
		{Name: "function-go-templating", Owner: "crossplane-contrib", Description: "go templates", SourceURL: "https://github.com/crossplane-contrib/function-go-templating", LicenseSPDX: "Apache-2.0"},
		{Name: "provider-upjet-aws", Owner: "crossplane-contrib", Description: "aws generator", SourceURL: "https://github.com/crossplane-contrib/provider-upjet-aws", LicenseSPDX: ""},
	}
	tags := map[string][]string{
		"function-go-templating": {"v0.9.2", "v0.10.0", "v0.11.0", "v0.0.0-20251028114116-30cc3a089783"},
		// provider-upjet-aws intentionally absent: no resolvable ghcr package.
	}

	got := buildCatalogue(repos, tags)
	want := []catalogue.Provider{
		{
			Name:        "function-go-templating",
			Ref:         "ghcr.io/crossplane-contrib/function-go-templating:v0.11.0",
			Description: "go templates",
			Source:      "https://github.com/crossplane-contrib/function-go-templating",
			License:     "Apache-2.0",
		},
		{
			Name:        "provider-upjet-aws",
			Ref:         "", // labelled, not dropped
			Description: "aws generator",
			Source:      "https://github.com/crossplane-contrib/provider-upjet-aws",
			License:     "NOASSERTION",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCatalogue = %+v, want %+v", got, want)
	}
}

// TestBuildCatalogueSortsByNameRegardlessOfInputOrder pins the determinism
// requirement directly: two calls with the same repos in different input
// order produce byte-identical (via reflect.DeepEqual on the ordered slice)
// results.
func TestBuildCatalogueSortsByNameRegardlessOfInputOrder(t *testing.T) {
	repos := []repo{
		{Name: "provider-c", Owner: "x"},
		{Name: "provider-a", Owner: "x"},
		{Name: "provider-b", Owner: "x"},
	}
	got := buildCatalogue(repos, nil)
	if len(got) != 3 || got[0].Name != "provider-a" || got[1].Name != "provider-b" || got[2].Name != "provider-c" {
		t.Errorf("buildCatalogue did not sort by Name: %+v", got)
	}
}

func TestWriteCatalogueIsDeterministicAndCreatesParentDirs(t *testing.T) {
	entries := []catalogue.Provider{
		{Name: "function-a", Ref: "ghcr.io/x/function-a:v1.0.0", Description: "d", Source: "s", License: "Apache-2.0"},
		{Name: "provider-b", Ref: "", Description: "d2", Source: "s2", License: "NOASSERTION"},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "providers.json")

	if err := writeCatalogue(path, entries); err != nil {
		t.Fatalf("writeCatalogue: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}

	if err := writeCatalogue(path, entries); err != nil {
		t.Fatalf("writeCatalogue (second run): %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file (second run): %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("writeCatalogue is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if first[len(first)-1] != '\n' {
		t.Error("written file does not end with a trailing newline")
	}

	var roundTripped []catalogue.Provider
	if err := json.Unmarshal(first, &roundTripped); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if !reflect.DeepEqual(roundTripped, entries) {
		t.Errorf("round-tripped entries = %+v, want %+v", roundTripped, entries)
	}
}

func TestWriteCatalogueRefusesInvalidCatalogue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")

	// Two entries with the same name: Validate must reject this before
	// anything is written.
	entries := []catalogue.Provider{
		{Name: "dup", Ref: "", Description: "", Source: "", License: "NOASSERTION"},
		{Name: "dup", Ref: "", Description: "", Source: "", License: "NOASSERTION"},
	}
	if err := writeCatalogue(path, entries); err == nil {
		t.Fatal("writeCatalogue with a duplicate-name catalogue = nil error, want one")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("writeCatalogue left a file behind despite rejecting the catalogue as invalid")
	}
}
