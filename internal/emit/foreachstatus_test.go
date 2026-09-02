// Tests for the observed-count forEach form: `forEach:
// resources.<name>.status.<path>`, the loop bound read at render time from
// another composed resource's OBSERVED status. Every render-semantics claim
// is proven by executing the emitted template body with Go's real
// text/template under Option("missingkey=error") — the engine
// function-go-templating wraps — never by string-matching alone.
package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// forEachStatusCRDs is a namespaced Queue whose status declares every leaf
// class an observed loop bound can hit: an integer (the happy path), a
// number (protojson delivers every count as float64, so number leaves are
// equally loopable), a string, a map, an untyped leaf and an object branch.
func forEachStatusCRDs(t *testing.T) []schema.CRD {
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
          status:
            properties:
              atProvider:
                properties:
                  nodeCount: {type: integer}
                  replicaFactor: {type: number}
                  url: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  mystery: {}
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

// forEachStatusBlueprint composes a main-queue and a replica-queue fanned
// out by main-queue's observed nodeCount.
func forEachStatusBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue-fan"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XQueueFan", Plural: "xqueuefans",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "main-queue", Kind: "Queue",
					Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
				},
				{
					Name: "replica-queue", Kind: "Queue",
					ForEach: "resources.main-queue.status.atProvider.nodeCount",
					Fields:  map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
				},
			},
		},
	}
}

// The guard chain and dereference for main-queue's status.atProvider.nodeCount,
// exactly as statusGuard builds them — shared by the golden below.
const (
	nodeCountGuard = `(and (hasKey $.observed "resources") (kindIs "map" $.observed.resources) ` +
		`(hasKey $.observed.resources "main-queue") (kindIs "map" (index $.observed.resources "main-queue")) ` +
		`(hasKey (index $.observed.resources "main-queue") "resource") ` +
		`(kindIs "map" (index $.observed.resources "main-queue").resource) ` +
		`(hasKey (index $.observed.resources "main-queue").resource "status") ` +
		`(kindIs "map" (index $.observed.resources "main-queue").resource.status) ` +
		`(hasKey (index $.observed.resources "main-queue").resource.status "atProvider") ` +
		`(kindIs "map" (index $.observed.resources "main-queue").resource.status.atProvider) ` +
		`(hasKey (index $.observed.resources "main-queue").resource.status.atProvider "nodeCount"))`
	nodeCountExpr = `(index $.observed.resources "main-queue").resource.status.atProvider.nodeCount`
)

