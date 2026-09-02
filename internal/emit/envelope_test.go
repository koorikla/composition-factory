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

// envelopeTestCRDs is one namespaced .m. Queue whose envelope mirrors the
// REAL v2 namespaced managed-resource shape (verified against
// provider-aws-sqs v2.7.0's queues.sqs.aws.m.upbound.io: managementPolicies
// is an array of enum strings, providerConfigRef is {kind, name} both
// required, writeConnectionSecretToRef carries name only — no namespace, and
// there is no deletionPolicy), plus three synthetic scalar envelope keys
// (enableDriftDetection, syncIntervalSeconds, syncJitterFactor) that exist
// solely so the boolean/integer/number type rules have a leaf to be tested
// against — the real .m. envelope has no scalar leaves outside objects.
func envelopeTestCRDs(t *testing.T) []schema.CRD {
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
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  maxMessageSize: {type: integer}
              managementPolicies:
                type: array
                items: {type: string, enum: ["Observe", "Create", "Update", "Delete", "LateInitialize", "*"]}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                required: [name]
                properties: {name: {type: string}}
              enableDriftDetection: {type: boolean}
              syncIntervalSeconds: {type: integer}
              syncJitterFactor: {type: number}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

// envelopeTestBlueprint is testBlueprint plus a required secretName parameter
// and an envelope exercising all three field forms: a comma-separated value
// on the array leaf, a wire on writeConnectionSecretToRef.name, and a raw
// scalar.
func envelopeTestBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.XRD.Parameters["secretName"] = blueprint.Parameter{Type: "string", Required: true}
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"managementPolicies":              {Value: "Observe, Create"},
		"writeConnectionSecretToRef.name": {From: "params.secretName"},
		"enableDriftDetection":            {Raw: "true"},
	}
	return b
}

