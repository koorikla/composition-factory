package schema

import (
	"strings"
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
                  endpoint:
                    properties:
                      url: {type: string}
                      port: {type: integer}
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
		"endpoint.port",
		"endpoint.url",
		"region",
		"tags",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Leaves paths (-want +got):\n%s", diff)
	}
}

// TestUntypedObjectWithPropertiesBecomesObjectNode pins buildNode's untyped
// fallback (the `default:` branch in tree.go). Real provider schemas
// frequently omit `type: object` on embedded objects; endpoint here has no
// `type:` key at all but does have `properties`, and is reached through the
// normal ForProvider() -> BuildTree -> buildNode path (not via items, and
// not via forProvider itself, both of which are read by direct field access
// and would bypass buildNode entirely).
func TestUntypedObjectWithPropertiesBecomesObjectNode(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fp, err := c.ForProvider()
	if err != nil {
		t.Fatalf("ForProvider: %v", err)
	}
	var endpoint *Node
	for _, n := range fp {
		if n.Name == "endpoint" {
			endpoint = n
		}
	}
	if endpoint == nil {
		t.Fatal("forProvider has no endpoint node")
	}
	if endpoint.Type != "object" {
		t.Errorf("endpoint.Type = %q, want %q (untyped-with-properties must default to object)", endpoint.Type, "object")
	}
	if len(endpoint.Children) == 0 {
		t.Error("endpoint.Children is empty, want url and port")
	}

	var got []string
	for _, l := range Leaves(fp, "") {
		if l.Path == "endpoint.url" || l.Path == "endpoint.port" {
			got = append(got, l.Path)
		}
	}
	want := []string{"endpoint.port", "endpoint.url"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("endpoint leaves (-want +got):\n%s", diff)
	}
}

// TestScalarArrayIsAWholeLeafNotIndexed pins the deliberate semantic that
// arrays of scalars (managementPolicies: type array, items: {type: string})
// are addressed as a single leaf with no [0] index -- the whole array is
// assigned as a value, unlike arrays of objects (e.g. containers) whose
// element fields are indexed. Task 9's Composition emitter depends on this
// distinction, so it must be pinned rather than left as incidental behavior.
func TestScalarArrayIsAWholeLeafNotIndexed(t *testing.T) {
	c := parseOne(t, nestedCRD)
	env, err := c.Envelope()
	if err != nil {
		t.Fatalf("Envelope: %v", err)
	}
	var matches []Leaf
	for _, l := range Leaves(env, "") {
		if l.Path == "managementPolicies" || strings.HasPrefix(l.Path, "managementPolicies[") {
			matches = append(matches, l)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("got %d leaves matching managementPolicies, want exactly 1: %+v", len(matches), matches)
	}
	if matches[0].Path != "managementPolicies" {
		t.Errorf("managementPolicies path = %q, want %q (scalar arrays are assigned whole, not indexed)", matches[0].Path, "managementPolicies")
	}
	if matches[0].Node.Type != "array" {
		t.Errorf("managementPolicies.Type = %q, want %q", matches[0].Node.Type, "array")
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

// statusCRD is nestedCRD's shape with a status subtree, the way every upjet
// CRD declares one: atProvider carrying the observed values, conditions an
// array of objects.
var statusCRD = []byte(`
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
            properties:
              forProvider:
                properties:
                  region: {type: string}
          status:
            properties:
              atProvider:
                properties:
                  id: {type: string}
                  url: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
              conditions:
                type: array
                items:
                  properties:
                    status: {type: string}
                    type: {type: string}
`)

// TestStatusExposesTheStatusSubtree pins Status() the way ForProvider() is
// pinned: it must walk openAPIV3Schema.properties.status with the same
// BuildTree the rest of the generator uses, so a cross-resource reference is
// validated against exactly the tree the provider declares.
func TestStatusExposesTheStatusSubtree(t *testing.T) {
	c := parseOne(t, statusCRD)
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	var got []string
	for _, l := range Leaves(st, "") {
		got = append(got, l.Path)
	}
	want := []string{
		"atProvider.id",
		"atProvider.tags",
		"atProvider.url",
		"conditions[0].status",
		"conditions[0].type",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Status leaves (-want +got):\n%s", diff)
	}
}

// A CRD with no status at all returns nil, nil — the caller owns turning
// that into an error naming the resource, not this package.
func TestStatusIsNilWhenTheCRDDeclaresNone(t *testing.T) {
	c := parseOne(t, nestedCRD)
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != nil {
		t.Errorf("Status = %v, want nil for a CRD with no status subtree", st)
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
