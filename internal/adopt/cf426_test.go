package adopt

import (
	"testing"
)

func TestAdoptGoTemplate_EnvForEachDefault(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-default
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- range $i := until (int (default 3 (index $env "count"))) }}
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              vpcId: vpc-123
          {{- end }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	envKey, ok := bp.Spec.Environment["count"]
	if !ok {
		t.Fatalf("expected count in bp.Spec.Environment")
	}
	if envKey.Default != "3" {
		t.Errorf("expected count.Default = %q, got %q", "3", envKey.Default)
	}
}
