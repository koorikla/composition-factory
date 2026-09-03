package adopt

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/testfixture"
)

func testCRDs(t *testing.T) []schema.CRD {
	return testfixture.QueueBothCRDs(t)
}

func TestAdoptEnvironment_WithAnnotation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.platform.sparky.ee
  annotations:
    factory.crossplane.io/environment-keys: '{"clusterCount":{"type":"integer","default":3},"region":{"type":"string","description":"target region","default":"us-east-1"}}'
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: environment-configs
      functionRef:
        name: function-environment-configs
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            {{- $xrMeta := .observed.composite.resource.metadata -}}
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: {{ $xr }}-main-queue
              annotations:
                {{- if hasKey $env "region" }}
                example.org/region: {{ $env.region | quote }}
                {{- end }}
            spec:
              forProvider:
                {{- if hasKey $env "region" }}
                region: {{ $env.region | quote }}
                {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("expected no true loss, got drops: %+v", report.Drops)
	}

	if len(bp.Spec.Environment) != 2 {
		t.Fatalf("expected 2 environment keys, got %d", len(bp.Spec.Environment))
	}
	if bp.Spec.Environment["region"].Type != "string" || bp.Spec.Environment["region"].Default != "us-east-1" {
		t.Errorf("expected region string with default us-east-1, got %+v", bp.Spec.Environment["region"])
	}
	if bp.Spec.Environment["clusterCount"].Type != "integer" || bp.Spec.Environment["clusterCount"].Default != "3" {
		t.Errorf("expected clusterCount integer with default 3, got %+v", bp.Spec.Environment["clusterCount"])
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Fields["region"].From != "env.region" {
		t.Errorf("expected field region from: env.region, got %q", r.Fields["region"].From)
	}
	if r.Annotations["example.org/region"].From != "env.region" {
		t.Errorf("expected annotation example.org/region from: env.region, got %q", r.Annotations["example.org/region"].From)
	}
}

func TestAdoptEnvironment_FallbackInference(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            {{- $xrMeta := .observed.composite.resource.metadata -}}
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: {{ $xr }}-main-queue
            spec:
              forProvider:
                {{- if hasKey $env "inferredRegion" }}
                region: {{ $env.inferredRegion | quote }}
                {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("expected no true loss, got drops: %+v", report.Drops)
	}

	if len(bp.Spec.Environment) != 1 {
		t.Fatalf("expected 1 inferred environment key, got %d", len(bp.Spec.Environment))
	}
	if bp.Spec.Environment["inferredRegion"].Type != "string" {
		t.Errorf("expected inferredRegion type string, got %+v", bp.Spec.Environment["inferredRegion"])
	}
}

func TestEnvironment_RoundTrip(t *testing.T) {
	bpOriginal := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xqueues.platform.sparky.ee"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.sparky.ee",
				Kind:    "XQueue",
				Plural:  "xqueues",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"region": {Type: "string", Default: "us-east-1", Description: "target region"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "main-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					Fields: map[string]blueprint.Field{
						"region": {From: "env.region"},
					},
					Annotations: map[string]blueprint.Field{
						"example.org/env-region": {From: "env.region"},
					},
				},
			},
		},
	}

	crds := testCRDs(t)
	compGen1, err := emit.Composition(bpOriginal, crds)
	if err != nil {
		t.Fatalf("First emit.Composition failed: %v", err)
	}

	bpAdopted, report, err := Adopt(compGen1, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("Adopt had unexpected true loss: %+v", report.Drops)
	}

	compGen2, err := emit.Composition(bpAdopted, crds)
	if err != nil {
		t.Fatalf("Second emit.Composition failed: %v", err)
	}

	if diff := cmp.Diff(string(compGen1), string(compGen2)); diff != "" {
		t.Errorf("Round-trip emitted Composition diff (-gen1 +gen2):\n%s", diff)
	}
}
