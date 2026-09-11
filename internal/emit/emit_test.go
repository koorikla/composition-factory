package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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
	b := testBlueprint()
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-central-1"}
	outs, err := Generate(b, testCRDs(t), "out")
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
	b1 := testBlueprint()
	b1.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-central-1"}
	a, err := Generate(b1, testCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (first run): %v", err)
	}
	b2 := testBlueprint()
	b2.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-central-1"}
	b, err := Generate(b2, testCRDs(t), "out")
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

func TestSourceHeaderDeterministic(t *testing.T) {
	for _, p := range []string{
		"custom/path/to/my-blueprint.cf.yaml",
		"/abs/path/to/my-blueprint.cf.yaml",
		"",
	} {
		b := testBlueprint()
		b.Spec.Resources[0].Fields["region"] = blueprint.Field{Value: "eu-central-1"}
		b.SetSourcePath(p)

		outs, err := Generate(b, testCRDs(t), "out")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}

		for _, o := range outs {
			if filepath.ToSlash(o.Path) == "out/providerconfigs/aws.yaml" {
				// ProviderConfig headers are sourced from provider package references
				continue
			}
			want := "# Source: " + b.Metadata.Name
			if !strings.Contains(string(o.Body), want) {
				t.Errorf("%s (sourcePath=%q): missing deterministic source header %q:\n%s", o.Path, p, want, string(o.Body))
			}
		}
	}
}

func TestGenerateRejectsMissingRequiredField(t *testing.T) {
	b := testBlueprint()
	// testBlueprint() omits "region" on Queue, which is required by testCRDs(t)
	_, err := Generate(b, testCRDs(t), "out")
	if err == nil {
		t.Fatal("expected error when required field is missing, got nil")
	}
	if !strings.Contains(err.Error(), `missing required field "region"`) {
		t.Fatalf("expected error mentioning missing required field \"region\", got: %v", err)
	}
}

func TestGenerateRejectsEmptyResourceMissingRequiredField(t *testing.T) {
	b := testBlueprint()
	// An empty resource (fields: {}) missing CRD-required fields (e.g. region on Queue)
	// must be refused during generation.
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{}
	_, err := Generate(b, testCRDs(t), "out")
	if err == nil {
		t.Fatal("expected error when required field is missing on empty resource, got nil")
	}
	if !strings.Contains(err.Error(), `missing required field "region"`) {
		t.Fatalf("expected error mentioning missing required field \"region\", got: %v", err)
	}
}
