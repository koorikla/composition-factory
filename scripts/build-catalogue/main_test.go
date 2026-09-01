package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/catalogue"
)

// TestRunFromFileEndToEnd drives run() exactly the way
// .github/workflows/catalogue.yml's own weekly job would if it were passed
// --from-file — the offline path every test in this package (and this
// repo's own dev sandbox, where a compiled Go binary cannot reach the
// network at all) uses instead of live mode. No test in this file, or
// anywhere else in this package, makes a real network request.
func TestRunFromFileEndToEnd(t *testing.T) {
	out := filepath.Join(t.TempDir(), "providers.json")
	var stdout, stderr bytes.Buffer

	err := run([]string{
		"--from-file", filepath.Join("testdata", "manifest.json"),
		"--out", out,
		"--org", "crossplane-contrib",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}

	raw := readFile(t, out)
	var entries []catalogue.Provider
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if err := catalogue.Validate(entries); err != nil {
		t.Fatalf("run produced an invalid catalogue: %v", err)
	}

	byName := make(map[string]catalogue.Provider, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (matching testdata/manifest.json): %+v", len(entries), entries)
	}
	if got := byName["function-go-templating"].Ref; got != "ghcr.io/crossplane-contrib/function-go-templating:v0.11.0" {
		t.Errorf("function-go-templating ref = %q, want the latest stable tag, pseudo-version excluded", got)
	}
	if got := byName["provider-aws"].Ref; got != "ghcr.io/crossplane-contrib/provider-aws:v0.48.0" {
		t.Errorf("provider-aws ref = %q, want v0.48.0", got)
	}
	if got := byName["provider-upjet-aws"].Ref; got != "" {
		t.Errorf("provider-upjet-aws ref = %q, want empty — no tags entry in the manifest at all, labelled not dropped", got)
	}
	if _, ok := byName["provider-upjet-aws"]; !ok {
		t.Error("provider-upjet-aws is missing from the catalogue entirely — an unresolved repo must still be labelled, not hidden")
	}

	if stdout.Len() == 0 {
		t.Error("run produced no stdout summary")
	}
}

func TestRunErrorsOnMissingManifest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"--from-file", filepath.Join(t.TempDir(), "nope.json")}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run with a missing manifest = nil error, want one")
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"--bogus-flag"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run with an unknown flag = nil error, want one")
	}
}
