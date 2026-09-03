package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestRawGoTemplateBareExpressionAutoWrapping(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"region":       {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "main-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					Fields: map[string]blueprint.Field{
						"region": {Raw: `printf "%s-subnet-%d" $xr $i`},
					},
					Annotations: map[string]blueprint.Field{
						"example.org/raw-var": {Raw: `$spec.region`},
					},
					Envelope: map[string]blueprint.Field{
						"providerConfigRef.name": {Raw: `$spec.providerName`},
					},
				},
			},
		},
	}

	crdDoc := []byte(`
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
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(bp, crds)
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(compBytes)

	// Verify bare raw expressions got auto-wrapped into Go-template actions {{ ... }}
	if !strings.Contains(s, `region: {{ printf "%s-subnet-%d" $xr $i }}`) {
		t.Errorf("expected auto-wrapped field region, got:\n%s", s)
	}
	if !strings.Contains(s, `'example.org/raw-var': {{ $spec.region }}`) {
		t.Errorf("expected auto-wrapped annotation, got:\n%s", s)
	}
	if !strings.Contains(s, `name: {{ $spec.providerName }}`) {
		t.Errorf("expected auto-wrapped envelope field, got:\n%s", s)
	}
}

func TestRawBareExpressionRejectedOnKCLAndPython(t *testing.T) {
	engines := []string{"kcl", "python"}

	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			bp := &blueprint.Blueprint{
				APIVersion: blueprint.APIVersion,
				Kind:       blueprint.Kind,
				Metadata:   blueprint.Metadata{Name: "xapp"},
				Spec: blueprint.Spec{
					Emit: &blueprint.Emit{Engine: engine},
					Sources: []blueprint.Source{
						{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
					},
					XRD: blueprint.XRD{
						Group:   "platform.example.org",
						Kind:    "XApp",
						Plural:  "xapps",
						Version: "v1alpha1",
						Scope:   "Namespaced",
						Parameters: map[string]blueprint.Parameter{
							"providerName": {Type: "string", Required: true},
						},
					},
					Resources: []blueprint.Resource{
						{
							Name:     "main-queue",
							Kind:     "Queue",
							Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
							Fields: map[string]blueprint.Field{
								"region": {Raw: `printf "%s-subnet-%d" $xr $i`},
							},
						},
					},
				},
			}

			crdDoc := []byte(`
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
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
			crds, _ := schema.ParseCRDs([][]byte{crdDoc})
			_, err := Composition(bp, crds)
			if err == nil {
				t.Fatalf("expected error on engine %q with bare Go-template in raw field, got nil", engine)
			}
			if !strings.Contains(err.Error(), "Go-template syntax") || !strings.Contains(err.Error(), "go-templating engine") {
				t.Errorf("unexpected error on engine %q: %v", engine, err)
			}
		})
	}
}
