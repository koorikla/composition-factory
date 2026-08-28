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
		"out/xrds/xqueues.platform.hooli.tech.yaml",
		"out/compositions/xqueues.platform.hooli.tech.yaml",
		"out/functions.yaml",
	} {
		if !got[want] {
			t.Errorf("missing output %q; got %v", want, got)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	a, _ := Generate(testBlueprint(), testCRDs(t), "out")
	b, _ := Generate(testBlueprint(), testCRDs(t), "out")
	if len(a) != len(b) {
		t.Fatalf("different file counts: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || string(a[i].Body) != string(b[i].Body) {
			t.Fatalf("output %q differs between runs", a[i].Path)
		}
	}
}
