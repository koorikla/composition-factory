// Tests for typed object parameters through the emitters: the XRD's real
// nested member schema, and the Composition's member wires with the three
// guard shapes — optional object (hasKey chain on both levels), optional
// member in a required object (member-level hasKey), required member in a
// required object (bare dereference).
package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
)

// typedTestCRDs is the namespaced .m. Queue with enough forProvider leaves
// to wire all three guard shapes at once.
func typedTestCRDs(t *testing.T) []schema.CRD {
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
                  maxMessageSize: {type: integer}
                  delaySeconds: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// typedTestBlueprint declares two typed object parameters — tuning
// (optional) and settings (required, with one required and one optional
// member) — and wires one member of each shape into main-queue.
func typedTestBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.hooli.tech", Kind: "XQueue", Plural: "xqueues",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"tuning": {Type: "object", Properties: map[string]blueprint.Parameter{
						"maxSize": {Type: "integer", Default: "2048", Description: "Maximum message size."},
					}},
					"settings": {Type: "object", Required: true, Properties: map[string]blueprint.Parameter{
						"queueName": {Type: "string", Required: true},
						"delay":     {Type: "integer"},
					}},
				},
			},
			Resources: []blueprint.Resource{{
				Name: "main-queue", Kind: "Queue",
				Fields: map[string]blueprint.Field{
					"region":         {Value: "eu-north-1"},
					"maxMessageSize": {From: "params.tuning.maxSize"},
					"name":           {From: "params.settings.queueName"},
					"delaySeconds":   {From: "params.settings.delay"},
				},
			}},
		},
	}
}

// --- XRD: a real nested schema, not the free-form map ---

func TestXRDTypedObjectEmitsNestedSchema(t *testing.T) {
	b := typedTestBlueprint()
	p := b.Spec.XRD.Parameters["settings"]
	p.Properties["mode"] = blueprint.Parameter{Type: "string", Enum: []string{"standard", "fifo"}, Default: "standard"}
	b.Spec.XRD.Parameters["settings"] = p

	got, err := XRD(b)
	if err != nil {
		t.Fatalf("XRD: %v", err)
	}
	props := paramProps(t, got)

	settings, ok := props["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings schema: expected a map, got %T", props["settings"])
	}
	if _, has := settings["additionalProperties"]; has {
		t.Error("a typed object must not carry additionalProperties — members ARE the schema")
	}
	members, ok := settings["properties"].(map[string]any)
	if !ok {
		t.Fatalf("settings.properties: expected a map, got %T (%v)", settings["properties"], settings["properties"])
	}
	queueName := dig(t, members, "queueName")
	if typ := dig(t, queueName, "type"); typ != "string" {
		t.Errorf("queueName.type = %v, want string", typ)
	}
	if typ := dig(t, members, "delay", "type"); typ != "integer" {
		t.Errorf("delay.type = %v, want integer", typ)
	}
	if def := dig(t, members, "mode", "default"); def != "standard" {
		t.Errorf("mode.default = %v, want standard", def)
	}
	enum, ok := dig(t, members, "mode", "enum").([]any)
	if !ok || len(enum) != 2 || enum[0] != "standard" || enum[1] != "fifo" {
		t.Errorf("mode.enum = %v, want [standard fifo]", enum)
	}
	req, ok := settings["required"].([]any)
	if !ok || len(req) != 1 || req[0] != "queueName" {
		t.Errorf("settings.required = %v, want exactly [queueName]", settings["required"])
	}

	// tuning: default and description flow through per member, and an
	// optional-only member set emits no required list at all.
	tuning := dig(t, props, "tuning").(map[string]any)
	if def := dig(t, tuning, "properties", "maxSize", "default"); def != float64(2048) {
		t.Errorf("maxSize.default = %v (%T), want the number 2048 — a quoted integer default is a "+
			"type mismatch the API server rejects", def, def)
	}
	if desc := dig(t, tuning, "properties", "maxSize", "description"); desc != "Maximum message size." {
		t.Errorf("maxSize.description = %v, want the declared text", desc)
	}
	if v, present := tuning["required"]; present {
		t.Errorf("tuning.required = %v, but no member is required — an empty required list is "+
			"schema noise", v)
	}
}

// TestTypedParamXRDGolden pins the whole emitted XRD byte-for-byte for the
// typed blueprint: member key order (sorted), per-member attribute order
// (type, description, default, enum), the member-level required list, and
// the absence of additionalProperties are all part of the determinism
// contract — any drift is a diff here before it is a churning file on a
// prune:true GitOps repo.
func TestTypedParamXRDGolden(t *testing.T) {
	got, err := XRD(typedTestBlueprint())
	if err != nil {
		t.Fatalf("XRD: %v", err)
	}
	want := `# Generated by compositionfactory. Do not edit.
# Source: blueprints/xqueue.cf.yaml
# Regenerate with: cf gen
apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: xqueues.platform.hooli.tech
spec:
  group: platform.hooli.tech
  names:
    kind: XQueue
    plural: xqueues
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              providerName:
                type: string
              settings:
                type: object
                properties:
                  delay:
                    type: integer
                  queueName:
                    type: string
                required: [queueName]
              tuning:
                type: object
                properties:
                  maxSize:
                    type: integer
                    description: 'Maximum message size.'
                    default: 2048
            required: [providerName, settings]
# required lists only the parameters the blueprint marks Required.
# A merely-dereferenced parameter is safe unforced: the Composition
# guards every optional access with hasKey, never a bare dereference.
`
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("XRD drifted (-want +got):\n%s", diff)
	}
}

