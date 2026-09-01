package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// conventionCRDs is a namespaced Queue whose forProvider carries the two
// convention targets the acceptance path uses on the real provider — name
// (a scalar) and tags (a map) — plus required region and a nested branch
// (endpoint) that a convention must NOT touch.
func conventionCRDs(t *testing.T) []schema.CRD {
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
                  name: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  endpoint:
                    properties:
                      hostname: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// conventionTestBlueprint: two queues under a naming and a tags convention,
// with queue-b overriding the name explicitly — the override case.
func conventionTestBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueueconv"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.hooli.tech", Kind: "XQueueConv", Plural: "xqueueconvs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Templates: map[string]string{
				"cf.name": "{{ .xr }}-{{ .resource }}",
				"cf.tags": "managed-by: crossplane\nxr: {{ .xr | quote }}",
			},
			Conventions: []blueprint.Convention{
				{Match: "name", Template: "cf.name"},
				{Match: "tags", Template: "cf.tags"},
			},
			Resources: []blueprint.Resource{{
				Name: "queue-a", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
				},
			}, {
				Name: "queue-b", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region": {Value: "eu-north-1"},
					// Explicit wins over the naming convention: that IS the
					// override mechanism.
					"name": {Value: "custom-b"},
				},
			}},
		},
	}
}

// TestTemplatesGoldenTemplate pins the emitted body byte-for-byte: the
// define blocks heading the template in sorted order (byte-identical to what
// blueprint validation parsed), the convention-injected include calls, and
// queue-b's explicit name winning over the convention.
func TestTemplatesGoldenTemplate(t *testing.T) {
	got, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	want := `{{- define "cf.name" }}
{{ .xr }}-{{ .resource }}
{{- end }}
{{- define "cf.tags" }}
managed-by: crossplane
xr: {{ .xr | quote }}
{{- end }}
{{- $spec := .observed.composite.resource.spec -}}
{{- $xr := .observed.composite.resource.metadata.name -}}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "queue-a" }}
spec:
  forProvider:
    name: {{ include "cf.name" (dict "spec" $spec "xr" $xr "resource" "queue-a" "field" "name") | trim | nindent 6 }}
    region: 'eu-north-1'
    tags: {{ include "cf.tags" (dict "spec" $spec "xr" $xr "resource" "queue-a" "field" "tags") | trim | nindent 6 }}
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "queue-b" }}
spec:
  forProvider:
    name: 'custom-b'
    region: 'eu-north-1'
    tags: {{ include "cf.tags" (dict "spec" $spec "xr" $xr "resource" "queue-b" "field" "tags") | trim | nindent 6 }}
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// TestConventionsRenderOnTheArtifact executes the emitted template and
// proves the contract structurally on the rendered YAML: the scalar template
// output becomes queue-a's forProvider.name, the multi-line template output
// becomes a real tags MAPPING (not a string) on both queues, and queue-b's
// explicit name overrides the convention. The nested endpoint branch stays
// untouched.
func TestConventionsRenderOnTheArtifact(t *testing.T) {
	got, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, got), map[string]any{
		"providerName": "aws-provider",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	byName := map[string]map[string]any{}
	for _, doc := range renderedDocs(t, rendered) {
		meta, _ := doc["metadata"].(map[string]any)
		anns, _ := meta["annotations"].(map[string]any)
		name, _ := anns["crossplane.io/composition-resource-name"].(string)
		spec, _ := doc["spec"].(map[string]any)
		fp, _ := spec["forProvider"].(map[string]any)
		byName[name] = fp
	}

	a, ok := byName["queue-a"]
	if !ok {
		t.Fatalf("no queue-a document rendered\n---\n%s", rendered)
	}
	if a["name"] != "my-xqueue-queue-a" {
		t.Errorf("queue-a name = %v, want my-xqueue-queue-a (the naming convention)", a["name"])
	}
	bq, ok := byName["queue-b"]
	if !ok {
		t.Fatalf("no queue-b document rendered\n---\n%s", rendered)
	}
	if bq["name"] != "custom-b" {
		t.Errorf("queue-b name = %v, want the explicit custom-b to win over the convention", bq["name"])
	}
	for _, q := range []string{"queue-a", "queue-b"} {
		tags, isMap := byName[q]["tags"].(map[string]any)
		if !isMap {
			t.Fatalf("%s tags = %v (%T), want a YAML mapping -- the multi-line template output "+
				"must nest under the key, not collapse into a string\n---\n%s",
				q, byName[q]["tags"], byName[q]["tags"], rendered)
		}
		if tags["managed-by"] != "crossplane" || tags["xr"] != "my-xqueue" {
			t.Errorf("%s tags = %v, want managed-by: crossplane and xr: my-xqueue", q, tags)
		}
		if _, present := byName[q]["endpoint"]; present {
			t.Errorf("%s grew an endpoint field -- a convention must only touch top-level leaves", q)
		}
	}
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(rendered, bad) {
			t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
		}
	}
}

// A field template call must also work outside conventions, composed with
// forEach: the context's .resource is the un-indexed node name.
func TestFieldTemplateInsideForEachRenders(t *testing.T) {
	b := conventionTestBlueprint()
	b.Spec.Conventions = nil
	b.Spec.XRD.Parameters["instanceCount"] = blueprint.Parameter{Type: "integer", Default: "2"}
	b.Spec.Resources = b.Spec.Resources[:1]
	b.Spec.Resources[0].ForEach = "params.instanceCount"
	b.Spec.Resources[0].Fields = map[string]blueprint.Field{
		"region": {Value: "eu-north-1"},
		"name":   {Template: "cf.name"},
	}
	got, err := Composition(b, conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, got), map[string]any{
		"providerName": "aws-provider", "instanceCount": float64(2),
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Structural, not a string match: nindent puts a scalar on the line
	// after its key ("name:\n      my-xqueue-queue-a"), which is the same
	// YAML value in a different serialization.
	docs := renderedDocs(t, rendered)
	if len(docs) != 2 {
		t.Fatalf("got %d rendered documents, want 2 loop iterations\n---\n%s", len(docs), rendered)
	}
	for _, doc := range docs {
		spec, _ := doc["spec"].(map[string]any)
		fp, _ := spec["forProvider"].(map[string]any)
		if fp["name"] != "my-xqueue-queue-a" {
			t.Errorf("forProvider.name = %v, want the template-set my-xqueue-queue-a on every "+
				"loop iteration\n---\n%s", fp["name"], rendered)
		}
	}
}

func TestTemplatesEmitIsDeterministic(t *testing.T) {
	first, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(conventionTestBlueprint(), conventionCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}
