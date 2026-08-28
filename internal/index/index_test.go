package index

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// crds returns one namespaced and one cluster-scoped Queue plus a ProviderConfig,
// mirroring what an upjet provider actually ships.
func crds(t *testing.T) map[string][]schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
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
                required: [region]
                properties:
                  region: {type: string}
                  tags: {type: object, additionalProperties: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: providerconfigs.aws.m.upbound.io}
spec:
  group: aws.m.upbound.io
  scope: Namespaced
  names: {kind: ProviderConfig, plural: providerconfigs, categories: [providerconfig]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)}
	parsed, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]schema.CRD{"ghcr.io/x/provider-aws-sqs:v2.7.0": parsed}
}

// collidingProviderRef and primaryProviderRef describe the two providers
// crdsWithProviderCollision uses: it adds a second provider that ships a
// Queue at the exact same apiVersion+kind ("sqs.aws.m.upbound.io/v1beta1/
// Queue") as the primary fixture's namespaced Queue, differing only in
// which provider it came from. "-alt" sorts before the primary ref's
// ":v2.7.0" (ASCII '-' < ':'), so Build's provider-sorted processing
// visits it first and the primary provider's entry — written second —
// wins the crds[] lookup used by Lookup. See
// TestLookupResolvesProviderCollisionDeterministically.
const (
	collidingProviderRef = "ghcr.io/x/provider-aws-sqs-alt:v1.0.0"
	primaryProviderRef   = "ghcr.io/x/provider-aws-sqs:v2.7.0"
	primaryPlural        = "queues"
)

// crdsWithProviderCollision extends crds with a second provider whose Queue
// collides with the first provider's namespaced Queue on both APIVersion
// and Kind — the only two fields All() sorts by. With a single provider (as
// in crds), that (APIVersion, Kind) sort alone happens to fully order the
// fixture, so a test built only on crds cannot tell whether Build's
// provider pre-sort or its Provider tie-break actually did anything: two
// entries that never collide can't expose a missing tie-break, and a single
// map key can't expose nondeterministic map iteration. This fixture makes
// both load-bearing: All()'s order now depends on the Provider tie-break to
// place the two colliding entries consistently, and Lookup's result for the
// collision now depends on providers being processed in a fixed order.
func crdsWithProviderCollision(t *testing.T) map[string][]schema.CRD {
	t.Helper()
	byProvider := crds(t)
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io.alt}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues-alt, categories: [managed]}
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
                required: [region]
                properties:
                  region: {type: string}