// TestEnvelopeGoldenTemplate pins the emitted template body byte-for-byte
// for a resource using every envelope form. What it proves structurally:
// blueprint entries and the computed providerConfigRef merge in sorted
// top-level key order after forProvider; the array value renders as a quoted
// flow sequence; the required wire is unguarded; raw passes verbatim.
func TestEnvelopeGoldenTemplate(t *testing.T) {
	got, err := Composition(envelopeTestBlueprint(), envelopeTestCRDs(t))
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
    {{- if or (hasKey $spec "maxMessageSize") }}
    {{- if hasKey $spec "maxMessageSize" }}
    maxMessageSize: {{ $spec.maxMessageSize }}
    {{- end }}
    {{- else }}
    {}
    {{- end }}
  enableDriftDetection: true
  managementPolicies: ['Observe', 'Create']
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
  writeConnectionSecretToRef:
    name: {{ $spec.secretName | quote }}
`
	if diff := cmp.Diff(want, extractTemplate(t, got)); diff != "" {
		t.Errorf("template body drifted (-want +got):\n%s", diff)
	}
}

// An envelope-free blueprint must emit byte-identically to what the fixed
// envelope produced before this grammar existed — proven at the unit level
// here (the acceptance-level proof is the HEAD-binary diff recorded in the
// task report, and the untouched pipeline/forEach goldens).
func TestEnvelopeFreeBlueprintKeepsTheComputedDefaultBlock(t *testing.T) {
	got, err := Composition(testBlueprint(), envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, got)
	want := `  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
`
	if !strings.HasSuffix(tmpl, want) {
		t.Errorf("envelope-free template must end with exactly the computed providerConfigRef block:\n%s", tmpl)
	}
	for _, stray := range []string{"managementPolicies", "writeConnectionSecretToRef", "deletionPolicy"} {
		if strings.Contains(tmpl, stray) {
			t.Errorf("envelope-free template gained %q\n---\n%s", stray, tmpl)
		}
	}
}

// A group whose every entry is wired from an optional parameter must be
// omitted ENTIRELY when the XR sets none of them — not rendered as a bare
// key (YAML null) or an empty map that the CRD's required-children rule
// rejects — and must render fully when the parameter is present. Proven by
// executing the emitted template, both ways.
func TestOptionalWiredEnvelopeGroupRendersOrDisappears(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.XRD.Parameters["secretName"] = blueprint.Parameter{Type: "string"} // optional now
	got, err := Composition(b, envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	t.Run("parameter present renders the nested group", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
			"secretName":   "queue-conn",
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

	t.Run("parameter absent omits the group entirely", func(t *testing.T) {
		rendered, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "aws-provider",
		})
		if err != nil {
			t.Fatalf("render must succeed when the optional wire's key is absent, got: %v\n---\n%s", err, tmplBody)
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

func TestUnknownEnvelopePathIsRejectedWithASuggestion(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"writeConnectionSecretToRef.nam": {From: "params.secretName"},
	}
	_, err := Composition(b, envelopeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted an envelope path absent from the CRD's envelope schema")
	}
	if !strings.Contains(err.Error(), "writeConnectionSecretToRef.nam") ||
		!strings.Contains(err.Error(), `"writeConnectionSecretToRef.name"`) {
		t.Errorf("err = %v, want it to name the offending path and suggest the close one", err)
	}
}

// The real .m. namespaced envelope carries no writeConnectionSecretToRef
// .namespace — schema-driven checking must reject it rather than assume the
// legacy cluster shape.
func TestNamespaceOnWriteConnectionSecretToRefIsRejectedForNamespacedVariant(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"writeConnectionSecretToRef.namespace": {Value: "default"},
	}
	_, err := Composition(b, envelopeTestCRDs(t))
	if err == nil || !strings.Contains(err.Error(), "writeConnectionSecretToRef.namespace") {
		t.Fatalf("err = %v, want a rejection naming the path (the .m. envelope has name only)", err)
	}
}

// deletionPolicy exists only on the cluster-scoped variant; asking for it on
// a namespaced blueprint gets the variant difference named, not a generic
// unknown-path error.
func TestDeletionPolicyOnNamespacedIsRejectedNamingTheVariantDifference(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"deletionPolicy": {Value: "Orphan"},
	}
	_, err := Composition(b, envelopeTestCRDs(t))
	if err == nil {
		t.Fatal("Composition accepted deletionPolicy on a namespaced .m. variant, which the API server would prune")
	}
	for _, want := range []string{"deletionPolicy", "namespaced", "cluster-scoped"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q (the variant difference must be named)", err, want)
		}
	}
}

// providerConfigRef can be customized per resource in the envelope and emitted.
func TestProviderConfigRefViaEnvelopeIsEmitted(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.Resources[0].Envelope = map[string]blueprint.Field{
		"providerConfigRef.name": {Value: "custom-pc"},
	}
	got, err := Composition(b, envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, got)
	if !strings.Contains(tmpl, "name: 'custom-pc'") {
		t.Errorf("template missing custom providerConfigRef name:\n%s", tmpl)
	}
}

// A native Kubernetes kind has no Crossplane envelope at all; envelope
// entries on one are refused with the reason, not a generic unknown path.
func TestNativeKindRefusesEnvelopeEntries(t *testing.T) {
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	b := testBlueprint()
	b.Spec.XRD.Parameters["secretName"] = blueprint.Parameter{Type: "string", Required: true}
	b.Spec.Resources = []blueprint.Resource{{
		Name: "web-config", Kind: "ConfigMap", Provider: blueprint.NativeProvider,
		Fields: map[string]blueprint.Field{"data": {Raw: "{app: web}"}},
		Envelope: map[string]blueprint.Field{
			"writeConnectionSecretToRef.name": {From: "params.secretName"},
		},
	}}
	_, err = Composition(b, append(native, envelopeTestCRDs(t)...))
	if err == nil {
		t.Fatal("Composition accepted envelope entries on a native kind, which has no Crossplane envelope")
	}
	for _, want := range []string{"native", "not a managed resource"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

func TestEnvelopeTypeRules(t *testing.T) {
	cases := []struct {
		name     string
		envelope map[string]blueprint.Field
		params   map[string]blueprint.Parameter
		wantErr  []string // all must appear; empty means the case must succeed
		wantLine string   // when successful, this line must appear in the template
	}{
		{
			name:     "from onto an array leaf is refused (documented v1 ruling)",
			envelope: map[string]blueprint.Field{"managementPolicies": {From: "params.secretName"}},
			wantErr:  []string{"array", "comma-separated"},
		},
		{
			name:     "value onto an object branch is refused",
			envelope: map[string]blueprint.Field{"writeConnectionSecretToRef": {Value: "conn"}},
			wantErr:  []string{"object", "raw"},
		},
		{
			name:     "from onto an object branch is refused",
			envelope: map[string]blueprint.Field{"writeConnectionSecretToRef": {From: "params.secretName"}},
			wantErr:  []string{"object", "children"},
		},
		{
			name:     "wire type must match the schema type",
			envelope: map[string]blueprint.Field{"syncIntervalSeconds": {From: "params.secretName"}},
			wantErr:  []string{`type "integer"`, `type "string"`},
		},
		{
			name:     "integer wire onto a number leaf is compatible",
			envelope: map[string]blueprint.Field{"syncJitterFactor": {From: "params.retries"}},
			params:   map[string]blueprint.Parameter{"retries": {Type: "integer", Required: true}},
			wantLine: "  syncJitterFactor: {{ $spec.retries }}",
		},
		{
			name:     "non-integer value on an integer leaf is refused",
			envelope: map[string]blueprint.Field{"syncIntervalSeconds": {Value: "abc"}},
			wantErr:  []string{"integer"},
		},
		{
			name:     "integer value emits canonically, unquoted",
			envelope: map[string]blueprint.Field{"syncIntervalSeconds": {Value: "0042"}},
			wantLine: "  syncIntervalSeconds: 42",
		},
		{
			name:     "boolean value must be true or false",
			envelope: map[string]blueprint.Field{"enableDriftDetection": {Value: "yes"}},
			wantErr:  []string{"boolean"},
		},
		{
			name:     "boolean value emits bare",
			envelope: map[string]blueprint.Field{"enableDriftDetection": {Value: "false"}},
			wantLine: "  enableDriftDetection: false",
		},
		{
			name:     "empty entry in a comma-separated array value is refused",
			envelope: map[string]blueprint.Field{"managementPolicies": {Value: "Observe,,Create"}},
			wantErr:  []string{"empty entry"},
		},
		{
			name:     "raw passes verbatim on an array leaf",
			envelope: map[string]blueprint.Field{"managementPolicies": {Raw: `["*"]`}},
			wantLine: `  managementPolicies: ["*"]`,
		},
		{
			name:     "providerConfigRef.name wired to custom parameter",
			envelope: map[string]blueprint.Field{"providerConfigRef.name": {From: "params.customProvider"}},
			params:   map[string]blueprint.Parameter{"customProvider": {Type: "string", Required: true}},
			wantLine: `    name: {{ $spec.customProvider | quote }}`,
		},
		{
			name:     "providerConfigRef.name set to literal value",
			envelope: map[string]blueprint.Field{"providerConfigRef.name": {Value: "dedicated-aws-pc"}},
			wantLine: `    name: 'dedicated-aws-pc'`,
		},
		{
			name:     "providerConfigRef.kind overridden to namespaced ProviderConfig",
			envelope: map[string]blueprint.Field{"providerConfigRef.kind": {Value: "ProviderConfig"}},
			wantLine: `    kind: 'ProviderConfig'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := envelopeTestBlueprint()
			for name, p := range tc.params {
				b.Spec.XRD.Parameters[name] = p
			}
			b.Spec.Resources[0].Envelope = tc.envelope
			got, err := Composition(b, envelopeTestCRDs(t))
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("Composition accepted %v", tc.envelope)
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("err = %v, want it to contain %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Composition: %v", err)
			}
			if !strings.Contains(extractTemplate(t, got), tc.wantLine+"\n") {
				t.Errorf("template missing line %q\n---\n%s", tc.wantLine, extractTemplate(t, got))
			}
		})
	}
}

