package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// whenTestBlueprint has three resources: an unconditional main-queue, an
// audit-queue gated on a string comparison, and a replica-queue that is BOTH
// looped and gated on a boolean — the composition case, proving the when
// wraps outside the range.
func whenTestBlueprint() *blueprint.Blueprint {
	b := testBlueprint()
	b.Spec.XRD.Parameters["tier"] = blueprint.Parameter{
		Type: "string", Default: "standard", Enum: []string{"standard", "pro"},
	}
	b.Spec.XRD.Parameters["replicasEnabled"] = blueprint.Parameter{Type: "boolean", Default: "true"}
	b.Spec.XRD.Parameters["instanceCount"] = blueprint.Parameter{Type: "integer", Default: "2"}
	b.Spec.Resources = append(b.Spec.Resources,
		blueprint.Resource{
			Name: "audit-queue", Kind: "Queue",
			When:   `params.tier == "pro"`,
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
		blueprint.Resource{
			Name: "replica-queue", Kind: "Queue",
			When:    "params.replicasEnabled",
			ForEach: "params.instanceCount",
			Fields:  map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
	)
	return b
}

// TestWhenGoldenTemplate pins the emitted template body byte-for-byte: the
// compiled conditions, and — the load-bearing part — the when wrapping
// OUTSIDE the forEach range, so a false condition skips every iteration
// (and the loop bound's own dereference) in one test.
func TestWhenGoldenTemplate(t *testing.T) {
	got, err := Composition(whenTestBlueprint(), testCRDs(t))
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
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
{{- if eq $spec.tier "pro" }}
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    {{ setResourceNameAnnotation "audit-queue" }}
spec:
  forProvider:
    region: 'eu-north-1'
  providerConfigRef:
    kind: ClusterProviderConfig
    name: {{ $spec.providerName }}
{{- end }}
{{- if $spec.replicasEnabled }}
{{- range $i := until (int $spec.instanceCount) }}
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

// TestWhenRendersBothWays executes the emitted template and proves the
// conditional on the rendered ARTIFACT: the gated document appears exactly
// when its condition holds, the unconditional resource always appears, and a
// gated forEach fans out to all-or-nothing.
func TestWhenRendersBothWays(t *testing.T) {
	got, err := Composition(whenTestBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, got)

	cases := []struct {
		name string
		spec map[string]any
		want []string
	}{
		{"tier pro, replicas enabled: everything renders",
			map[string]any{"providerName": "p", "tier": "pro", "replicasEnabled": true, "instanceCount": float64(2)},
			[]string{"main-queue", "audit-queue", "replica-queue-0", "replica-queue-1"}},
		{"tier standard: the == guard skips the audit queue",
			map[string]any{"providerName": "p", "tier": "standard", "replicasEnabled": true, "instanceCount": float64(2)},
			[]string{"main-queue", "replica-queue-0", "replica-queue-1"}},
		{"replicas disabled: the bare boolean guard skips every loop iteration",
			map[string]any{"providerName": "p", "tier": "standard", "replicasEnabled": false, "instanceCount": float64(2)},
			[]string{"main-queue"}},
		{"both off: only the unconditional resource",
			map[string]any{"providerName": "p", "tier": "standard", "replicasEnabled": false, "instanceCount": float64(0)},
			[]string{"main-queue"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderTemplate(t, tmplBody, tc.spec)
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

	// The negative half of Validate's required-or-default rule, on the real
	// template semantics: with the condition's parameter genuinely absent the
	// render hard-fails under missingkey=error rather than silently deciding
	// the condition either way. The API server's schema defaulting makes this
	// state unreachable for a defaulted parameter — which is exactly why
	// Validate refuses a when over a parameter that is neither required nor
	// defaulted.
	t.Run("absent condition parameter hard-fails the render", func(t *testing.T) {
		_, err := renderTemplate(t, tmplBody, map[string]any{
			"providerName": "p", "replicasEnabled": true, "instanceCount": float64(2),
			// tier intentionally absent
		})
		if err == nil {
			t.Fatal("render succeeded with the when parameter absent; missingkey=error must make this a hard failure")
		}
	})
}

// Determinism: the same blueprint yields byte-identical output, twice.
func TestWhenEmitIsDeterministic(t *testing.T) {
	first, err := Composition(whenTestBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	second, err := Composition(whenTestBlueprint(), testCRDs(t))
	if err != nil {
		t.Fatalf("Composition (second run): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two runs over the same blueprint produced different bytes")
	}
}
