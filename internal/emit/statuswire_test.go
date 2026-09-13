// Tests for cross-resource status wires: `from:
// resources.<name>.status.<path>` rendered as a guarded dereference of
// $.observed.resources. Every render-semantics claim here is proven by
// executing the emitted template body with Go's real text/template under
// Option("missingkey=error") — the engine function-go-templating wraps —
// never by string-matching alone.
package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// wireCRDs is testCRDs plus a QueuePolicy and a status schema on the
// namespaced Queue, mirroring the real provider-aws-sqs shapes: a scalar
// url, a map (tags), an array of objects (conditions) and an untyped leaf,
// so every path-classification branch has a target.
func wireCRDs(t *testing.T) []schema.CRD {
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
          status:
            properties:
              atProvider:
                properties:
                  url: {type: string}
                  maxMessageSize: {type: integer}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  mystery: {}
              conditions:
                type: array
                items:
                  properties:
                    type: {type: string}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queuepolicies.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: QueuePolicy, plural: queuepolicies, categories: [managed]}
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
                  policy: {type: string}
                  queueUrl: {type: string}
                  maxMessageSize: {type: integer}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: nostatuses.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: NoStatus, plural: nostatuses, categories: [managed]}
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
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// wireBlueprint composes a Queue and a QueuePolicy whose queueUrl is wired
// from the Queue's observed status.
func wireBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.Resources = []blueprint.Resource{
		{
			Name: "main-queue", Kind: "Queue",
			Fields: map[string]blueprint.Field{
				"region": {Value: "eu-north-1"},
			},
		},
		{
			Name: "queue-policy", Kind: "QueuePolicy",
			Fields: map[string]blueprint.Field{
				"region":   {Value: "eu-north-1"},
				"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
			},
		},
	}
	return b
}

// observedQueue is a fully-observed main-queue entry for the template data,
// in the shape function-go-templating hands the template: observed.resources
// keyed by composition-resource-name, each entry carrying the object under
// .resource.
func observedQueue(url string) map[string]any {
	return map[string]any{
		"main-queue": map[string]any{
			"resource": map[string]any{
				"status": map[string]any{
					"atProvider": map[string]any{"url": url},
				},
			},
		},
	}
}

func renderedPolicyDoc(t *testing.T, rendered string) map[string]any {
	t.Helper()
	for _, docText := range strings.Split(rendered, "\n---\n") {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, docText)
		}
		if doc["kind"] == "QueuePolicy" {
			return doc
		}
	}
	t.Fatalf("no QueuePolicy document in the rendered output\n---\n%s", rendered)
	return nil
}

func policyForProvider(t *testing.T, rendered string) map[string]any {
	t.Helper()
	doc := renderedPolicyDoc(t, rendered)
	spec, _ := doc["spec"].(map[string]any)
	fp, ok := spec["forProvider"].(map[string]any)
	if !ok {
		t.Fatalf("QueuePolicy spec.forProvider = %T (%v), want a map\n---\n%s",
			spec["forProvider"], spec["forProvider"], rendered)
	}
	return fp
}

// The value flows once the source resource is observed.
func TestStatusWireRendersTheObservedValue(t *testing.T) {
	got, err := Composition(wireBlueprint(), wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "localstack"},
		observedQueue("https://sqs.eu-north-1.amazonaws.com/1/demo"))
	if err != nil {
		t.Fatalf("render with an observed source: %v", err)
	}
	fp := policyForProvider(t, rendered)
	if fp["queueUrl"] != "https://sqs.eu-north-1.amazonaws.com/1/demo" {
		t.Errorf("queueUrl = %v, want the observed URL\n---\n%s", fp["queueUrl"], rendered)
	}
}

func hasDocKind(t *testing.T, rendered, kind string) bool {
	t.Helper()
	for _, docText := range strings.Split(rendered, "\n---\n") {
		trimmed := strings.TrimSpace(docText)
		if trimmed == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, docText)
		}
		if doc["kind"] == kind {
			return true
		}
	}
	return false
}

func hasDocResourceName(t *testing.T, rendered, resourceName string) bool {
	t.Helper()
	for _, docText := range strings.Split(rendered, "\n---\n") {
		trimmed := strings.TrimSpace(docText)
		if trimmed == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, docText)
		}
		meta, _ := doc["metadata"].(map[string]any)
		anns, _ := meta["annotations"].(map[string]any)
		if anns["crossplane.io/composition-resource-name"] == resourceName {
			return true
		}
	}
	return false
}