// A top-level envelope key the spec schema requires must not be omittable:
// all-optional wiring would omit it for an XR that sets none of the
// parameters, generating an artifact that can never apply for a valid XR.
func TestRequiredEnvelopeKeyRefusesAllOptionalWiring(t *testing.T) {
	crds := envelopeTestCRDs(t)
	// Make writeConnectionSecretToRef required in the spec schema.
	v := &crds[0].Versions[0]
	spec := v.Properties["spec"].(map[string]any)
	spec["required"] = []any{"forProvider", "writeConnectionSecretToRef"}

	b := envelopeTestBlueprint()
	b.Spec.XRD.Parameters["secretName"] = blueprint.Parameter{Type: "string"} // optional
	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "required by the kind's spec schema") {
		t.Fatalf("err = %v, want the required-key-cannot-be-all-optional refusal", err)
	}
}

// Determinism: the same envelope-carrying blueprint yields byte-identical
// output, twice.
func TestEnvelopeEmitIsDeterministic(t *testing.T) {
	first, err := Composition(envelopeTestBlueprint(), envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(envelopeTestBlueprint(), envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}

// TestMultipleResourcesDifferentProviderConfigs tests that in the same composition,
// one resource can use the shared default providerName while another specifies a custom providerConfigRef.
func TestMultipleResourcesDifferentProviderConfigs(t *testing.T) {
	b := envelopeTestBlueprint()
	b.Spec.XRD.Parameters["backupProviderName"] = blueprint.Parameter{Type: "string", Required: true}
	b.Spec.Resources = []blueprint.Resource{
		{
			Name:   "primary-queue",
			Kind:   "Queue",
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
			// Uses default providerConfigRef (falls back to providerName)
		},
		{
			Name:   "backup-queue",
			Kind:   "Queue",
			Fields: map[string]blueprint.Field{"region": {Value: "us-east-1"}},
			Envelope: map[string]blueprint.Field{
				"providerConfigRef.name": {From: "params.backupProviderName"},
			},
		},
		{
			Name:   "static-queue",
			Kind:   "Queue",
			Fields: map[string]blueprint.Field{"region": {Value: "ap-southeast-1"}},
			Envelope: map[string]blueprint.Field{
				"providerConfigRef.name": {Value: "dedicated-infra-pc"},
				"providerConfigRef.kind": {Value: "ProviderConfig"},
			},
		},
	}

	got, err := Composition(b, envelopeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, got)

	// primary-queue should have default providerName and ClusterProviderConfig
	if !strings.Contains(tmpl, "name: {{ $spec.providerName }}") {
		t.Errorf("template missing default providerName:\n%s", tmpl)
	}
	// backup-queue should have backupProviderName
	if !strings.Contains(tmpl, "name: {{ $spec.backupProviderName | quote }}") {
		t.Errorf("template missing backupProviderName wire:\n%s", tmpl)
	}
	// static-queue should have dedicated-infra-pc and ProviderConfig
	if !strings.Contains(tmpl, "name: 'dedicated-infra-pc'") {
		t.Errorf("template missing static providerConfigRef name:\n%s", tmpl)
	}
	if !strings.Contains(tmpl, "kind: 'ProviderConfig'") {
		t.Errorf("template missing custom providerConfigRef kind:\n%s", tmpl)
	}
}
