package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// xrCRD is the "existing composition claim" case: the CRD Crossplane
// generates for another composition's XR, scanned into a crds: source.
const xrCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: xdatabases.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XDatabase
    plural: xdatabases
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                engine: {type: string}
                sizeGB: {type: integer}
            status:
              properties:
                ready: {type: boolean}
`

func crdSourceFixture(t *testing.T) (*blueprint.Blueprint, []schema.CRD) {
	t.Helper()
	scanned, err := schema.ParseCRDManifest([]byte(xrCRD))
	if err != nil {
		t.Fatal(err)
	}
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "crds/xdatabase.yaml"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"dbEngine":     {Type: "string", Required: true},
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "db",
					Kind:     "XDatabase",
					Provider: "crds/xdatabase.yaml",
					Fields: map[string]blueprint.Field{
						"spec.engine": {From: "params.dbEngine"},
						"spec.sizeGB": {Value: "20"},
					},
				},
			},
		},
	}
	return b, scanned
}

func TestScannedCRDKindEmitsObjectRooted(t *testing.T) {
	b, crds := crdSourceFixture(t)
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, err := Composition(b, crds)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"kind: XDatabase",
		"apiVersion: platform.example.org/v1alpha1",
		"engine:",
		"sizeGB:",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("composition missing %q:\n%s", want, s)
		}
	}
	// object-rooted: the composed document IS the object
	for _, reject := range []string{"forProvider", "providerConfigRef"} {
		if strings.Contains(s, reject) {
			t.Errorf("scanned kind must not carry %q:\n%s", reject, s)
		}
	}
}

func TestScannedCRDTypoFieldFailsLoudly(t *testing.T) {
	b, crds := crdSourceFixture(t)
	b.Spec.Resources[0].Fields["spec.engnie"] = blueprint.Field{Value: "x"}
	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "engnie") {
		t.Fatalf("typo'd field not refused: %v", err)
	}
}

func TestScannedCRDStatusWireResolves(t *testing.T) {
	b, crds := crdSourceFixture(t)
	// a second resource wiring from the scanned kind's status
	b.Spec.Resources = append(b.Spec.Resources, blueprint.Resource{
		Name: "app", Kind: "XDatabase", Provider: "crds/xdatabase.yaml",
		Fields: map[string]blueprint.Field{
			"spec.engine": {From: "resources.db.status.ready"},
		},
	})
	if _, err := Composition(b, crds); err != nil {
		t.Fatalf("status wire against a scanned kind: %v", err)
	}
}

func TestConfigurationMetaSkipsCRDSources(t *testing.T) {
	b, _ := crdSourceFixture(t)
	got, err := ConfigurationMeta(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	// a CRD manifest is not an installable package: no dependsOn entry
	if strings.Contains(string(got), "crds/xdatabase.yaml") {
		t.Errorf("crds source leaked into dependsOn:\n%s", got)
	}
}

func TestScannedCRDGenerateWholeTree(t *testing.T) {
	b, crds := crdSourceFixture(t)
	outputs, err := Generate(b, crds, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// a crds-only blueprint has no provider families, so no providerconfigs
	for _, o := range outputs {
		if strings.Contains(o.Path, "providerconfigs") {
			t.Errorf("unexpected providerconfig output %s", o.Path)
		}
	}
}
