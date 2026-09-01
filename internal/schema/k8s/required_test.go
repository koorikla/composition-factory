package k8s

import (
	"encoding/json"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// These tests pin the required flag to the VENDORED data itself, not to
// hand-written expectations that could drift from it: each one decodes the
// embedded vendored file's own required array for an object and asserts the
// built tree agrees with it exactly, in both directions — every listed name
// carries Required, every unlisted sibling does not, and an object whose
// vendored schema has NO required array (absent array = nothing required at
// that level) yields no Required child at all. An inversion or a
// default-to-required in the resolver cannot pass any of them.

// vendoredRequired decodes the named schema straight out of the embedded
// vendored file — bypassing the resolver under test — and returns its
// required array as a set (empty when the array is absent).
func vendoredRequired(t *testing.T, file, schemaName string) map[string]bool {
	t.Helper()
	raw, err := vendored.ReadFile(file)
	if err != nil {
		t.Fatalf("read vendored %s: %v", file, err)
	}
	var f vendoredFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse vendored %s: %v", file, err)
	}
	node, ok := f.Schemas[schemaName]
	if !ok {
		t.Fatalf("vendored %s has no schema %q", file, schemaName)
	}
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(node, &decoded); err != nil {
		t.Fatalf("decode %s: %v", schemaName, err)
	}
	out := make(map[string]bool, len(decoded.Required))
	for _, r := range decoded.Required {
		out[r] = true
	}
	return out
}

// childAt walks the Deployment field tree by node names (array indices are
// not part of a Node's name) and returns the children of the node at path.
func childAt(t *testing.T, nodes []*schema.Node, path ...string) []*schema.Node {
	t.Helper()
	for _, name := range path {
		var next *schema.Node
		for _, n := range nodes {
			if n.Name == name {
				next = n
				break
			}
		}
		if next == nil {
			t.Fatalf("no node %q along %v", name, path)
		}
		nodes = next.Children
	}
	return nodes
}

// requireExactly asserts that within children, exactly the names in want
// carry Required — both directions, so an inversion (every unlisted sibling
// suddenly required) and a dropped array (a listed name coming through
// unrequired) each fail with the field named.
func requireExactly(t *testing.T, children []*schema.Node, want map[string]bool, where string) {
	t.Helper()
	if len(children) == 0 {
		t.Fatalf("%s has no children to check", where)
	}
	for _, c := range children {
		if c.Required && !want[c.Name] {
			t.Errorf("%s.%s is Required but the vendored schema does not list it", where, c.Name)
		}
		if !c.Required && want[c.Name] {
			t.Errorf("%s.%s lost the Required the vendored schema declares", where, c.Name)
		}
	}
}

// The parent objects the palette shows first, checked against the arrays the
// vendored files actually carry. DeploymentSpec requires selector and
// template (and nothing else); Container requires name (and NOT image);
// ContainerPort requires containerPort; DeploymentStrategy declares no
// required array, so none of its fields may come through required; and the
// Deployment object's own top level has no required array either, so spec —
// and everything beside it — must be optional at depth zero.
func TestRequiredMatchesTheVendoredArraysExactly(t *testing.T) {
	tree, err := kindByName(t, "Deployment").FieldTree()
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		schemaName string
		path       []string
	}{
		{"io.k8s.api.apps.v1.Deployment", nil},
		{"io.k8s.api.apps.v1.DeploymentSpec", []string{"spec"}},
		{"io.k8s.api.apps.v1.DeploymentStrategy", []string{"spec", "strategy"}},
		{"io.k8s.api.core.v1.PodSpec", []string{"spec", "template", "spec"}},
		{"io.k8s.api.core.v1.Container", []string{"spec", "template", "spec", "containers"}},
		{"io.k8s.api.core.v1.ContainerPort", []string{"spec", "template", "spec", "containers", "ports"}},
	}
	for _, c := range checks {
		want := vendoredRequired(t, "openapi_apps_v1.json", c.schemaName)
		where := "Deployment"
		for _, p := range c.path {
			where += "." + p
		}
		requireExactly(t, childAt(t, tree, c.path...), want, where)
	}

	// The two acceptance examples, spelled out so a regeneration that
	// changes them is noticed as the upstream change it would be — these are
	// what the vendored v1.34.1 arrays say, cross-checked above.
	spec := childAt(t, tree, "spec")
	required := map[string]bool{}
	for _, c := range spec {
		if c.Required {
			required[c.Name] = true
		}
	}
	if len(required) != 2 || !required["selector"] || !required["template"] {
		t.Errorf("Deployment spec-level required = %v, want exactly {selector, template}", required)
	}
	containers := childAt(t, tree, "spec", "template", "spec", "containers")
	for _, c := range containers {
		switch c.Name {
		case "name":
			if !c.Required {
				t.Error("containers[i].name must be required (Container.required lists it)")
			}
		case "image":
			if c.Required {
				t.Error("containers[i].image must NOT be required — the schema does not require it")
			}
		}
	}
}

// Requiredness must stay a minority property of the tree. The faithful
// figure for the vendored v1.34.1 Deployment is 250 of 842 leaves (~30%):
// Kubernetes marks members of many deeply nested objects required WITHIN
// those objects (EnvVar.name, GRPCAction.port, KeyToPath.key/path, ...), and
// the pod template triples them across containers/initContainers/
// ephemeralContainers. An inverted flag would flip that to ~70%, and
// default-to-required to 100%, so the guard sits at one half: loose enough
// that a regeneration shifting the exact counts cannot trip it spuriously,
// tight enough that any inversion fails loudly. The all-optional failure
// mode (required arrays dropped wholesale) is caught by the zero check —
// and, field by field, by TestRequiredMatchesTheVendoredArraysExactly.
func TestDeploymentRequiredLeavesStayAMinority(t *testing.T) {
	tree, err := kindByName(t, "Deployment").FieldTree()
	if err != nil {
		t.Fatal(err)
	}
	leaves := schema.Leaves(tree, "")
	if len(leaves) == 0 {
		t.Fatal("Deployment has no leaves")
	}
	required := 0
	for _, l := range leaves {
		if l.Node.Required {
			required++
		}
	}
	if required == 0 {
		t.Error("no required leaves at all — the vendored required arrays are being dropped")
	}
	if required*2 >= len(leaves) {
		t.Errorf("%d of %d leaves are required (>= half) — the required flag looks inverted or defaulted",
			required, len(leaves))
	}
}
