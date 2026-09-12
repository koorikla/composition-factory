package adopt

import (
	"strings"
	"testing"
)

func TestCF447AdoptAuxiliaryStepNamedRenderResources(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: gotemplates
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: example.org/v1
            kind: TestBucket
            metadata:
              name: b
    - step: render-resources
      functionRef:
        name: function-auto-ready
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("expected successful adopt, got error: %v", err)
	}
	if bp == nil {
		t.Fatalf("expected non-nil blueprint")
	}
	if bp.ResourceNamed("b") == nil {
		t.Fatalf("expected resource b to be adopted")
	}
}

func TestCF447AdoptAuxiliaryEnvironmentConfigsNamedRenderResources(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: render-resources
      functionRef:
        name: function-environment-configs
      input:
        apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
        kind: Input
        spec:
          environmentConfigs:
            - type: Reference
              ref:
                name: my-config
    - step: gotemplates
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: example.org/v1
            kind: TestBucket
            metadata:
              name: b
            spec:
              region: {{ $env.region }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("expected successful adopt, got error: %v", err)
	}
	if len(bp.Spec.EnvironmentConfigs) != 1 || bp.Spec.EnvironmentConfigs[0].Name != "my-config" {
		t.Errorf("expected my-config in EnvironmentConfigs, got %+v", bp.Spec.EnvironmentConfigs)
	}
	if bp.ResourceNamed("b") == nil {
		t.Fatalf("expected resource b to be adopted")
	}
}

func TestCF447AdoptAuxiliaryCustomFunctionNamedRenderResources(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-custom-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: gotemplates
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: example.org/v1
            kind: TestBucket
            metadata:
              name: b
    - step: render-resources
      functionRef:
        name: function-cel-filter
      input:
        apiVersion: cel.fn.crossplane.io/v1alpha1
        kind: Filter
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("expected successful adopt, got error: %v", err)
	}
	if len(bp.Spec.Pipeline) != 1 {
		t.Fatalf("expected 1 pipeline step, got %d: %+v", len(bp.Spec.Pipeline), bp.Spec.Pipeline)
	}
	step := bp.Spec.Pipeline[0]
	if step.Name != "cel-filter" {
		t.Errorf("expected disambiguated step name 'cel-filter', got %q", step.Name)
	}
	if step.FunctionRef != "function-cel-filter" {
		t.Errorf("expected FunctionRef 'function-cel-filter', got %q", step.FunctionRef)
	}
	if step.Position != "after" {
		t.Errorf("expected Position 'after', got %q", step.Position)
	}
}

func TestCF447AdoptAuxiliaryAutoReadyWithCustomStepsNamedRenderResources(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-autoready-custom
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: custom-filter
      functionRef:
        name: function-cel-filter
      input:
        apiVersion: cel.fn.crossplane.io/v1alpha1
        kind: Filter
    - step: gotemplates
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: example.org/v1
            kind: TestBucket
            metadata:
              name: b
    - step: render-resources
      functionRef:
        name: function-auto-ready
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("expected successful adopt, got error: %v", err)
	}
	// With custom steps present, auto-ready must be retained and disambiguated from render-resources
	var autoReadyStepFound, filterFound bool
	for _, s := range bp.Spec.Pipeline {
		if s.FunctionRef == "function-auto-ready" {
			autoReadyStepFound = true
			if s.Name != "auto-ready" {
				t.Errorf("expected auto-ready step name 'auto-ready', got %q", s.Name)
			}
		}
		if s.FunctionRef == "function-cel-filter" {
			filterFound = true
			if s.Name != "custom-filter" {
				t.Errorf("expected custom filter name 'custom-filter', got %q", s.Name)
			}
		}
	}
	if !autoReadyStepFound {
		t.Errorf("expected auto-ready step to be preserved in pipeline alongside custom steps")
	}
	if !filterFound {
		t.Errorf("expected custom-filter step in pipeline")
	}
}

func TestCF447AdoptAuxiliaryStepNameDeduplication(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-dedup
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: gotemplates
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: example.org/v1
            kind: TestBucket
            metadata:
              name: b
    - step: cel-filter
      functionRef:
        name: function-cel-filter-1
      input:
        apiVersion: cel.fn.crossplane.io/v1alpha1
        kind: Filter1
    - step: render-resources
      functionRef:
        name: function-cel-filter
      input:
        apiVersion: cel.fn.crossplane.io/v1alpha1
        kind: Filter2
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("expected successful adopt, got error: %v", err)
	}
	if len(bp.Spec.Pipeline) != 2 {
		t.Fatalf("expected 2 pipeline steps, got %d: %+v", len(bp.Spec.Pipeline), bp.Spec.Pipeline)
	}
	names := []string{bp.Spec.Pipeline[0].Name, bp.Spec.Pipeline[1].Name}
	if names[0] != "cel-filter" || names[1] != "cel-filter-2" {
		t.Errorf("expected steps ['cel-filter', 'cel-filter-2'], got %v", names)
	}
}

func TestCF447RefuseUnsupportedEnginesBasedOnFunctionIdentity(t *testing.T) {
	for _, engine := range []string{"function-kcl", "function-python"} {
		// Even when step name is something other than render-resources, unsupported engines are refused
		manifestCustomStep := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-engine-refusal
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  pipeline:
    - step: custom-step
      functionRef:
        name: ` + engine + `
`
		_, _, err := Adopt([]byte(manifestCustomStep), Options{})
		if err == nil {
			t.Fatalf("expected error for %s, got nil", engine)
		}
		if !strings.Contains(err.Error(), "cf adopt supports function-go-templating and function-patch-and-transform") {
			t.Errorf("expected engine refusal message, got: %v", err)
		}
		if !strings.Contains(err.Error(), engine) {
			t.Errorf("expected error to mention %s, got: %v", engine, err)
		}
	}
}
