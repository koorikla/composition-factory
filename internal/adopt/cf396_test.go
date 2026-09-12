// internal/adopt/cf396_test.go
package adopt

import (
	"testing"
)

func TestCF396_AdoptMultiEnvironmentConfigMixedScalarWidening(t *testing.T) {
	t.Run("string_then_int", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: dev-env
data:
  metricPort: "unspecified"
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: prod-env
data:
  metricPort: 8080
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if bp == nil {
			t.Fatalf("expected blueprint, got nil")
		}
		envKey, ok := bp.Spec.Environment["metricPort"]
		if !ok {
			t.Fatalf("expected metricPort in bp.Spec.Environment")
		}
		if envKey.Type != "string" {
			t.Errorf("expected metricPort type to widen to 'string', got %q", envKey.Type)
		}
	})

	t.Run("int_then_string", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: prod-env
data:
  metricPort: 8080
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: dev-env
data:
  metricPort: "unspecified"
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if bp == nil {
			t.Fatalf("expected blueprint, got nil")
		}
		envKey, ok := bp.Spec.Environment["metricPort"]
		if !ok {
			t.Fatalf("expected metricPort in bp.Spec.Environment")
		}
		if envKey.Type != "string" {
			t.Errorf("expected metricPort type to widen to 'string', got %q", envKey.Type)
		}
	})

	t.Run("int_then_number", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: dev-env
data:
  ratio: 1
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: prod-env
data:
  ratio: 1.5
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if bp == nil {
			t.Fatalf("expected blueprint, got nil")
		}
		envKey, ok := bp.Spec.Environment["ratio"]
		if !ok {
			t.Fatalf("expected ratio in bp.Spec.Environment")
		}
		if envKey.Type != "number" {
			t.Errorf("expected ratio type to widen to 'number', got %q", envKey.Type)
		}
	})

	t.Run("number_then_int", func(t *testing.T) {
		manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: dev-env
data:
  ratio: 1.5
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: prod-env
data:
  ratio: 1
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if bp == nil {
			t.Fatalf("expected blueprint, got nil")
		}
		envKey, ok := bp.Spec.Environment["ratio"]
		if !ok {
			t.Fatalf("expected ratio in bp.Spec.Environment")
		}
		if envKey.Type != "number" {
			t.Errorf("expected ratio type to widen to 'number', got %q", envKey.Type)
		}
	})
}

func TestWidenEnvType(t *testing.T) {
	cases := []struct {
		t1, t2   string
		expected string
	}{
		{"string", "string", "string"},
		{"integer", "integer", "integer"},
		{"number", "number", "number"},
		{"boolean", "boolean", "boolean"},
		{"string", "integer", "string"},
		{"integer", "string", "string"},
		{"string", "number", "string"},
		{"number", "string", "string"},
		{"string", "boolean", "string"},
		{"boolean", "string", "string"},
		{"integer", "number", "number"},
		{"number", "integer", "number"},
		{"boolean", "integer", "string"},
		{"integer", "boolean", "string"},
		{"boolean", "number", "string"},
		{"number", "boolean", "string"},
		{"", "integer", "string"},
		{"integer", "", "string"},
	}

	for _, tc := range cases {
		got := widenEnvType(tc.t1, tc.t2)
		if got != tc.expected {
			t.Errorf("widenEnvType(%q, %q) = %q, want %q", tc.t1, tc.t2, got, tc.expected)
		}
	}
}
