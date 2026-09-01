package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"sigs.k8s.io/yaml"
)

// statusRefCRDs is testCRDs plus a status schema on the namespaced Queue and
// a QueuePolicy kind shaped like the real provider-aws-sqs one: forProvider
// carries queueUrl (the field a cross-resource wire targets), policy and a
// required region.
func statusRefCRDs(t *testing.T) []schema.CRD {
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
                  arn: {type: string}
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
                required: [region]
                properties:
                  region: {type: string}
                  policy: {type: string}
                  queueUrl: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// statusRefTestBlueprint wires queue-policy's queueUrl from main-queue's
// observed status.atProvider.url.
func statusRefTestBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueuepair"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.hooli.tech", Kind: "XQueuePair", Plural: "xqueuepairs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
				},
			}, {
				Name: "queue-policy", Kind: "QueuePolicy",
				Fields: map[string]blueprint.Field{
					"region":   {Value: "eu-north-1"},
					"policy":   {Value: `{"Version":"2012-10-17"}`},
					"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
				},
			}},
		},
	}
}

// TestStatusRefGoldenTemplate pins the emitted template body byte-for-byte:
// the hasKey guard chain over $.observed.resources, the index-based
// dereference (a resource name is a DNS label with hyphens, which template
// field access cannot express), and the field order. Determinism is a
// correctness requirement, so the golden is exact.
func TestStatusRefGoldenTemplate(t *testing.T) {
	got, err := Composition(statusRefTestBlueprint(), statusRefCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	want := `{{- $spec := .observed.composite.resource.spec -}}
{{- $xr := .observed.composite.resource.metadata.name -}}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "main-queue" }}
spec:
  forProvider:
    region: 'eu-north-1'
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: QueuePolicy
metadata:
  annotations:
    {{ setResourceNameAnnotation "queue-policy" }}
spec:
  forProvider:
    policy: '{"Version":"2012-10-17"}'
    {{- if and (hasKey $.observed "resources") (hasKey $.observed.resources "main-queue") (hasKey (index $.observed.resources "main-queue").resource "status") (hasKey (index $.observed.resources "main-queue").resource.status "atProvider") (hasKey (index $.observed.resources "main-queue").resource.status.atProvider "url") }}
    queueUrl: {{ (index $.observed.resources "main-queue").resource.status.atProvider.url }}
    {{- end }}
    region: 'eu-north-1'
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// TestStatusRefRendersObservedValueAndOmitsUnobserved executes the emitted
// template the way the real engine does and proves both halves of the wire's
// contract on the rendered ARTIFACT:
//
//   - target observed  -> the value flows into queueUrl;
//   - nothing observed -> the field is omitted cleanly (the protojson form
//     of an empty observed map is NO resources key at all, which is exactly
//     what the first hasKey link guards);
//   - target observed but status not yet written -> omitted cleanly too.
//
// In every case: a successful render and no "<no value>"/"<nil>".
func TestStatusRefRendersObservedValueAndOmitsUnobserved(t *testing.T) {
	got, err := Composition(statusRefTestBlueprint(), statusRefCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)
	const url = "https://sqs.eu-north-1.amazonaws.com/123456789012/demo"

	cases := []struct {
		name     string
		observed map[string]any // nil: no resources key at all, like protojson
		wantURL  bool
	}{
		{"observed: the value flows", map[string]any{
			"main-queue": map[string]any{
				"resource": map[string]any{
					"status": map[string]any{
						"atProvider": map[string]any{"url": url},
					},
				},
			},
		}, true},
		{"nothing observed: field omitted, render succeeds", nil, false},
		{"observed with empty status: field omitted, render succeeds", map[string]any{
			"main-queue": map[string]any{
				"resource": map[string]any{
					"status": map[string]any{},
				},
			},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderTemplateObserved(t, tmplBody,
				map[string]any{"providerName": "aws-provider"}, tc.observed)
			if err != nil {
				t.Fatalf("render: %v\n---\n%s", err, tmplBody)
			}
			docs := renderedDocs(t, rendered)
			if len(docs) != 2 {
				t.Fatalf("got %d rendered documents, want 2 (Queue and QueuePolicy)\n---\n%s", len(docs), rendered)
			}
			var gotURL any
			var urlPresent bool
			for _, doc := range docs {
				if doc["kind"] != "QueuePolicy" {
					continue
				}
				spec, _ := doc["spec"].(map[string]any)
				fp, _ := spec["forProvider"].(map[string]any)
				gotURL, urlPresent = fp["queueUrl"]
			}
			if tc.wantURL {
				if gotURL != url {
					t.Errorf("queueUrl = %v, want %q\n---\n%s", gotURL, url, rendered)
				}
			} else if urlPresent {
				t.Errorf("queueUrl must be omitted entirely when the target is unobserved, got %v\n---\n%s",
					gotURL, rendered)
			}
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(rendered, bad) {
					t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
				}
			}
		})
	}
}

// A resource whose EVERY field is a status reference exercises the
// all-conditional wrapper: unobserved, forProvider must render as an empty
// map, not null.
func TestStatusRefOnlyResourceRendersEmptyMapWhenUnobserved(t *testing.T) {
	b := statusRefTestBlueprint()
	b.Spec.Resources[1].Fields = map[string]blueprint.Field{
		"queueUrl": {From: "resources.main-queue.status.atProvider.url"},
	}
	got, err := Composition(b, statusRefCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "aws-provider"}, nil)
	if err != nil {
		t.Fatalf("render must succeed with nothing observed, got: %v", err)
	}
	for _, doc := range renderedDocs(t, rendered) {
		if doc["kind"] != "QueuePolicy" {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		fp, present := spec["forProvider"]
		if !present || fp == nil {
			t.Fatalf("forProvider = %v, want an empty map, not null/absent -- a structural schema "+
				"with type: object rejects an explicit null at apply time\n---\n%s", fp, rendered)
		}
	}
}

func TestStatusRefUnknownPathIsRejectedWithASuggestion(t *testing.T) {
	b := statusRefTestBlueprint()
	b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{From: "resources.main-queue.status.atProvider.ur"}
	_, err := Composition(b, statusRefCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a status path absent from the target CRD's status schema; " +
			"the guard would stay false forever and the field silently never materialise")
	}
	if !strings.Contains(err.Error(), "atProvider.ur") {
		t.Errorf("err = %v, want it to name the offending path", err)
	}
	if !strings.Contains(err.Error(), `"atProvider.url"`) {
		t.Errorf("err = %v, want it to suggest the closest scalar status path", err)
	}
}

// A path that resolves to a non-scalar (the atProvider branch itself, or the
// tags map) must be refused: a bare dereference would render Go's fmt of the
// map -- valid YAML, silently wrong.
func TestStatusRefNonScalarPathIsRejected(t *testing.T) {
	for _, path := range []string{
		"resources.main-queue.status.atProvider",
		"resources.main-queue.status.atProvider.tags",
	} {
		t.Run(path, func(t *testing.T) {
			b := statusRefTestBlueprint()
			b.Spec.Resources[1].Fields["queueUrl"] = blueprint.Field{From: path}
			_, err := Composition(b, statusRefCRDs(t))
			if err == nil {
				t.Fatalf("Composition accepted %q, which does not resolve to a scalar status leaf", path)
			}
		})
	}
}

// The legacy fixture Queue (testCRDs) declares no status at all; a reference
// into it must fail loudly rather than emit a guard that can never be true.
func TestStatusRefIntoKindWithoutStatusIsRejected(t *testing.T) {
	crds := statusRefCRDs(t)
	// Strip the Queue's status by re-using the original no-status fixture.
	crds = append(testCRDs(t), crds[1]) // no-status Queue variants + QueuePolicy
	_, err := Composition(statusRefTestBlueprint(), crds)
	if err == nil {
		t.Fatal("Composition accepted a status reference into a kind whose CRD declares no status")
	}
	if !strings.Contains(err.Error(), "no status schema") {
		t.Errorf("err = %v, want it to say the kind declares no status schema", err)
	}
}

func TestStatusRefEmitIsDeterministic(t *testing.T) {
	first, err := Composition(statusRefTestBlueprint(), statusRefCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(statusRefTestBlueprint(), statusRefCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}

// renderTemplateObserved is renderTemplate with control over the observed
// resources map. observedResources == nil omits the "resources" key from
// .observed entirely -- the exact shape function-go-templating sees on a
// first reconcile, because protojson omits an empty protobuf map.
func renderTemplateObserved(t *testing.T, tmplBody string, xrSpec map[string]any, observedResources map[string]any) (string, error) {
	t.Helper()
	observed := map[string]any{
		"composite": map[string]any{
			"resource": map[string]any{
				"metadata": map[string]any{"name": "my-xqueue"},
				"spec":     xrSpec,
			},
		},
	}
	if observedResources != nil {
		observed["resources"] = observedResources
	}
	return renderTemplateData(t, tmplBody, map[string]any{"observed": observed})
}

// TestStatusRefBlueprintRoundTripsExactly mirrors TestForEachRoundTripsExactly
// for the grown from: grammar: the HTTP API persists the whole document by
// re-marshaling the Go struct, so the reference string must survive a
// marshal/unmarshal round trip byte-exactly.
func TestStatusRefBlueprintRoundTripsExactly(t *testing.T) {
	b := statusRefTestBlueprint()
	body, err := yaml.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reloaded blueprint.Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if diff := cmp.Diff(b, &reloaded); diff != "" {
		t.Errorf("blueprint changed across a marshal/unmarshal round trip (-original +reloaded):\n%s", diff)
	}
}

// --- status references into and out of NATIVE targets ---
//
// The merge ruling: a status reference resolves its TARGET through the same
// provider-aware resolveKind the emitter uses for the target's own document,
// so a native target (provider: k8s) is checked against its vendored status
// subtree (Deployment.status.readyReplicas and friends) exactly the way a
// managed target is checked against its CRD's — one path, both families.

// TestStatusRefToNativeTargetRenders wires a managed Queue's field from a
// native Deployment's observed status and executes the emitted template both
// ways: unobserved omits the field cleanly, observed carries the value.
func TestStatusRefToNativeTargetRenders(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "resources.web.status.readyReplicas"}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate refused a status reference to a native resource: %v", err)
	}
	comp, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, comp)
	spec := map[string]any{"providerName": "p", "image": "nginx:1.29"}

	rendered, err := renderTemplateObserved(t, tmpl, spec, nil)
	if err != nil {
		t.Fatalf("render (unobserved): %v\n---\n%s", err, tmpl)
	}
	if strings.Contains(rendered, "maxMessageSize") {
		t.Errorf("maxMessageSize must be omitted until the Deployment is observed\n---\n%s", rendered)
	}

	rendered, err = renderTemplateObserved(t, tmpl, spec, map[string]any{
		"web": map[string]any{"resource": map[string]any{
			"status": map[string]any{"readyReplicas": float64(3)},
		}},
	})
	if err != nil {
		t.Fatalf("render (observed): %v\n---\n%s", err, tmpl)
	}
	if !strings.Contains(rendered, "maxMessageSize: 3") {
		t.Errorf("observed native status did not flow across the wire\n---\n%s", rendered)
	}
}

// TestStatusRefFromNativeResourceRenders is the other direction: a NATIVE
// resource carrying a status-wired leaf. This exercises the guard through the
// native tree writer (writeNativeLeaf and the branch-guard aggregation over
// guard strings — the refactor the merge carried into native.go): the
// containers[0] element keeps its unconditional name while the wired image
// appears only once the managed Queue's status has been observed.
func TestStatusRefFromNativeResourceRenders(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds := append(native, statusRefCRDs(t)...)

	b := statusRefTestBlueprint()
	b.Spec.Resources = append(b.Spec.Resources[:1], blueprint.Resource{
		Name: "web", Kind: "Deployment", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{
			"spec.selector.matchLabels":              {Raw: "{app: web}"},
			"spec.template.metadata.labels":          {Raw: "{app: web}"},
			"spec.template.spec.containers[0].name":  {Value: "web"},
			"spec.template.spec.containers[0].image": {From: "resources.main-queue.status.atProvider.url"},
		},
	})
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, comp)
	spec := map[string]any{"providerName": "p"}

	rendered, err := renderTemplateObserved(t, tmpl, spec, nil)
	if err != nil {
		t.Fatalf("render (unobserved): %v\n---\n%s", err, tmpl)
	}
	if strings.Contains(rendered, "image:") {
		t.Errorf("the wired image must be omitted until the queue is observed\n---\n%s", rendered)
	}
	if !strings.Contains(rendered, "name: web") {
		t.Errorf("the unconditional container name must render regardless of the wire\n---\n%s", rendered)
	}

	rendered, err = renderTemplateObserved(t, tmpl, spec, map[string]any{
		"main-queue": map[string]any{"resource": map[string]any{
			"status": map[string]any{"atProvider": map[string]any{"url": "queue-url"}},
		}},
	})
	if err != nil {
		t.Fatalf("render (observed): %v\n---\n%s", err, tmpl)
	}
	if !strings.Contains(rendered, "image: queue-url") {
		t.Errorf("observed managed status did not flow into the native leaf\n---\n%s", rendered)
	}
}

// A typo'd status path on a NATIVE target must fail at generation time
// against the vendored status schema, with the same suggestion machinery a
// managed target gets.
func TestStatusRefToNativeTargetRejectsUnknownPath(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "resources.web.status.readyReplicaz"}
	_, err := Composition(b, nativeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a status path the vendored Deployment schema does not declare")
	}
	for _, want := range []string{"readyReplicaz", "Deployment", "status schema", `"readyReplicas"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A non-scalar status path on a native target (Deployment.status.conditions,
// an array) is refused for the same fmt-formatting reason a managed one is.
func TestStatusRefToNativeTargetRejectsNonScalarPath(t *testing.T) {
	b := nativeTestBlueprint()
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "resources.web.status.conditions"}
	_, err := Composition(b, nativeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted an array-typed status path on a native target")
	}
	if !strings.Contains(err.Error(), "scalar") {
		t.Errorf("error %q does not explain the scalar-only rule", err)
	}
}
