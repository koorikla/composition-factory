package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

const envCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: apps.example.org
spec:
  group: example.org
  names: {kind: App, plural: apps}
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                image: {type: string}
                env:
                  type: array
                  items:
                    type: object
                    properties:
                      name: {type: string}
                      value: {type: string}
`

func nativeArrayFixture(t *testing.T, fields map[string]blueprint.Field) (*blueprint.Blueprint, []schema.CRD) {
	t.Helper()
	crds, err := schema.ParseCRDManifest([]byte(envCRD))
	if err != nil {
		t.Fatal(err)
	}
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xenv"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{CRDs: "crds/app.yaml"}},
			XRD: blueprint.XRD{
				Group: "platform.example.org", Kind: "XEnv", Plural: "xenvs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "app", Kind: "App",
				Provider: "crds/app.yaml",
				Fields:   fields,
			}},
		},
	}
	return b, crds
}

// env[0] and env[1] render a real two-element sequence, elements in index
// order — the shape the workload-selector helper (slice 58) emits.
func TestNativeMultiElementArray(t *testing.T) {
	b, crds := nativeArrayFixture(t, map[string]blueprint.Field{
		"spec.env[0].name":  {Value: "PORT"},
		"spec.env[0].value": {Value: "8080"},
		"spec.env[1].name":  {Value: "MODE"},
		"spec.env[1].value": {Value: "prod"},
	})
	got, err := Composition(b, crds)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	i0 := strings.Index(s, "PORT")
	i1 := strings.Index(s, "MODE")
	if i0 < 0 || i1 < 0 || i0 > i1 {
		t.Fatalf("elements missing or out of order (PORT@%d, MODE@%d):\n%s", i0, i1, s)
	}
	if strings.Count(s, "- name:") < 2 && strings.Count(s, "-") < 2 {
		t.Fatalf("not a two-element sequence:\n%s", s)
	}
}

// Indices must be contiguous from zero: env[2] with no env[1] would render
// a list whose positions silently disagree with the written indices.
func TestNativeArrayIndexGapRefused(t *testing.T) {
	b, crds := nativeArrayFixture(t, map[string]blueprint.Field{
		"spec.env[0].name": {Value: "A"},
		"spec.env[2].name": {Value: "C"},
	})
	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("index gap not refused: %v", err)
	}
}

// The schema lookup normalizes any index: a typo inside element [1] is
// still caught against the element schema.
func TestNativeArrayElementTypoStillCaught(t *testing.T) {
	b, crds := nativeArrayFixture(t, map[string]blueprint.Field{
		"spec.env[1].nmae": {Value: "x"},
	})
	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "nmae") {
		t.Fatalf("element typo not refused: %v", err)
	}
}