// TestStatusForEachGoldenTemplate pins the emitted template body
// byte-for-byte, exactly like TestForEachGoldenTemplate does for the params
// form: determinism is a correctness requirement on a prune:true GitOps
// repo, so any drift in the guard chain, the guarded range wrapper, the
// indexed annotation or the end markers is a diff here before it is a
// churning file there.
func TestStatusForEachGoldenTemplate(t *testing.T) {
	got, err := Composition(forEachStatusBlueprint(), forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	want := `{{- $spec := .observed.composite.resource.spec -}}
{{- $xr := .observed.composite.resource.metadata.name -}}
{{- $xrMeta := .observed.composite.resource.metadata -}}
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
{{- if ` + nodeCountGuard + ` }}
{{- range $i := until (int ` + nodeCountExpr + `) }}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation (printf "replica-queue-%d" $i) }}
spec:
  forProvider:
    region: 'eu-north-1'
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
{{- end }}
{{- end }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// observedNodeCount is a fully-observed main-queue entry reporting the given
// count, in the shape function-go-templating hands the template.
func observedNodeCount(count any) map[string]any {
	return map[string]any{
		"main-queue": map[string]any{
			"resource": map[string]any{
				"status": map[string]any{
					"atProvider": map[string]any{"nodeCount": count},
				},
			},
		},
	}
}

// TestStatusForEachRendersObservedCountInstances executes the emitted
// template and proves the loop semantics on the rendered ARTIFACT: the
// observed count fans the looped resource out to exactly N documents with
// DISTINCT composition-resource-name annotations (§8), beside exactly one
// copy of the source resource. The count arrives as float64 in the main
// cases because that is what the real engine sees — protobuf's Struct
// carries every number as float64 — which is what makes the (int ...) cast
// in the range head load-bearing.
func TestStatusForEachRendersObservedCountInstances(t *testing.T) {
	got, err := Composition(forEachStatusBlueprint(), forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	cases := []struct {
		name  string
		count any
		want  []string
	}{
		{"observed count 3 (float64 as protobuf delivers it)", float64(3),
			[]string{"main-queue", "replica-queue-0", "replica-queue-1", "replica-queue-2"}},
		{"observed count 1", float64(1),
			[]string{"main-queue", "replica-queue-0"}},
		{"observed count 0 renders no looped instances", float64(0),
			[]string{"main-queue"}},
		{"observed count 2 as int64 (a non-protobuf decoder shape)", int64(2),
			[]string{"main-queue", "replica-queue-0", "replica-queue-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderTemplateObserved(t, tmplBody,
				map[string]any{"providerName": "aws-provider"}, observedNodeCount(tc.count))
			if err != nil {
				t.Fatalf("render: %v\n---\n%s", err, tmplBody)
			}
			names := resourceNames(t, renderedDocs(t, rendered))
			if diff := cmp.Diff(tc.want, names); diff != "" {
				t.Errorf("composition-resource-name annotations (-want +got):\n%s", diff)
			}
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(rendered, bad) {
					t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
				}
			}
		})
	}
}

// The load-bearing semantic of the observed bound, proven on the executed
// template: an UNOBSERVED source yields ZERO instances and a clean render —
// nothing exists until the cluster says how many. Each case is one rung of
// the guard chain genuinely missing, plus the degenerate shapes (nil,
// non-map) a hand-written observed fixture or a half-populated resource can
// produce, which hasKey alone would hard-fail on. This is the exact opposite
// of the params form, where an absent bound hard-fails the render — the
// params bound is a spec value XRD gates make unconditional, the status
// bound is observed state that legitimately does not exist yet.
func TestStatusForEachUnobservedRendersZeroInstances(t *testing.T) {
	got, err := Composition(forEachStatusBlueprint(), forEachStatusCRDs(t))
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
		{"nodeCount absent", map[string]any{
			"main-queue": map[string]any{"resource": map[string]any{
				"status": map[string]any{"atProvider": map[string]any{}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderTemplateObserved(t, tmplBody,
				map[string]any{"providerName": "aws-provider"}, tc.observed)
			if err != nil {
				t.Fatalf("render must succeed with the source unobserved, got: %v\n---\n%s", err, tmplBody)
			}
			names := resourceNames(t, renderedDocs(t, rendered))
			if diff := cmp.Diff([]string{"main-queue"}, names); diff != "" {
				t.Errorf("unobserved source must fan out to ZERO instances (-want +got):\n%s", diff)
			}
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(rendered, bad) {
					t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
				}
			}
		})
	}
}

// when composes with the observed bound exactly as with the params form: the
// condition wraps OUTSIDE the guard and the range, so a false condition
// skips the whole fan-out — guard evaluation included — in one test.
func TestStatusForEachWhenStaysOutsideTheGuardedRange(t *testing.T) {
	b := forEachStatusBlueprint()
	b.Spec.XRD.Parameters["replicasEnabled"] = blueprint.Parameter{Type: "boolean", Required: true}
	b.Spec.Resources[1].When = "params.replicasEnabled"
	got, err := Composition(b, forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	iWhen := strings.Index(tmplBody, "{{- if $spec.replicasEnabled }}")
	iGuard := strings.Index(tmplBody, "{{- if (and (hasKey $.observed")
	iRange := strings.Index(tmplBody, "{{- range $i :=")
	if iWhen == -1 || iGuard == -1 || iRange == -1 {
		t.Fatalf("template missing the when condition, the observed guard or the range\n---\n%s", tmplBody)
	}
	if !(iWhen < iGuard && iGuard < iRange) {
		t.Errorf("when must wrap OUTSIDE the guard, which wraps outside the range "+
			"(when@%d guard@%d range@%d)\n---\n%s", iWhen, iGuard, iRange, tmplBody)
	}

	// Executed: condition false skips every iteration even with an observed
	// count; condition true fans out.
	rendered, err := renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "aws-provider", "replicasEnabled": false},
		observedNodeCount(float64(3)))
	if err != nil {
		t.Fatalf("render with the condition false: %v", err)
	}
	if names := resourceNames(t, renderedDocs(t, rendered)); len(names) != 1 || names[0] != "main-queue" {
		t.Errorf("false condition must skip the whole fan-out, got %v", names)
	}
	rendered, err = renderTemplateObserved(t, tmplBody,
		map[string]any{"providerName": "aws-provider", "replicasEnabled": true},
		observedNodeCount(float64(2)))
	if err != nil {
		t.Fatalf("render with the condition true: %v", err)
	}
	if names := resourceNames(t, renderedDocs(t, rendered)); len(names) != 3 {
		t.Errorf("true condition must fan out over the observed count, got %v", names)
	}
}

// A number leaf is as loopable as an integer one: protojson delivers every
// numeric status value as float64 anyway, and upjet providers declare many
// genuinely-integral fields as number — the (int ...) cast handles both.
func TestStatusForEachNumberLeafIsAccepted(t *testing.T) {
	b := forEachStatusBlueprint()
	b.Spec.Resources[1].ForEach = "resources.main-queue.status.atProvider.replicaFactor"
	got, err := Composition(b, forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition rejected a number status leaf as a loop bound: %v", err)
	}
	rendered, err := renderTemplateObserved(t, extractTemplate(t, got),
		map[string]any{"providerName": "aws-provider"},
		map[string]any{"main-queue": map[string]any{"resource": map[string]any{
			"status": map[string]any{"atProvider": map[string]any{"replicaFactor": float64(2)}}}}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if names := resourceNames(t, renderedDocs(t, rendered)); len(names) != 3 {
		t.Errorf("names = %v, want the source plus two instances", names)
	}
}

// --- generation-time refusals ---

// A loop bound must COUNT something: only an integer or number status leaf
// qualifies. A string would make until (int ...) depend on cast.ToInt's
// string parsing of arbitrary provider output, and a map, an untyped leaf or
// an object branch have no defensible count at all.
func TestStatusForEachNonNumericLeafIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"string leaf", "resources.main-queue.status.atProvider.url"},
		{"map leaf", "resources.main-queue.status.atProvider.tags"},
		{"untyped leaf", "resources.main-queue.status.atProvider.mystery"},
		{"object branch", "resources.main-queue.status.atProvider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := forEachStatusBlueprint()
			b.Spec.Resources[1].ForEach = tc.path
			_, err := Composition(b, forEachStatusCRDs(t))
			if err == nil {
				t.Fatalf("Composition accepted %q as a loop bound", tc.path)
			}
			if !strings.Contains(err.Error(), "replica-queue") {
				t.Errorf("err = %v, want it to name the offending resource", err)
			}
		})
	}
}

func TestStatusForEachUnknownPathIsRejectedWithASuggestion(t *testing.T) {
	b := forEachStatusBlueprint()
	b.Spec.Resources[1].ForEach = "resources.main-queue.status.atProvider.nodeCoun" // one character short
	_, err := Composition(b, forEachStatusCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted a status path absent from the source CRD's status schema")
	}
	if !strings.Contains(err.Error(), "nodeCoun") {
		t.Errorf("err = %v, want it to name the offending path", err)
	}
	if !strings.Contains(err.Error(), `"atProvider.nodeCount"`) {
		t.Errorf("err = %v, want it to suggest the closest valid path", err)
	}
}

func TestStatusForEachFromCRDWithoutStatusSchemaIsRejected(t *testing.T) {
	b := forEachStatusBlueprint()
	b.Spec.Resources[0].Kind = "NoStatus"
	_, err := Composition(b, forEachStatusCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("err = %v, want a refusal naming the missing status schema", err)
	}
}

// Determinism: the same blueprint yields byte-identical output, twice.
func TestStatusForEachEmitIsDeterministic(t *testing.T) {
	first, err := Composition(forEachStatusBlueprint(), forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(forEachStatusBlueprint(), forEachStatusCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}
