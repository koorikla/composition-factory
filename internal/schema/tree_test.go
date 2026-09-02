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

// FieldTree is the one "settable fields" entry point: for a managed
// resource it must be exactly ForProvider (so nothing downstream changes
// behavior for providers), and for a native kind it is the object's own
// top-level properties minus the generator- and server-owned quartet.
func TestFieldTreeMatchesForProviderForManagedCRDs(t *testing.T) {
	c := parseOne(t, nestedCRD)
	fromFieldTree, err := c.FieldTree()
	if err != nil {
		t.Fatalf("FieldTree: %v", err)
	}
	fromForProvider, err := c.ForProvider()
	if err != nil {
		t.Fatalf("ForProvider: %v", err)
	}
	var a, b []string
	for _, l := range Leaves(fromFieldTree, "") {
		a = append(a, l.Path)
	}
	for _, l := range Leaves(fromForProvider, "") {
		b = append(b, l.Path)
	}
	if diff := cmp.Diff(b, a); diff != "" {
		t.Errorf("FieldTree and ForProvider disagree for a managed CRD (-forProvider +fieldTree):\n%s", diff)
	}
}

func TestFieldTreeOfNativeKindExcludesGeneratorOwnedKeys(t *testing.T) {
	c := CRD{
		Kind: "ConfigMap", Native: true,
		Versions: []Version{{Name: "v1", Served: true, Storage: true, Properties: map[string]any{
			"apiVersion": map[string]any{"type": "string"},
			"kind":       map[string]any{"type": "string"},
			"metadata": map[string]any{"type": "object", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"labels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			}},
			"status": map[string]any{"type": "object"},
			"data":   map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		}}},
	}
	nodes, err := c.FieldTree()
	if err != nil {
		t.Fatalf("FieldTree: %v", err)
	}
	leaves := Leaves(nodes, "")
	foundData, foundName, foundLabels := false, false, false
	for _, l := range leaves {
		if l.Path == "data" {
			foundData = true
		}
		if l.Path == "metadata.name" {
			foundName = true
		}
		if l.Path == "metadata.labels" {
			foundLabels = true
		}
	}
	if !foundData || !foundName || !foundLabels {
		t.Errorf("native FieldTree leaves = %+v, want data, metadata.name, metadata.labels", leaves)
	}
}

func TestEnvelopeOfNativeKindIsEmpty(t *testing.T) {
	c := CRD{Kind: "Deployment", Native: true,
		Versions: []Version{{Name: "v1", Served: true, Storage: true, Properties: map[string]any{
			"spec": map[string]any{"type": "object", "properties": map[string]any{"replicas": map[string]any{"type": "integer"}}},
		}}}}
	nodes, err := c.Envelope()
	if err != nil {
		t.Fatalf("Envelope: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("native Envelope = %v, want empty: the composed object has no Crossplane wrapper", nodes)
	}
}

// statusCRD carries a realistic upjet-shaped status (atProvider + machinery
// fields) so Status() can be proven against the shape the reference layer
// actually wires from. It deliberately also carries an array-of-objects
// (conditions) and a map (tags) so leaf classification is pinned for status
// trees the same way it is for forProvider trees.
var queueStatusCRD = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
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
                  url: {type: string, description: The queue URL.}
                  maxMessageSize: {type: integer}
                  tags:
                    type: object
                    additionalProperties: {type: string}
              conditions:
                type: array
                items:
                  properties:
                    type: {type: string}
                    status: {type: string}
              observedGeneration: {type: integer}
`)

func TestStatusBuildsTheStatusSubtree(t *testing.T) {
	c := parseOne(t, queueStatusCRD)
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	var got []string
	for _, l := range Leaves(st, "") {
		got = append(got, l.Path)
	}
	want := []string{
		"atProvider.maxMessageSize",
		"atProvider.tags",
		"atProvider.url",
		"conditions[0].status",
		"conditions[0].type",
		"observedGeneration",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status leaves mismatch (-want +got):\n%s", diff)
	}
}

// A CRD with no status schema at all (a native-envelope difference: nothing
// guarantees a status subtree exists) must return nil, not an error — the
// same contract ForProvider has for a missing forProvider. The caller (the
// emit layer) is the one that turns "no status schema" into a user-facing
// refusal, because only it knows a wire is being drawn from it.
func TestStatusIsNilWhenTheCRDHasNoStatusSchema(t *testing.T) {
	c := parseOne(t, nestedCRD) // nestedCRD declares no status property
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st != nil {
		t.Errorf("Status = %v, want nil for a CRD with no status schema", st)
	}
}

// Status must come from the preferred (storage) version, not versions[0] —
// the same rule Preferred() enforces everywhere else.
func TestStatusUsesThePreferredVersion(t *testing.T) {
	doc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: things.test.m.example.org}
spec:
  group: test.m.example.org
  scope: Namespaced
  names: {kind: Thing, plural: things, categories: [managed]}
  versions:
  - name: v1alpha1
    served: true
    storage: false
    schema:
      openAPIV3Schema:
        properties:
          status:
            properties:
              old: {type: string}
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          status:
            properties:
              current: {type: string}
`)
	c := parseOne(t, doc)
	st, err := c.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st) != 1 || st[0].Name != "current" {
		t.Errorf("Status = %+v, want exactly the storage version's [current]", st)
	}
}