// CF-482: Composed resources whose annotation is wired to another resource's status
// must be gated entirely until prerequisite status fields exist in observed state.
func TestStatusWireInAnnotationGatesDependentResource(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields = map[string]blueprint.Field{
		"region":   {Value: "eu-north-1"},
		"queueUrl": {Value: "https://static"},
	}
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"crossplane.io/external-name": {From: "resources.main-queue.status.atProvider.url"},
	}

	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	// When source is unobserved: QueuePolicy must NOT be rendered at all.
	rendered, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"}, nil)
	if err != nil {
		t.Fatalf("render must succeed when source is unobserved, got: %v", err)
	}
	if hasDocKind(t, rendered, "QueuePolicy") {
		t.Fatalf("QueuePolicy must NOT be rendered while main-queue status is unobserved; got rendered output:\n%s", rendered)
	}
	if !hasDocKind(t, rendered, "Queue") {
		t.Fatalf("Queue must be rendered while unobserved; got rendered output:\n%s", rendered)
	}

	// When source is observed: QueuePolicy must be rendered and have the annotation.
	renderedObs, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"},
		observedQueue("https://sqs.eu-north-1.amazonaws.com/1/demo"))
	if err != nil {
		t.Fatalf("render with observed source: %v", err)
	}
	if !hasDocKind(t, renderedObs, "QueuePolicy") {
		t.Fatalf("QueuePolicy must be rendered when status is observed; got:\n%s", renderedObs)
	}
	doc := renderedPolicyDoc(t, renderedObs)
	meta, _ := doc["metadata"].(map[string]any)
	anns, _ := meta["annotations"].(map[string]any)
	if anns["crossplane.io/external-name"] != "https://sqs.eu-north-1.amazonaws.com/1/demo" {
		t.Errorf("crossplane.io/external-name = %v, want observed url", anns["crossplane.io/external-name"])
	}
}

// CF-482: Literal repro — two Queue resources, the second annotated with the
// first's observed status. The second resource must be gated entirely until observed.
func TestStatusWireInAnnotationLiteralRepro(t *testing.T) {
	b := testBlueprint()
	b.Spec.Resources = []blueprint.Resource{
		{
			Name: "main-queue", Kind: "Queue",
			Fields: map[string]blueprint.Field{
				"region": {Value: "eu-north-1"},
			},
		},
		{
			Name: "follower-queue", Kind: "Queue",
			Annotations: map[string]blueprint.Field{
				"crossplane.io/external-name": {From: "resources.main-queue.status.atProvider.url"},
			},
			Fields: map[string]blueprint.Field{
				"region": {Value: "eu-north-1"},
			},
		},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	// When unobserved: main-queue rendered, follower-queue must NOT be rendered.
	rendered, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"}, nil)
	if err != nil {
		t.Fatalf("render must succeed when source is unobserved, got: %v", err)
	}
	if !hasDocResourceName(t, rendered, "main-queue") {
		t.Fatalf("main-queue must be rendered while unobserved; got:\n%s", rendered)
	}
	if hasDocResourceName(t, rendered, "follower-queue") {
		t.Fatalf("follower-queue must NOT be rendered while main-queue status is unobserved; got rendered output:\n%s", rendered)
	}

	// When observed: both rendered.
	renderedObs, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"},
		observedQueue("https://sqs.eu-north-1.amazonaws.com/1/demo"))
	if err != nil {
		t.Fatalf("render with observed source: %v", err)
	}
	if !hasDocResourceName(t, renderedObs, "main-queue") {
		t.Fatalf("main-queue must be rendered when observed")
	}
	if !hasDocResourceName(t, renderedObs, "follower-queue") {
		t.Fatalf("follower-queue must be rendered when observed")
	}
	for _, docText := range strings.Split(renderedObs, "\n---\n") {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
			continue
		}
		meta, _ := doc["metadata"].(map[string]any)
		anns, _ := meta["annotations"].(map[string]any)
		if anns["crossplane.io/composition-resource-name"] == "follower-queue" {
			if anns["crossplane.io/external-name"] != "https://sqs.eu-north-1.amazonaws.com/1/demo" {
				t.Errorf("follower-queue crossplane.io/external-name = %v, want observed url", anns["crossplane.io/external-name"])
			}
		}
	}
}