// A propertyless object parameter keeps the v1 free-form map schema exactly:
// additionalProperties string, no properties, no required.
func TestXRDPropertylessObjectKeepsFreeFormMap(t *testing.T) {
	b := typedTestBlueprint()
	b.Spec.XRD.Parameters["labels"] = blueprint.Parameter{Type: "object"}

	got, err := XRD(b)
	if err != nil {
		t.Fatalf("XRD: %v", err)
	}
	labels := dig(t, paramProps(t, got), "labels").(map[string]any)
	ap, ok := labels["additionalProperties"].(map[string]any)
	if !ok || ap["type"] != "string" {
		t.Errorf("labels.additionalProperties = %v, want {type: string} — a propertyless object "+
			"parameter must keep the free-form map semantics untouched", labels["additionalProperties"])
	}
	if _, has := labels["properties"]; has {
		t.Error("a propertyless object parameter must not gain a properties key")
	}
}

func TestXRDTypedObjectIsDeterministic(t *testing.T) {
	a, err := XRD(typedTestBlueprint())
	if err != nil {
		t.Fatalf("XRD (first run): %v", err)
	}
	b, err := XRD(typedTestBlueprint())
	if err != nil {
		t.Fatalf("XRD (second run): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two runs produced different bytes; member iteration must be sorted")
	}
}

// --- Composition: the three guard shapes, pinned byte-for-byte ---

func TestTypedParamGoldenTemplate(t *testing.T) {
	got, err := Composition(typedTestBlueprint(), typedTestCRDs(t))
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
    {{- if hasKey $spec.settings "delay" }}
    delaySeconds: {{ $spec.settings.delay }}
    {{- end }}
    {{- if and (hasKey $spec "tuning") (hasKey $spec.tuning "maxSize") }}
    maxMessageSize: {{ $spec.tuning.maxSize }}
    {{- end }}
    name: {{ $spec.settings.queueName | quote }}
    region: 'eu-north-1'
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// The proof that matters: the emitted template is really executed under
// missingkey=error for every optionality shape an XR can take.
func TestTypedParamWiresRenderUnderMissingKeyError(t *testing.T) {
	got, err := Composition(typedTestBlueprint(), typedTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	forProvider := func(t *testing.T, rendered string) map[string]any {
		t.Helper()
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, rendered)
		}
		spec, _ := doc["spec"].(map[string]any)
		fp, _ := spec["forProvider"].(map[string]any)
		if fp == nil {
			t.Fatalf("no forProvider map in rendered output\n---\n%s", rendered)
		}
		return fp
	}

	t.Run("every member present renders every wire", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
			"tuning":       map[string]any{"maxSize": float64(4096)},
			"settings":     map[string]any{"queueName": "orders", "delay": float64(5)},
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		fp := forProvider(t, rendered)
		if fp["maxMessageSize"] != float64(4096) || fp["name"] != "orders" || fp["delaySeconds"] != float64(5) {
			t.Errorf("forProvider = %v, want maxMessageSize 4096, name orders, delaySeconds 5", fp)
		}
	})

	t.Run("optional object absent omits its wire cleanly", func(t *testing.T) {
		// tuning is genuinely absent — the ordinary case for an optional
		// object the XR never set. The first hasKey link is what keeps this
		// from hard-failing under missingkey=error.
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
			"settings":     map[string]any{"queueName": "orders"},
		})
		if err != nil {
			t.Fatalf("render must succeed with the optional object absent, got: %v\n---\n%s", err, tmplBody)
		}
		fp := forProvider(t, rendered)
		if _, present := fp["maxMessageSize"]; present {
			t.Errorf("maxMessageSize must be omitted when tuning is absent, got %v", fp)
		}
		if _, present := fp["delaySeconds"]; present {
			t.Errorf("delaySeconds must be omitted when settings.delay is absent, got %v", fp)
		}
		if fp["name"] != "orders" {
			t.Errorf("name = %v, want the required-in-required wire to render bare", fp["name"])
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(rendered, bad) {
				t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
			}
		}
	})

	t.Run("required object with only its required member renders", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
			"tuning":       map[string]any{"maxSize": float64(1024)},
			"settings":     map[string]any{"queueName": "orders"},
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		fp := forProvider(t, rendered)
		if fp["maxMessageSize"] != float64(1024) {
			t.Errorf("maxMessageSize = %v, want 1024", fp["maxMessageSize"])
		}
	})
}

