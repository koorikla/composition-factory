package index

import (
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

func TestAllIsSortedAndStable(t *testing.T) {
	i, _ := Build(crds(t))
	a, b := i.All(), i.All()
	if diff := cmp.Diff(a, b); diff != "" {
		t.Errorf("two calls differ (-first +second):\n%s", diff)
	}
	for n := 0; n < 5; n++ {
		j, _ := Build(crds(t))
		if diff := cmp.Diff(a, j.All()); diff != "" {
			t.Fatalf("rebuild %d differs, index must be deterministic:\n%s", n, diff)
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