// CF-482: A resource with status wires in both an annotation and a field:
// must not render until BOTH status dependencies are observed.
func TestStatusWireInAnnotationAndField(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider.url",
	}
	b.Spec.Resources[1].Annotations = map[string]blueprint.Field{
		"crossplane.io/external-name": {From: "resources.main-queue.status.atProvider.maxMessageSize"},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("neither observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, nil)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when neither status field is observed\n---\n%s", rendered)
		}
	})

	t.Run("only url observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"url": "https://q",
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when only url is observed\n---\n%s", rendered)
		}
	})

	t.Run("only maxMessageSize observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"maxMessageSize": 2048,
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when only maxMessageSize is observed\n---\n%s", rendered)
		}
	})

	t.Run("both observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"url": "https://q", "maxMessageSize": 2048,
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !hasDocKind(t, rendered, "QueuePolicy") {
			t.Fatalf("QueuePolicy must render when both status fields are observed\n---\n%s", rendered)
		}
		doc := renderedPolicyDoc(t, rendered)
		meta, _ := doc["metadata"].(map[string]any)
		anns, _ := meta["annotations"].(map[string]any)
		if anns["crossplane.io/external-name"] != "2048" {
			t.Errorf("crossplane.io/external-name = %v, want 2048", anns["crossplane.io/external-name"])
		}
	})
}

// CF-481: Composed resources referencing status fields of another resource
// must be gated entirely until prerequisite status fields exist in observed state.
func TestStatusWireGatesDependentResourceWhenUnobserved(t *testing.T) {
	got, err := Composition(wireBlueprint(), wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	rendered, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"}, nil)
	if err != nil {
		t.Fatalf("render must succeed when source is unobserved, got: %v", err)
	}
	if hasDocKind(t, rendered, "QueuePolicy") {
		t.Fatalf("QueuePolicy must NOT be rendered while main-queue status is unobserved; got rendered output:\n%s", rendered)
	}
	if !hasDocKind(t, rendered, "Queue") {
		t.Fatalf("Queue must be rendered while unobserved; got rendered output:\n%s", rendered)
	}
}

// CF-481: The unobserved cases: the render must SUCCEED, the dependent resource
// must be cleanly gated (not rendered at all), and the source resource must appear.
// Never "<no value>", never a hard render failure.
func TestStatusWireOmitsTheFieldWhenUnobserved(t *testing.T) {
	got, err := Composition(wireBlueprint(), wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	cases := []struct {
		name     string
		observed map[string]any // nil means no observed.resources key at all
	}{
		{"no observed.resources key at all", nil},
		{"source resource not observed", map[string]any{}},
		{"entry without a resource key", map[string]any{
			"main-queue": map[string]any{}}},
		{"resource without status", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{}}}},
		{"status explicitly null", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{"status": nil}}}},
		{"status is not a map", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{"status": "weird"}}}},
		{"atProvider absent", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{}}}}},
		{"atProvider null", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": nil}}}}},
		{"atProvider is not a map", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": "weird"}}}}},
		{"url absent", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderTemplateObserved(t, tmplBody,
				map[string]any{"providerName": "localstack"}, tc.observed)
			if err != nil {
				t.Fatalf("render must succeed when the source is unobserved, got: %v", err)
			}
			if hasDocKind(t, rendered, "QueuePolicy") {
				t.Errorf("QueuePolicy must be omitted while unobserved\n---\n%s", rendered)
			}
			if !hasDocKind(t, rendered, "Queue") {
				t.Errorf("Queue must be rendered while unobserved\n---\n%s", rendered)
			}
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(rendered, bad) {
					t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
				}
			}
		})
	}
}

// CF-481: A resource whose ONLY field is a status wire: while unobserved it
// must not render at all (avoiding empty objects dispatched to controllers),
// and once observed it renders with its fields.
func TestForProviderIsEmptyMapWhenOnlyStatusWireIsUnobserved(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields = map[string]blueprint.Field{
		"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	rendered, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"}, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if hasDocKind(t, rendered, "QueuePolicy") {
		t.Fatalf("QueuePolicy must not render while its only status wire is unobserved\n---\n%s", rendered)
	}

	// And once observed, the same template renders the value.
	rendered, err = renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "localstack"}, observedQueue("https://q"))
	if err != nil {
		t.Fatalf("render observed: %v", err)
	}
	if fp := policyForProvider(t, rendered); fp["queueUrl"] != "https://q" {
		t.Errorf("queueUrl = %v, want https://q\n---\n%s", fp["queueUrl"], rendered)
	}
}

