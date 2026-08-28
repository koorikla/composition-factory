package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

func testCRDs(t *testing.T) []schema.CRD {
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
                  maxMessageSize: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
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
                properties: {region: {type: string}}
              deletionPolicy: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

func TestCompositionSelectsNamespacedVariant(t *testing.T) {
	got, err := Composition(testBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "apiVersion: sqs.aws.m.upbound.io/v1beta1") {
		t.Errorf("did not select the .m. namespaced variant\n---\n%s", s)
	}
	if strings.Contains(s, "apiVersion: sqs.aws.upbound.io/v1beta1") {
		t.Error("emitted the legacy cluster-scoped variant for a Namespaced XRD")
	}
}

// The single most important assertion in this package.
func TestOptionsIsTopLevelNotNestedUnderInline(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	lines := strings.Split(string(got), "\n")
	var optIndent, inlineIndent int = -1, -1
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		indent := len(l) - len(trimmed)
		if strings.HasPrefix(trimmed, "options:") && optIndent == -1 {
			optIndent = indent
		}
		if strings.HasPrefix(trimmed, "inline:") && inlineIndent == -1 {
			inlineIndent = indent
		}
	}
	if optIndent == -1 {
		t.Fatal("no options: key; missingkey=error must always be emitted")
	}
	if inlineIndent == -1 {
		t.Fatal("no inline: key")
	}
	if optIndent != inlineIndent {
		t.Errorf("options: is indented %d and inline: is %d — options must be a SIBLING of inline, "+
			"not nested inside it (nesting is a fatal error at runtime)", optIndent, inlineIndent)
	}
	if !strings.Contains(string(got), "missingkey=error") {
		t.Error("missingkey=error missing; without it a missing field renders the string <no value>")
	}
}

func TestOptionalFieldIsGuarded(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	s := string(got)
	if !strings.Contains(s, "{{- with $spec.maxMessageSize }}") {
		t.Errorf("optional field not wrapped in a with-guard\n---\n%s", s)
	}
}

func TestProviderConfigRefCarriesKindAndName(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	s := string(got)
	if !strings.Contains(s, "kind: ClusterProviderConfig") || !strings.Contains(s, "name: {{ $spec.providerName }}") {
		t.Errorf("providerConfigRef must carry both kind and name in the v2 namespaced envelope\n---\n%s", s)
	}
}

func TestNoDeletionPolicyForNamespacedMR(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	if strings.Contains(string(got), "deletionPolicy") {
		t.Error("deletionPolicy is absent from the v2 namespaced envelope and would be pruned")
	}
}

func TestResourceNameAnnotationPresent(t *testing.T) {
	got, _ := Composition(testBlueprint(), testCRDs(t))
	if !strings.Contains(string(got), `setResourceNameAnnotation "main-queue"`) {
		t.Error("every composed resource needs a stable composition-resource-name annotation")
	}
}

func TestUnknownKindIsAClearError(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources[0].Kind = "Nonexistent"
	_, err := Composition(b, testCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "Nonexistent") {
		t.Fatalf("err = %v, want an error naming the unknown kind", err)
	}
}
