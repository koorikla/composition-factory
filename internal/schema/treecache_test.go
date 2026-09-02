package schema

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// gvkCRD renders a managed CRD with a fixed GVK (things.test.example.org,
// v1beta1) whose forProvider and status property lists are the caller's, so
// two CRDs can share an identity and differ only in schema.
func gvkCRD(forProvider, status string) []byte {
	return []byte(fmt.Sprintf(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: things.test.example.org}
spec:
  group: test.example.org
  scope: Cluster
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
%s
              managementPolicies:
                type: array
                items: {type: string}
          status:
            properties:
%s
`, indent(forProvider, 18), indent(status, 14)))
}

func indent(block string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimSpace(block), "\n")
	for i, l := range lines {
		lines[i] = pad + strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

// sameTree reports whether two trees are the same object graph: same
// length and the same *Node at every top-level position. Equal content in
// distinct allocations is deliberately NOT "same" — the memo's whole point
// is that a second call hands back the first call's nodes.
func sameTree(a, b []*Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The four tree methods are memoised per parsed CRD: a second call — on the
// same value or on a copy of it, since CRD travels by value through the
// index, the API and the emitter — returns the very same node graph, not a
// fresh rebuild from the raw map.
func TestTreeMethodsReturnTheSameTreeOnRepeatedCalls(t *testing.T) {
	c := parseOne(t, gvkCRD("region: {type: string}", "url: {type: string}"))
	copyOfC := c

	for _, m := range []struct {
		name string
		fn   func(CRD) ([]*Node, error)
	}{
		{"ForProvider", CRD.ForProvider},
		{"FieldTree", CRD.FieldTree},
		{"Status", CRD.Status},
		{"Envelope", CRD.Envelope},
	} {
		first, err := m.fn(c)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if len(first) == 0 {
			t.Fatalf("%s: empty tree; the fixture should give it something to memoise", m.name)
		}
		second, _ := m.fn(c)
		if !sameTree(first, second) {
			t.Errorf("%s: second call rebuilt the tree instead of returning the memoised one", m.name)
		}
		viaCopy, _ := m.fn(copyOfC)
		if !sameTree(first, viaCopy) {
			t.Errorf("%s: a by-value copy of the CRD did not share the memoised tree", m.name)
		}
	}
}

// The memo key is the parsed schema, never the GVK: two CRDs that share
// apiVersion+kind but carry different schemas (a cluster-scoped and a
// namespaced variant, two provider versions, two providers colliding) must
// each get their own tree.
func TestTreeMemoKeysOnTheSchemaNotTheGVK(t *testing.T) {
	a := parseOne(t, gvkCRD("region: {type: string}", "url: {type: string}"))
	b := parseOne(t, gvkCRD("zone: {type: string}", "arn: {type: string}"))
	if av, _ := a.APIVersion(); av != mustAPIVersion(t, b) || a.Kind != b.Kind {
		t.Fatal("fixture: the two CRDs should share a GVK")
	}

	fa, _ := a.ForProvider()
	fb, _ := b.ForProvider()
	if sameTree(fa, fb) {
		t.Fatal("ForProvider: two different schemas under one GVK shared a memoised tree")
	}
	if fa[0].Name != "region" || fb[0].Name != "zone" {
		t.Errorf("ForProvider trees crossed: a=%q b=%q", fa[0].Name, fb[0].Name)
	}
	sa, _ := a.Status()
	sb, _ := b.Status()
	if sa[0].Name != "url" || sb[0].Name != "arn" {
		t.Errorf("Status trees crossed: a=%q b=%q", sa[0].Name, sb[0].Name)
	}

	// Re-parsing the same document is a new schema instance: it gets its own
	// (content-equal) tree rather than aliasing the first parse's memo, so a
	// re-read provider cache can never serve nodes pinned by a stale parse.
	a2 := parseOne(t, gvkCRD("region: {type: string}", "url: {type: string}"))
	fa2, _ := a2.ForProvider()
	if sameTree(fa, fa2) {
		t.Error("a fresh parse aliased the memo of an unrelated parse")
	}
	if diff := cmp.Diff(fa, fa2); diff != "" {
		t.Errorf("fresh parse built a different tree (-first +fresh):\n%s", diff)
	}
}

func mustAPIVersion(t *testing.T, c CRD) string {
	t.Helper()
	av, err := c.APIVersion()
	if err != nil {
		t.Fatal(err)
	}
	return av
}

// Native flips what FieldTree and Envelope mean (the object's own tree; no
// envelope at all), and ParseCRDManifest sets it after parsing. The memo
// must not hand a managed-shaped tree to the native view of the same
// schema or vice versa.
func TestTreeMemoSeparatesNativeFromManagedViews(t *testing.T) {
	managed := parseOne(t, gvkCRD("region: {type: string}", "url: {type: string}"))
	managedTree, _ := managed.FieldTree()
	managedEnv, _ := managed.Envelope()

	native := managed
	native.Native = true
	nativeTree, _ := native.FieldTree()
	nativeEnv, _ := native.Envelope()

	if sameTree(managedTree, nativeTree) || nativeTree[0].Name != "spec" || managedTree[0].Name != "region" {
		t.Errorf("FieldTree: managed=%q native=%q, want region vs spec", managedTree[0].Name, nativeTree[0].Name)
	}
	if len(managedEnv) == 0 || nativeEnv != nil {
		t.Errorf("Envelope: managed=%d nodes native=%v, want non-empty vs nil", len(managedEnv), nativeEnv)
	}
}

// The API server is concurrent: many handlers can ask one CRD for its trees
// at once. Under -race this proves the memo is safe for parallel readers
// and that every reader ends up on the same node graph.
func TestTreeMemoIsSafeForConcurrentReaders(t *testing.T) {
	c := parseOne(t, gvkCRD("region: {type: string}\nname: {type: string}", "url: {type: string}"))

	const readers = 32
	results := make([][4][]*Node, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				fp, _ := c.ForProvider()
				ft, _ := c.FieldTree()
				st, _ := c.Status()
				en, _ := c.Envelope()
				results[i] = [4][]*Node{fp, ft, st, en}
			}
		}()
	}
	wg.Wait()

	for i := 1; i < readers; i++ {
		for k, name := range []string{"ForProvider", "FieldTree", "Status", "Envelope"} {
			if !sameTree(results[0][k], results[i][k]) {
				t.Errorf("%s: reader %d saw a different node graph than reader 0", name, i)
			}
		}
	}
}

// BenchmarkStatusTree is the per-call cost the emitter pays inside its
// status-wire loops: one Status() on a real-shaped CRD.
func BenchmarkStatusTree(b *testing.B) {
	crds, err := ParseCRDs([][]byte{queueStatusCRD})
	if err != nil {
		b.Fatal(err)
	}
	c := crds[0]
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Status(); err != nil {
			b.Fatal(err)
		}
	}
}