func TestStatusWireMultipleDependencies(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields["maxMessageSize"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider.maxMessageSize",
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("neither observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"}, nil)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when neither status field is observed\n---\n%s", rendered)
		}
	})

	t.Run("only url observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"url": "https://q",
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when only url is observed\n---\n%s", rendered)
		}
	})

	t.Run("only maxMessageSize observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"maxMessageSize": 2048,
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when only maxMessageSize is observed\n---\n%s", rendered)
		}
	})

	t.Run("both observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack"},
			map[string]any{"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{
					"url": "https://q", "maxMessageSize": 2048,
				}}}}})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !hasDocKind(t, rendered, "QueuePolicy") {
			t.Fatalf("QueuePolicy must render when both status fields are observed\n---\n%s", rendered)
		}
		fp := policyForProvider(t, rendered)
		if fp["queueUrl"] != "https://q" {
			t.Errorf("queueUrl = %v, want https://q", fp["queueUrl"])
		}
		if fp["maxMessageSize"] != float64(2048) {
			t.Errorf("maxMessageSize = %v, want 2048", fp["maxMessageSize"])
		}
	})
}

func TestStatusWireWithWhenCondition(t *testing.T) {
	b := wireBlueprint()
	b.Spec.XRD.Parameters["enablePolicy"] = blueprint.Parameter{
		Type: "boolean", Required: true,
	}
	b.Spec.Resources[1].When = "params.enablePolicy"

	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("when false and observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack", "enablePolicy": false},
			observedQueue("https://q"))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when when: condition is false")
		}
	})

	t.Run("when true and unobserved", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack", "enablePolicy": true},
			nil)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must not render when status is unobserved")
		}
	})

	t.Run("when true and observed", func(t *testing.T) {
		rendered, err := renderTemplateObserved(t, tmplBody,
			map[string]any{"providerName": "localstack", "enablePolicy": true},
			observedQueue("https://q"))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !hasDocKind(t, rendered, "QueuePolicy") {
			t.Errorf("QueuePolicy must render when when: is true and status is observed")
		}
	})
}

// The guard idiom is hasKey/kindIs, never `with` (spec §8: {{- with }} is
// incompatible with missingkey=error — do not reinstate).
func TestStatusWireNeverUsesWithGuards(t *testing.T) {
	got, err := Composition(wireBlueprint(), wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	if strings.Contains(string(got), "{{- with") {
		t.Error("emitted template uses a with-guard, which hard-fails under missingkey=error")
	}
	if !strings.Contains(string(got), `hasKey (dig "resources"`) {
		t.Errorf("status wire must use Sprig dig/hasKey guard — got:\n---\n%s", got)
	}
}

// --- generation-time refusals ---

func TestStatusWireUnknownPathIsRejectedWithASuggestion(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider.ur", // one character short
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a status path absent from the source CRD's status schema")
	}
	if !strings.Contains(err.Error(), "atProvider.ur") {
		t.Errorf("err = %v, want it to name the offending path", err)
	}
	if !strings.Contains(err.Error(), `"atProvider.url"`) {
		t.Errorf("err = %v, want it to suggest the closest valid path", err)
	}
}

func TestStatusWireBranchPathIsRejected(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider",
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "atProvider") {
		t.Fatalf("err = %v, want a refusal naming the branch path: interpolating an object "+
			"renders Go's fmt of the map", err)
	}
}

// A map-typed leaf (additionalProperties) and an untyped leaf are both
// refused: the first renders Go's fmt of the map, the second cannot be
// proven scalar.
func TestStatusWireNonScalarLeafIsRejected(t *testing.T) {
	for _, path := range []string{
		"resources.main-queue.status.atProvider.tags",
		"resources.main-queue.status.atProvider.mystery",
	} {
		t.Run(path, func(t *testing.T) {
			b := wireBlueprint()
			b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{From: path}
			if _, err := Composition(b, wireCRDs(t)); err == nil {
				t.Fatalf("Composition accepted a non-scalar status leaf behind %q", path)
			}
		})
	}
}

