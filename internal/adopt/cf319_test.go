package adopt

import (
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

func TestRoundTrip_AnnotationMetadataWire(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-ann-wire"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XTest",
				Plural:  "xtests",
				Scope:   "Namespaced",
			},
			Resources: []blueprint.Resource{
				{
					Name:     "dep",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
				},
				{
					Name:     "app",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					Annotations: map[string]blueprint.Field{
						"example.com/target-dep": {From: "resources.dep.metadata.name"},
					},
				},
			},
		},
	}

	kinds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	compBytes, err := emit.Composition(b, kinds)
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}

	adoptedBP, _, err := Adopt(compBytes, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v\nEmitted Composition:\n%s", err, string(compBytes))
	}

	app := adoptedBP.ResourceNamed("app")
	if app == nil {
		t.Fatalf("app resource not found in adopted blueprint")
	}

	ann, ok := app.Annotations["example.com/target-dep"]
	if !ok {
		t.Fatalf("annotation example.com/target-dep missing in adopted app: %+v", app.Annotations)
	}

	if ann.From != "resources.dep.metadata.name" {
		t.Fatalf("annotation From = %q, Raw = %q, want From = %q", ann.From, ann.Raw, "resources.dep.metadata.name")
	}
}

func TestMatchResourceRef_Variants(t *testing.T) {
	cases := []struct {
		input   string
		wantRes string
	}{
		// 1. Direct interpolation
		{"{{ $xr }}-dep", "dep"},
		{"{{- $xr -}}-dep", "dep"},
		{"{{ .observed.composite.resource.metadata.name }}-dep", "dep"},
		{"{{- .observed.composite.resource.metadata.name -}}-dep", "dep"},
		{"{{ $.observed.composite.resource.metadata.name }}-dep", "dep"},
		{"{{- $.observed.composite.resource.metadata.name -}}-dep", "dep"},
		// 2. Printf embedded format
		{`{{ printf "%s-dep" $xr | quote }}`, "dep"},
		{`{{- printf "%s-dep" $xr | quote -}}`, "dep"},
		{`{{ printf '%s-dep' $xr | quote }}`, "dep"},
		{`{{ printf "%s-dep" $xr }}`, "dep"},
		{`{{ (printf "%s-dep" $xr) | quote }}`, "dep"},
		{`{{ printf "%s-dep" .observed.composite.resource.metadata.name | quote }}`, "dep"},
		{`{{ printf "%s-dep" $.observed.composite.resource.metadata.name | quote }}`, "dep"},
		{`{{ printf "%s-dep" ($xr) | quote }}`, "dep"},
		{`{{ printf "%s-dep" $xr | b64enc }}`, "dep"},
		{`{{ printf "%s-dep" $xr | quote | b64enc }}`, "dep"},
		{`{{ printf "%s_dep" $xr | quote }}`, "dep"},
		// 3. Printf with resource name as 2nd arg
		{`{{ printf "%s-%s" $xr "dep" | quote }}`, "dep"},
		{`{{ printf "%s-%s" $xr 'dep' | quote }}`, "dep"},
		{`{{ printf "%s_%s" $xr "dep" | quote }}`, "dep"},
		// 4. Printf with resource name as 1st arg
		{`{{ printf "%s-%s" "dep" $xr | quote }}`, "dep"},
		{`{{ printf "%s_%s" "dep" $xr | quote }}`, "dep"},
		// Negatives
		{`{{ printf "%s-foo-%s" $xr $val }}`, ""},
		{`{{ printf "hello-%s" $xr }}`, ""},
		{`{{ $spec.name }}`, ""},
		{`{{ .observed.resources.dep.status.ready }}`, ""},
		{`{{ $xr }}-dep/extra`, ""},
		{`literal-value`, ""},
	}
	for _, tc := range cases {
		got := matchResourceRef(tc.input)
		if got != tc.wantRes {
			t.Errorf("matchResourceRef(%q) = %q, want %q", tc.input, got, tc.wantRes)
		}
	}
}

func TestAdoptGoTemplate_AnnotationAndFieldPrintfWires(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-printf-wires
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: dep
              annotations:
                crossplane.io/composition-resource-name: dep
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-app
              annotations:
                crossplane.io/composition-resource-name: test-app
                wire.ann1: '{{ printf "%s-dep" $xr | quote }}'
                wire.ann2: '{{ printf "%s-%s" $xr "dep" | quote }}'
            data:
              field1: '{{ printf "%s-dep" $xr | quote }}'
              field2: '{{ printf "%s-%s" $xr "dep" | quote }}'
              slice1:
                - '{{ printf "%s-dep" $xr | quote }}'
                - '{{ printf "%s-%s" $xr "dep" | quote }}'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	app := bp.ResourceNamed("test-app")
	if app == nil {
		t.Fatalf("test-app resource not found in adopted blueprint")
	}
	for _, annKey := range []string{"wire.ann1", "wire.ann2"} {
		if a, ok := app.Annotations[annKey]; !ok || a.From != "resources.dep.metadata.name" {
			t.Errorf("annotation %s = %+v, want From: resources.dep.metadata.name", annKey, a)
		}
	}
	for _, fldKey := range []string{"data[field1]", "data[field2]", "data[slice1][0]", "data[slice1][1]"} {
		if f, ok := app.Fields[fldKey]; !ok || f.From != "resources.dep.metadata.name" {
			t.Errorf("field %s = %+v, want From: resources.dep.metadata.name", fldKey, f)
		}
	}
}
