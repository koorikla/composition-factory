package schema

import "testing"

var twoVersionCRD = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.test.m.example.org
spec:
  group: test.m.example.org
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
    categories: [crossplane, managed]
  versions:
  - name: v1beta1
    served: true
    storage: false
    deprecated: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region: {type: string}
  - name: v1beta2
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                properties:
                  region: {type: string}
`)

var notManaged = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: providerconfigs.test.example.org
spec:
  group: test.example.org
  scope: Cluster
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [crossplane, providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)

func TestParseCRDsPicksStorageVersion(t *testing.T) {
	crds, err := ParseCRDs([][]byte{twoVersionCRD})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if len(crds) != 1 {
		t.Fatalf("got %d CRDs, want 1", len(crds))
	}
	c := crds[0]
	v, err := c.Preferred()
	if err != nil {
		t.Fatalf("Preferred: %v", err)
	}
	if v.Name != "v1beta2" {
		t.Errorf("Preferred = %q, want v1beta2 (the storage version, not versions[0])", v.Name)
	}
	if got, want := c.APIVersion(), "test.m.example.org/v1beta2"; got != want {
		t.Errorf("APIVersion = %q, want %q", got, want)
	}
	if !c.IsManaged() {
		t.Error("IsManaged = false, want true")
	}
	if !c.Namespaced() {
		t.Error("Namespaced = false, want true")
	}
}

func TestParseCRDsSkipsNonManaged(t *testing.T) {
	crds, err := ParseCRDs([][]byte{notManaged})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if crds[0].IsManaged() {
		t.Error("ProviderConfig reported as managed; it carries the providerconfig category")
	}
}

func TestParseCRDsIgnoresNonCRDDocuments(t *testing.T) {
	meta := []byte("apiVersion: meta.pkg.crossplane.io/v1\nkind: Provider\nmetadata: {name: p}\n")
	crds, err := ParseCRDs([][]byte{meta, twoVersionCRD})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	if len(crds) != 1 {
		t.Fatalf("got %d CRDs, want 1 (the meta document must be skipped)", len(crds))
	}
}
