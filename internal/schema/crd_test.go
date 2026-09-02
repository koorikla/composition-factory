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
	apiVersion, err := c.APIVersion()
	if err != nil {
		t.Fatalf("APIVersion: %v", err)
	}
	if got, want := apiVersion, "test.m.example.org/v1beta2"; got != want {
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

// TestPreferred covers the consequential branches of Preferred(): the
// domain's most-cited bug is a legacy CRD serving two versions with
// inconsistent storage flags, so the fallback and failure paths need
// explicit coverage, not just the happy path exercised above.
func TestPreferred(t *testing.T) {
	tests := []struct {
		name     string
		crd      CRD
		wantName string
		wantErr  bool
	}{
		{
			name:    "no versions at all",
			crd:     CRD{Kind: "Widget", Plural: "widgets", Group: "test.example.org"},
			wantErr: true,
		},
		{
			name: "all versions unserved",
			crd: CRD{
				Kind: "Widget", Plural: "widgets", Group: "test.example.org",
				Versions: []Version{
					{Name: "v1beta1", Served: false, Storage: false, Deprecated: false},
					{Name: "v1beta2", Served: false, Storage: false, Deprecated: false},
				},
			},
			wantErr: true,
		},
		{
			name: "all versions deprecated",
			crd: CRD{
				Kind: "Widget", Plural: "widgets", Group: "test.example.org",
				Versions: []Version{
					{Name: "v1beta1", Served: true, Storage: false, Deprecated: true},
					{Name: "v1beta2", Served: true, Storage: false, Deprecated: true},
				},
			},
			wantErr: true,
		},
		{
			name: "two versions both storage:true takes the first in list order",
			crd: CRD{
				Kind: "Widget", Plural: "widgets", Group: "test.example.org",
				Versions: []Version{
					{Name: "v1beta1", Served: true, Storage: true, Deprecated: false},
					{Name: "v1beta2", Served: true, Storage: true, Deprecated: false},
				},
			},
			wantName: "v1beta1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := tt.crd.Preferred()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Preferred() = %+v, nil; want an error", v)
				}
				return
			}
			if err != nil {
				t.Fatalf("Preferred(): unexpected error: %v", err)
			}
			if v.Name != tt.wantName {
				t.Errorf("Preferred().Name = %q, want %q", v.Name, tt.wantName)
			}
		})
	}
}

// TestAPIVersionErrorsRatherThanReturningMalformedString guards against the
// exact defect class this project cares most about: a legal-looking string
// that is quietly wrong. A CRD with no usable version must not produce
// "group/" (an apiVersion with an empty version segment) -- it must return
// an error and an empty string.
func TestAPIVersionErrorsRatherThanReturningMalformedString(t *testing.T) {
	c := CRD{Kind: "Widget", Plural: "widgets", Group: "test.example.org"}
	got, err := c.APIVersion()
	if err == nil {
		t.Fatalf("APIVersion() = %q, nil; want an error for a CRD with no usable version", got)
	}
	if got != "" {
		t.Errorf("APIVersion() = %q, want empty string on error", got)
	}
}

// A native core-group kind (Group "") has the bare version as its
// apiVersion -- "v1", never the malformed "/v1" the naive concatenation
// would produce. Only internal/schema/k8s builds CRDs with an empty group;
// a parsed CustomResourceDefinition cannot have one.
func TestAPIVersionOfCoreGroupIsBareVersion(t *testing.T) {
	c := CRD{Kind: "ConfigMap", Plural: "configmaps", Native: true,
		Versions: []Version{{Name: "v1", Served: true, Storage: true}}}
	got, err := c.APIVersion()
	if err != nil {
		t.Fatalf("APIVersion: %v", err)
	}
	if got != "v1" {
		t.Errorf("APIVersion() = %q, want %q", got, "v1")
	}
}

func TestParseCRDManifestHandlesMultipleDocsAndBlockScalars(t *testing.T) {
	manifest := []byte(`
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: items.test.example.org
spec:
  group: test.example.org
  scope: Namespaced
  names:
    kind: Item
    plural: items
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        description: |
          A multi-line block scalar description
          ---
          containing document separator marker within
        properties:
          spec:
            properties:
              data: {type: string}
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.test.example.org
spec:
  group: test.example.org
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
  versions:
  - name: v1alpha1
    served: true
    storage: true
---
`)

	crds, err := ParseCRDManifest(manifest)
	if err != nil {
		t.Fatalf("ParseCRDManifest failed: %v", err)
	}
	if len(crds) != 2 {
		t.Fatalf("got %d CRDs, want 2", len(crds))
	}
	if !crds[0].Native || !crds[1].Native {
		t.Errorf("expected all parsed CRDs from manifest to have Native=true")
	}
}
