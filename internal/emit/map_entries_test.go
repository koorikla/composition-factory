package emit_test

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestMapEntryBracketFields(t *testing.T) {
	crdYAML := []byte(`
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
                properties:
                  tags:
                    type: object
                    additionalProperties:
                      type: string
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdYAML})
	if err != nil {
		t.Fatalf("ParseCRDs failed: %v", err)
	}

	b := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-map"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"env":          {Type: "string", Required: false},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"tags[Team]":        {Value: "infrastructure"},
						"tags[Environment]": {From: "params.env"},
					},
				},
			},
		},
	}

	if err := b.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	out, err := emit.Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}

	s := string(out)
	t.Logf("Emitted Composition:\n%s", s)

	// Verify tags block contains Environment and Team
	if !strings.Contains(s, "tags:") {
		t.Errorf("expected tags block, got:\n%s", s)
	}
	if !strings.Contains(s, "Team: 'infrastructure'") {
		t.Errorf("expected Team tag, got:\n%s", s)
	}
	if !strings.Contains(s, "{{- if hasKey $spec \"env\" }}") {
		t.Errorf("expected conditional hasKey for env, got:\n%s", s)
	}
	if !strings.Contains(s, "Environment: {{ $spec.env | quote }}") {
		t.Errorf("expected Environment tag from param, got:\n%s", s)
	}
}

func TestMapEntryAllConditionalFallback(t *testing.T) {
	crdYAML := []byte(`
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
                properties:
                  tags:
                    type: object
                    additionalProperties:
                      type: string
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdYAML})
	if err != nil {
		t.Fatalf("ParseCRDs failed: %v", err)
	}

	b := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-map-cond"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"env":          {Type: "string", Required: false},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"tags[Environment]": {From: "params.env"},
					},
				},
			},
		},
	}

	out, err := emit.Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}

	s := string(out)
	t.Logf("Emitted Composition:\n%s", s)

	if !strings.Contains(s, "{{- if or (hasKey $spec \"env\") }}") {
		t.Errorf("expected outer conditional wrapper for tags, got:\n%s", s)
	}
	if !strings.Contains(s, "tags: {}") {
		t.Errorf("expected tags: {} fallback, got:\n%s", s)
	}
}
