package schema

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

var nestedCRD = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: things.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Thing, plural: things, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string, description: Region to use.}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  containers:
                    type: array
                    items:
                      properties:
                        image: {type: string}
                        ports:
                          type: array
                          items:
                            properties:
                              containerPort: {type: integer}
              managementPolicies:
                type: array
                items: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties:
                  kind: {type: string}
                  name: {type: string}
`)

func parseOne(t *testing.T, doc []byte) CRD {
	t.Helper()
	crds, err := ParseCRDs([][]byte{doc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	return crds[0]
}

func TestLeavesUsesArrayIndexedPaths(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, err := c.ForProvider()
	if err != nil {
		t.Fatalf("ForProvider: %v", err)
	}
	var got []string
	for _, l := range Leaves(fp, "") {
		got = append(got, l.Path)
	}
	want := []string{
		"containers[0].image",
		"containers[0].ports[0].containerPort",
		"region",
		"tags",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Leaves paths (-want +got):\n%s", diff)
	}
}

func TestMapIsALeafNotABranch(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, _ := c.ForProvider()
	for _, l := range Leaves(fp, "") {
		if l.Path == "tags" && l.Node.Type != "map" {
			t.Errorf("tags type = %q, want map (additionalProperties collapses to a leaf)", l.Node.Type)
		}
	}
}

func TestRequiredIsCarried(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, _ := c.ForProvider()
	for _, l := range Leaves(fp, "") {
		if l.Path == "region" && !l.Node.Required {
			t.Error("region.Required = false, want true")
		}
		if l.Path == "tags" && l.Node.Required {
			t.Error("tags.Required = true, want false")
		}
	}
}

func TestEnvelopeExcludesForProviderAndInitProvider(t *testing.T) {
	c := parseOne(t, nestedCRD)
	env, err := c.Envelope()
	if err != nil {
		t.Fatalf("Envelope: %v", err)
	}
	var got []string
	for _, n := range env {
		got = append(got, n.Name)
	}
	want := []string{"managementPolicies", "providerConfigRef"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Envelope (-want +got):\n%s", diff)
	}
}