func TestStatusWireFromCRDWithoutStatusSchemaIsRejected(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[0] = blueprint.Resource{
		Name: "main-queue", Kind: "NoStatus",
		Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("err = %v, want a refusal naming the missing status schema", err)
	}
}

// An integer status leaf is a legal wire source — the scalar rule admits
// every scalar type, not just strings. When wired into a string target field,
// it renders quoted as a string; when wired into an integer target field, it
// renders as a bare scalar.
func TestStatusWireIntegerLeafIsAccepted(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Resources[1].Fields["policy"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider.maxMessageSize",
	}
	b.Spec.Resources[1].Fields["maxMessageSize"] = blueprint.Field{
		From: "resources.main-queue.status.atProvider.maxMessageSize",
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "localstack"},
		map[string]any{"main-queue": map[string]any{"resource": map[string]any{
			"status": map[string]any{"atProvider": map[string]any{
				"url": "https://q", "maxMessageSize": 2048,
			}}}}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	fp := policyForProvider(t, rendered)
	if fp["policy"] != "2048" {
		t.Errorf("policy = %v (%T), want the observed integer quoted as string \"2048\"", fp["policy"], fp["policy"])
	}
	if fp["maxMessageSize"] != float64(2048) {
		t.Errorf("maxMessageSize = %v (%T), want the observed integer 2048", fp["maxMessageSize"], fp["maxMessageSize"])
	}
}

func renderedCompositeDoc(t *testing.T, rendered string, kind string) map[string]any {
	t.Helper()
	for _, docText := range strings.Split(rendered, "\n---\n") {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(docText), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, docText)
		}
		if doc["kind"] == kind {
			return doc
		}
	}
	t.Fatalf("no %s document in the rendered output\n---\n%s", kind, rendered)
	return nil
}

func TestStatusWireXRStatusRendersObservedValue(t *testing.T) {
	b := wireBlueprint()
	b.Spec.XRD.Status = map[string]blueprint.Parameter{
		"url": {
			Type: "string",
			From: "resources.main-queue.status.atProvider.url",
		},
		"maxMessageSize": {
			Type: "integer",
			From: "resources.main-queue.status.atProvider.maxMessageSize",
		},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "localstack"},
		observedQueue("https://sqs.eu-north-1.amazonaws.com/1/demo"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := renderedCompositeDoc(t, rendered, "XQueue")
	st, ok := doc["status"].(map[string]any)
	if !ok {
		t.Fatalf("doc[status] is not a map: %v", doc["status"])
	}
	if st["url"] != "https://sqs.eu-north-1.amazonaws.com/1/demo" {
		t.Errorf("status.url = %v, want https://sqs.eu-north-1.amazonaws.com/1/demo", st["url"])
	}
}

func TestStatusWireXRStatusOmitsWhenUnobserved(t *testing.T) {
	b := wireBlueprint()
	b.Spec.XRD.Status = map[string]blueprint.Parameter{
		"url": {
			Type: "string",
			From: "resources.main-queue.status.atProvider.url",
		},
	}
	got, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "localstack"},
		nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	doc := renderedCompositeDoc(t, rendered, "XQueue")
	st, ok := doc["status"].(map[string]any)
	if !ok {
		t.Fatalf("doc[status] is not a map: %v", doc["status"])
	}
	if _, present := st["url"]; present {
		t.Errorf("status.url must be absent when unobserved, got %v", st["url"])
	}
}

func TestStatusWireXRStatusUnknownPathRejectedWithSuggestion(t *testing.T) {
	b := wireBlueprint()
	b.Spec.XRD.Status = map[string]blueprint.Parameter{
		"url": {
			Type: "string",
			From: "resources.main-queue.status.atProvider.ur",
		},
	}
	_, err := Composition(b, wireCRDs(t))
	if err == nil {
		t.Fatal("expected error for unknown status path, got nil")
	}
	if !strings.Contains(err.Error(), "atProvider.ur") || !strings.Contains(err.Error(), "atProvider.url") {
		t.Errorf("err = %v, want suggestion for atProvider.url", err)
	}
}