`)}
	parsed, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	byProvider[collidingProviderRef] = parsed
	return byProvider
}

func TestBuildIndexesOnlyManagedResources(t *testing.T) {
	i, err := Build(crds(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var kinds []string
	for _, k := range i.All() {
		kinds = append(kinds, k.APIVersion+"/"+k.Kind)
	}
	want := []string{
		"sqs.aws.m.upbound.io/v1beta1/Queue",
		"sqs.aws.upbound.io/v1beta1/Queue",
	}
	if diff := cmp.Diff(want, kinds); diff != "" {
		t.Errorf("indexed kinds (-want +got):\n%s\nProviderConfig is not a managed resource and must be excluded", diff)
	}
}

// TestAllIsSortedAndStable uses crdsWithProviderCollision, not crds, so that
// the guarantee it checks is actually load-bearing: see that fixture's doc
// comment. It checks two things across every rebuild: All()'s content and
// order (which depends on the Provider tie-break placing the two colliding
// Queue entries consistently), and Lookup's resolution of the collision
// itself (which depends on providers being visited in a fixed order, since
// the crds[] map is last-write-wins). Fix round 1 verified empirically
// (see task-1-report.md) that removing the provider pre-sort makes this
// test fail via the Lookup check specifically, not via the All() diff —
// the Provider tie-break alone already makes All()'s output order
// independent of provider-processing order once every tie is fully broken.
func TestAllIsSortedAndStable(t *testing.T) {
	i, _ := Build(crdsWithProviderCollision(t))
	a, b := i.All(), i.All()
	if diff := cmp.Diff(a, b); diff != "" {
		t.Errorf("two calls differ (-first +second):\n%s", diff)
	}
	wantPlural := primaryPlural
	for n := 0; n < 5; n++ {
		j, err := Build(crdsWithProviderCollision(t))
		if err != nil {
			t.Fatalf("rebuild %d: Build: %v", n, err)
		}
		if diff := cmp.Diff(a, j.All()); diff != "" {
			t.Fatalf("rebuild %d differs, index must be deterministic:\n%s", n, diff)
		}
		c, ok := j.Lookup("sqs.aws.m.upbound.io/v1beta1", "Queue")
		if !ok {
			t.Fatalf("rebuild %d: Lookup did not find the colliding Queue", n)
		}
		if c.Plural != wantPlural {
			t.Fatalf("rebuild %d: Lookup's collision winner changed (Plural=%q, want %q); "+
				"Build's provider processing order must be deterministic", n, c.Plural, wantPlural)
		}
	}
}

func TestFieldCountsComeFromForProvider(t *testing.T) {
	i, _ := Build(crds(t))
	for _, k := range i.All() {
		if k.Namespaced {
			if k.Fields != 2 || k.Required != 1 {
				t.Errorf("namespaced Queue: Fields=%d Required=%d, want 2 and 1 (region required, tags not)",
					k.Fields, k.Required)
			}
		}
	}
}

func TestSearchMatchesKindCaseInsensitivelyAndRespectsLimit(t *testing.T) {
	i, _ := Build(crds(t))
	if got := i.Search("queue", 10); len(got) != 2 {
		t.Errorf("Search(queue) = %d results, want 2", len(got))
	}
	if got := i.Search("QUEUE", 10); len(got) != 2 {
		t.Errorf("Search is not case-insensitive: got %d", len(got))
	}
	if got := i.Search("queue", 1); len(got) != 1 {
		t.Errorf("Search ignored limit: got %d, want 1", len(got))
	}
	if got := i.Search("nothing-matches-this", 10); len(got) != 0 {
		t.Errorf("Search(nonsense) = %d, want 0", len(got))
	}
}

func TestSearchAlsoMatchesGroup(t *testing.T) {
	i, _ := Build(crds(t))
	if got := i.Search("sqs.aws.m", 10); len(got) != 1 {
		t.Errorf("Search by group = %d results, want 1 (only the .m. variant)", len(got))
	}
}

func TestLookupFindsTheExactVariant(t *testing.T) {
	i, _ := Build(crds(t))
	c, ok := i.Lookup("sqs.aws.m.upbound.io/v1beta1", "Queue")
	if !ok {
		t.Fatal("Lookup did not find the namespaced Queue")
	}
	if !c.Namespaced() {
		t.Error("Lookup returned the cluster-scoped variant for a .m. apiVersion")
	}
	if _, ok := i.Lookup("sqs.aws.m.upbound.io/v1beta1", "Nonexistent"); ok {
		t.Error("Lookup reported success for a kind that does not exist")
	}
}

// TestLookupResolvesProviderCollisionDeterministically documents and pins
// the deliberate choice for the case flagged in fix round 1: two different
// providers shipping the exact same apiVersion+kind. Build processes
// providers in sorted order and crds[] is last-write-wins, so the
// lexicographically greatest provider ref (primaryProviderRef, which sorts
// after collidingProviderRef) must win.
func TestLookupResolvesProviderCollisionDeterministically(t *testing.T) {
	i, err := Build(crdsWithProviderCollision(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	c, ok := i.Lookup("sqs.aws.m.upbound.io/v1beta1", "Queue")
	if !ok {
		t.Fatal("Lookup did not find the colliding Queue")
	}
	if c.Plural != primaryPlural {
		t.Errorf("Lookup on collision returned Plural=%q, want %q (from %s, the lexicographically greatest provider ref)",
			c.Plural, primaryPlural, primaryProviderRef)
	}
}

// TestBuildErrorWrapsUnderlyingFailure covers fix round 1 finding 2: the
// total-failure error must wrap the last underlying Preferred()/APIVersion()
// error with %w, not just report a count, so it is actionable.
func TestBuildErrorWrapsUnderlyingFailure(t *testing.T) {
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: brokens.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Broken, plural: brokens, categories: [managed]}
  versions:
  - {name: v1beta1, served: false, storage: false}
`)}
	parsed, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(map[string][]schema.CRD{"ghcr.io/x/broken:v1.0.0": parsed})
	if err == nil {
		t.Fatal("Build did not error when every managed CRD failed to index")
	}
	const want = "no storage or served non-deprecated version"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Build error does not wrap the underlying Preferred() failure (want it to contain %q): %v", want, err)
	}
}