// A member reference the parameter does not declare must be refused here
// too, not only in blueprint.Validate: Composition is exported, and the
// unknown key would otherwise reach the template as a dereference that
// hard-fails every render (or worse, renders <no value> without the
// missingkey option).
func TestCompositionRejectsUnknownMemberReference(t *testing.T) {
	b := typedTestBlueprint()
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "params.tuning.nope"}
	_, err := Composition(b, typedTestCRDs(t))
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "tuning") {
		t.Fatalf("err = %v, want an error naming the unknown member and its parameter", err)
	}
}

// --- envelope entries take member wires with the same guard shapes ---

// typedEnvelopeBlueprint wires writeConnectionSecretToRef.name from a member
// of the conn object parameter; required steers which guard shape emerges.
func typedEnvelopeBlueprint(connRequired bool) *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.XRD.Parameters["conn"] = blueprint.Parameter{
		Type: "object", Required: connRequired,
		Properties: map[string]blueprint.Parameter{
			"secretName": {Type: "string"},
		},
	}
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"writeConnectionSecretToRef.name": {From: "params.conn.secretName"},
	}
	return b
}

func TestEnvelopeMemberWireGuardShapes(t *testing.T) {
	t.Run("optional object gets the two-level hasKey chain", func(t *testing.T) {
		got, err := Composition(typedEnvelopeBlueprint(false), envelopeTestCRDs(t))
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		tmpl := extractTemplate(t, got)
		if !strings.Contains(tmpl, `{{- if and (hasKey $spec "conn") (hasKey $spec.conn "secretName") }}`) {
			t.Errorf("missing the two-level guard chain\n---\n%s", tmpl)
		}
	})
	t.Run("required object gets the member-level hasKey", func(t *testing.T) {
		got, err := Composition(typedEnvelopeBlueprint(true), envelopeTestCRDs(t))
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		tmpl := extractTemplate(t, got)
		if !strings.Contains(tmpl, `{{- if hasKey $spec.conn "secretName" }}`) {
			t.Errorf("missing the member-level guard\n---\n%s", tmpl)
		}
		if strings.Contains(tmpl, `(hasKey $spec "conn")`) {
			t.Errorf("a required object needs no top-level hasKey — the XRD gate makes it present\n---\n%s", tmpl)
		}
	})
}

// Executed both ways: the optional-object member wire renders the nested
// envelope group when present and omits it entirely — key line included —
// when absent.
func TestEnvelopeMemberWireRendersOrDisappears(t *testing.T) {
	got, err := Composition(typedEnvelopeBlueprint(false), envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("member present renders the group", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
			"conn":         map[string]any{"secretName": "queue-conn"},
		})
		if err != nil {
			t.Fatalf("render: %v\n---\n%s", err, tmplBody)
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, rendered)
		}
		spec := doc["spec"].(map[string]any)
		ref, ok := spec["writeConnectionSecretToRef"].(map[string]any)
		if !ok || ref["name"] != "queue-conn" {
			t.Errorf("writeConnectionSecretToRef = %v, want map with name queue-conn\n---\n%s",
				spec["writeConnectionSecretToRef"], rendered)
		}
	})

	t.Run("object absent omits the group entirely", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
		})
		if err != nil {
			t.Fatalf("render must succeed with the object absent, got: %v\n---\n%s", err, tmplBody)
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(rendered), &doc); err != nil {
			t.Fatalf("rendered output is not valid YAML: %v\n---\n%s", err, rendered)
		}
		spec := doc["spec"].(map[string]any)
		if v, present := spec["writeConnectionSecretToRef"]; present {
			t.Errorf("writeConnectionSecretToRef must be omitted entirely, got %v\n---\n%s", v, rendered)
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(rendered, bad) {
				t.Errorf("rendered output contains %q\n---\n%s", bad, rendered)
			}
		}
	})
}

// The wire's type compatibility is judged against the MEMBER's type, not the
// object's: an integer member cannot land on a string envelope leaf.
func TestEnvelopeMemberWireTypeChecksAgainstMemberType(t *testing.T) {
	b := typedEnvelopeBlueprint(true)
	p := b.Spec.XRD.Parameters["conn"]
	p.Properties["secretName"] = blueprint.Parameter{Type: "integer"}
	b.Spec.XRD.Parameters["conn"] = p
	_, err := Composition(b, envelopeTestCRDs(t))
	if err == nil || !strings.Contains(err.Error(), `"integer"`) {
		t.Fatalf("err = %v, want a type-mismatch error naming the member's type", err)
	}
}

func TestTypedParamEmitIsDeterministic(t *testing.T) {
	first, err := Generate(typedTestBlueprint(), typedTestCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (first run): %v", err)
	}
	second, err := Generate(typedTestBlueprint(), typedTestCRDs(t), "out")
	if err != nil {
		t.Fatalf("Generate (second run): %v", err)
	}
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("output counts differ or are empty: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path || !bytes.Equal(first[i].Body, second[i].Body) {
			t.Errorf("output %q differs between runs", first[i].Path)
		}
	}
}
