package adopt

import (
	"testing"
)

func TestCF409_AdoptMultilineTemplateAction(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-multiline
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          ---
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: {{
              .observed.composite.resource.spec.name
            }}
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatalf("expected blueprint, got nil")
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d (report: %+v)", len(bp.Spec.Resources), report)
	}
	res := bp.Spec.Resources[0]
	if res.Kind != "ConfigMap" {
		t.Errorf("expected kind ConfigMap, got %q", res.Kind)
	}
}
