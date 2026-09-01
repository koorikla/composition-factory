package emit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionsYAMLListsBothFunctions(t *testing.T) {
	got, err := Functions(testBlueprint())
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"kind: Function",
		"name: function-go-templating",
		"name: function-auto-ready",
		"render.crossplane.io/runtime-docker-name",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("functions.yaml missing %q\n---\n%s", want, s)
		}
	}
	if strings.Count(s, "kind: Function") != 2 {
		t.Errorf("want exactly 2 Function documents, got %d", strings.Count(s, "kind: Function"))
	}
}

func TestGenerateProducesThreeFilesAtStablePaths(t *testing.T) {
	outs, err := Generate(testBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got := map[string]bool{}
	for _, o := range outs {
		got[filepath.ToSlash(o.Path)] = true
	}
	for _, want := range []string{
		"out/xrds/xqueues.platform.sparky.ee.yaml",
		"out/compositions/xqueues.platform.sparky.ee.yaml",
		"out/functions.yaml",
	} {
		if !got[want] {
			t.Errorf("missing output %q; got %v", want, got)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	// Both errors are asserted, not discarded. With `a, _ := ...` twice, a
	// Generate that failed for any reason returned nil from both calls and
	// this test compared nothing to nothing and passed -- which is exactly
	// what would have happened the moment Generate started validating.
	a, err := Generate(testBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (first run): %v", err)
	}
	b, err := Generate(testBlueprint(), testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (second run): %v", err)
	}
	if len(a) == 0 {
		t.Fatal("Generate produced no output; there is nothing to compare")
	}
	if len(a) != len(b) {
		t.Fatalf("different file counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || string(a[i].Body) != string(b[i].Body) {
			t.Fatalf("output %q differs between runs", a[i].Path)
		}
	}
}
