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

func TestObjectParamIntoMapLeafWithExplicitMerging(t *testing.T) {
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
		Metadata:   blueprint.Metadata{Name: "test-map-obj"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"customTags": {
						Type: "object",
						Properties: map[string]blueprint.Parameter{
							"project": {Type: "string"},
							"owner":   {Type: "string"},
						},
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"tags":        {From: "params.customTags"},
						"tags[env]":   {Value: "production"},
						"tags[owner]": {Value: "team-platform"},
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

	// Explicit tags[owner] overrides object property; tags[env] is merged; tags[project] comes from customTags
	if !strings.Contains(s, "env: 'production'") {
		t.Errorf("expected explicit env tag, got:\n%s", s)
	}
	if !strings.Contains(s, "owner: 'team-platform'") {
		t.Errorf("expected explicit owner override tag, got:\n%s", s)
	}
	if !strings.Contains(s, "{{ $spec.customTags.project | quote }}") {
		t.Errorf("expected wired project tag from customTags, got:\n%s", s)
	}
}

func TestUntypedObjectParam(t *testing.T) {
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
                  nested:
                    type: object
                    properties:
                      enabled:
                        type: boolean
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdYAML})
	if err != nil {
		t.Fatalf("ParseCRDs failed: %v", err)
	}

	t.Run("NestedFieldsExercisesNativeTree", func(t *testing.T) {
		b := &blueprint.Blueprint{
			APIVersion: blueprint.APIVersion,
			Kind:       blueprint.Kind,
			Metadata:   blueprint.Metadata{Name: "test-untyped-obj-nested"},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "example.org",
					Version: "v1alpha1",
					Kind:    "XApp",
					Plural:  "xapps",
					Scope:   "Namespaced",
					Parameters: map[string]blueprint.Parameter{
						"providerName": {Type: "string", Required: true},
						"customTags":   {Type: "object"},
					},
				},
				Resources: []blueprint.Resource{
					{
						Name: "queue",
						Kind: "Queue",
						Fields: map[string]blueprint.Field{
							"nested.enabled": {Value: "true"},
							"tags":           {From: "params.customTags"},
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
		t.Logf("Emitted Composition (nested):\n%s", s)

		if !strings.Contains(s, "enabled: true") {
			t.Errorf("expected nested enabled field, got:\n%s", s)
		}
		if !strings.Contains(s, "{{- if hasKey $spec \"customTags\" }}") {
			t.Errorf("expected conditional hasKey for customTags, got:\n%s", s)
		}
		if !strings.Contains(s, "tags:") {
			t.Errorf("expected tags block, got:\n%s", s)
		}
		if !strings.Contains(s, "{{- range $k, $v := $spec.customTags }}") {
			t.Errorf("expected range loop for customTags, got:\n%s", s)
		}
		if !strings.Contains(s, "{{ $k }}: {{ $v }}") {
			t.Errorf("expected key-value output inside range loop, got:\n%s", s)
		}

		// Verify ordering: nested enabled -> if hasKey -> tags: -> range -> key:value -> end
		nestedIdx := strings.Index(s, "enabled: true")
		ifIdx := strings.Index(s, "{{- if hasKey $spec \"customTags\" }}")
		tagsIdx := strings.Index(s, "tags:")
		rangeIdx := strings.Index(s, "{{- range $k, $v := $spec.customTags }}")
		kvIdx := strings.Index(s, "{{ $k }}: {{ $v }}")

		if !(nestedIdx < ifIdx && ifIdx < tagsIdx && tagsIdx < rangeIdx && rangeIdx < kvIdx) {
			t.Errorf("expected order: enabled -> if -> tags -> range -> kv, got indices: nested=%d, if=%d, tags=%d, range=%d, kv=%d",
				nestedIdx, ifIdx, tagsIdx, rangeIdx, kvIdx)
		}
	})

	t.Run("WithExplicitKeys", func(t *testing.T) {
		b := &blueprint.Blueprint{
			APIVersion: blueprint.APIVersion,
			Kind:       blueprint.Kind,
			Metadata:   blueprint.Metadata{Name: "test-untyped-obj-explicit"},
			Spec: blueprint.Spec{
				XRD: blueprint.XRD{
					Group:   "example.org",
					Version: "v1alpha1",
					Kind:    "XApp",
					Plural:  "xapps",
					Scope:   "Namespaced",
					Parameters: map[string]blueprint.Parameter{
						"providerName": {Type: "string", Required: true},
						"customTags":   {Type: "object"},
					},
				},
				Resources: []blueprint.Resource{
					{
						Name: "queue",
						Kind: "Queue",
						Fields: map[string]blueprint.Field{
							"tags":              {From: "params.customTags"},
							"tags[Environment]": {Value: "prod"},
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
		t.Logf("Emitted Composition (explicit):\n%s", s)

		if count := strings.Count(s, "tags:"); count != 1 {
			t.Errorf("expected exactly 1 'tags:' occurrence, got %d:\n%s", count, s)
		}
		if !strings.Contains(s, "{{- range $k, $v := $spec.customTags }}") {
			t.Errorf("expected range loop for customTags, got:\n%s", s)
		}
		if !strings.Contains(s, "Environment: 'prod'") {
			t.Errorf("expected explicit Environment tag, got:\n%s", s)
		}

		rangeIdx := strings.Index(s, "range $k, $v := $spec.customTags")
		explicitIdx := strings.Index(s, "Environment: 'prod'")
		if rangeIdx == -1 || explicitIdx == -1 || rangeIdx > explicitIdx {
			t.Errorf("expected dynamic range loop before explicit tag, got rangeIdx=%d, explicitIdx=%d", rangeIdx, explicitIdx)
		}
	})
}
