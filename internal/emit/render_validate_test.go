package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

var testCRDList = []schema.CRD{
	{
		Group:      "sqs.aws.m.upbound.io",
		Kind:       "Queue",
		Plural:     "queues",
		Scope:      "Namespaced",
		Categories: []string{"crossplane", "managed"},
		Versions: []schema.Version{
			{
				Name:    "v1beta1",
				Served:  true,
				Storage: true,
				Properties: map[string]any{
					"spec": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"deletionPolicy": map[string]any{"type": "string", "enum": []any{"Orphan", "Delete"}},
							"providerConfigRef": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
									"kind": map[string]any{"type": "string"},
								},
							},
							"forProvider": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"region":                   map[string]any{"type": "string"},
									"visibilityTimeoutSeconds": map[string]any{"type": "integer"},
									"fifoQueue":                map[string]any{"type": "boolean"},
									"tags": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "string",
										},
									},
									"redrivePolicy": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"deadLetterTargetArn": map[string]any{"type": "string"},
											"maxReceiveCount":     map[string]any{"type": "integer"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

func testCRDsWithNative(t *testing.T) []schema.CRD {
	t.Helper()
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds(): %v", err)
	}
	all := make([]schema.CRD, len(testCRDList)+len(native))
	copy(all, testCRDList)
	copy(all[len(testCRDList):], native)
	return all
}

func TestValidateRenderedValidStream(t *testing.T) {
	crds := testCRDsWithNative(t)
	stream := `---
apiVersion: platform.sparky.ee/v1alpha1
kind: XQueue
metadata:
  name: render-check
spec:
  providerName: sample
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
    visibilityTimeoutSeconds: 45
    fifoQueue: true
    tags:
      env: dev
    redrivePolicy:
      deadLetterTargetArn: arn:aws:sqs:eu-north-1:123456789012:dlq
      maxReceiveCount: 5
`
	if err := ValidateRendered([]byte(stream), crds); err != nil {
		t.Fatalf("ValidateRendered failed on valid stream: %v", err)
	}
}

func TestValidateRenderedUnknownFieldWithSuggestion(t *testing.T) {
	crds := testCRDsWithNative(t)
	stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    region: eu-north-1
    visibiltyTimeoutSeconds: 45
`
	err := ValidateRendered([]byte(stream), crds)
	if err == nil {
		t.Fatal("expected error for typo'd field visibiltyTimeoutSeconds, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "visibiltyTimeoutSeconds") {
		t.Errorf("error %q should mention visibiltyTimeoutSeconds", msg)
	}
	if !strings.Contains(msg, "visibilityTimeoutSeconds") {
		t.Errorf("error %q should suggest visibilityTimeoutSeconds", msg)
	}
	if !strings.Contains(msg, "line 10") {
		t.Errorf("error %q should report line 10", msg)
	}
	if !strings.Contains(msg, `resource "main-queue"`) {
		t.Errorf("error %q should report resource main-queue", msg)
	}
}

func TestValidateRenderedTypeMismatches(t *testing.T) {
	crds := testCRDsWithNative(t)

	tests := []struct {
		name    string
		stream  string
		wantErr []string
	}{
		{
			name: "string instead of integer",
			stream: `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    region: eu-north-1
    visibilityTimeoutSeconds: "45"
`,
			wantErr: []string{"line 10", `spec.forProvider.visibilityTimeoutSeconds`, "invalid type", "expected integer", "got string"},
		},
		{
			name: "integer instead of string",
			stream: `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    region: 12345
`,
			wantErr: []string{"line 9", `spec.forProvider.region`, "invalid type", "expected string", "got integer"},
		},
		{
			name: "string instead of boolean",
			stream: `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    region: eu-north-1
    fifoQueue: "true"
`,
			wantErr: []string{"line 10", `spec.forProvider.fifoQueue`, "invalid type", "expected boolean", "got string"},
		},
		{
			name: "scalar instead of object in redrivePolicy",
			stream: `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    region: eu-north-1
    redrivePolicy: "disabled"
`,
			wantErr: []string{"line 10", `spec.forProvider.redrivePolicy`, "invalid type", "expected object"},
		},
		{
			name: "invalid enum on deletionPolicy",
			stream: `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  deletionPolicy: Destroy
  forProvider:
    region: eu-north-1
`,
			wantErr: []string{"line 8", `spec.deletionPolicy`, "invalid value", "supported values", "Orphan", "Delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRendered([]byte(tt.stream), crds)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			msg := err.Error()
			for _, w := range tt.wantErr {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q missing expected substring %q", msg, w)
				}
			}
		})
	}
}

func TestValidateRenderedNativeKind(t *testing.T) {
	crds := testCRDsWithNative(t)

	validDeployment := `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: default
  annotations:
    crossplane.io/composition-resource-name: web-deploy
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:latest
`
	if err := ValidateRendered([]byte(validDeployment), crds); err != nil {
		t.Fatalf("valid Deployment failed: %v", err)
	}

	invalidDeployment := `---
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    crossplane.io/composition-resource-name: web-deploy
spec:
  replicas: "three"
  templete:
    spec:
      containers:
      - name: web
        image: 123
`
	err := ValidateRendered([]byte(invalidDeployment), crds)
	if err == nil {
		t.Fatal("expected errors on invalid deployment, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "replicas") || !strings.Contains(msg, "expected integer") {
		t.Errorf("error %q should report replicas type error", msg)
	}
	if !strings.Contains(msg, "templete") || !strings.Contains(msg, "template") {
		t.Errorf("error %q should report templete typo and suggest template", msg)
	}
}

func TestValidateRenderedMissingCRDSchema(t *testing.T) {
	crds := testCRDsWithNative(t)
	stream := `---
apiVersion: unknown.provider.io/v1alpha1
kind: UnknownThing
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-unknown
spec:
  forProvider:
    foo: bar
`
	err := ValidateRendered([]byte(stream), crds)
	if err == nil {
		t.Fatal("expected error for unknown CRD, got nil")
	}
	if !strings.Contains(err.Error(), "no matching CRD schema found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidateRenderedNativeServiceIntOrStringTargetPort(t *testing.T) {
	crds := testCRDsWithNative(t)

	// Integer targetPort must be valid for IntOrString
	intService := `---
apiVersion: v1
kind: Service
metadata:
  name: my-svc
  annotations:
    crossplane.io/composition-resource-name: my-svc
spec:
  ports:
  - port: 80
    targetPort: 8080
`
	if err := ValidateRendered([]byte(intService), crds); err != nil {
		t.Fatalf("ValidateRendered with integer targetPort failed: %v", err)
	}

	// Named/string targetPort must also be valid for IntOrString
	strService := `---
apiVersion: v1
kind: Service
metadata:
  name: my-svc
  annotations:
    crossplane.io/composition-resource-name: my-svc
spec:
  ports:
  - port: 80
    targetPort: http
`
	if err := ValidateRendered([]byte(strService), crds); err != nil {
		t.Fatalf("ValidateRendered with string targetPort failed: %v", err)
	}

	// Non-scalar (e.g. boolean or map) must still be rejected
	invalidService := `---
apiVersion: v1
kind: Service
metadata:
  name: my-svc
  annotations:
    crossplane.io/composition-resource-name: my-svc
spec:
  ports:
  - port: 80
    targetPort: true
`
	if err := ValidateRendered([]byte(invalidService), crds); err == nil {
		t.Fatal("expected error for boolean targetPort, got nil")
	}
}

func TestValidateRenderedOptionalParamHint(t *testing.T) {
	crdWithRequired := []schema.CRD{
		{
			Group:      "sqs.aws.m.upbound.io",
			Kind:       "Queue",
			Plural:     "queues",
			Scope:      "Namespaced",
			Categories: []string{"crossplane", "managed"},
			Versions: []schema.Version{
				{
					Name:    "v1beta1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type":     "object",
									"required": []any{"region"},
									"properties": map[string]any{
										"region": map[string]any{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-bp
spec:
  xrd:
    group: example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      region:
        type: string
        required: false
  resources:
    - name: dead-letter
      kind: Queue
      fields:
        region:
          from: params.region
`
	bp, err := blueprint.Parse([]byte(bpYAML))
	if err != nil {
		t.Fatalf("blueprint.Parse: %v", err)
	}

	// Rendered stream where dead-letter has omitted region because params.region is optional
	stream := `---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: dead-letter
spec:
  forProvider: {}
`
	err = ValidateRenderedWithBlueprint([]byte(stream), crdWithRequired, bp)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	expectedSubstr := `missing required field "spec.forProvider.region" in Queue spec.forProvider (fed by optional parameter params.region; mark parameter required in the XRD or provide a default)`
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Fatalf("error %q does not contain expected substring %q", err.Error(), expectedSubstr)
	}
}

func TestValidateRenderedObjectWithPropertiesAndAdditionalProperties(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:  "example.org",
			Kind:   "ExtensibleConfig",
			Plural: "extensibleconfigs",
			Scope:  "Namespaced",
			Versions: []schema.Version{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type":     "object",
									"required": []any{"name"},
									"properties": map[string]any{
										"name": map[string]any{"type": "string"},
										"port": map[string]any{"type": "integer"},
									},
									"additionalProperties": true,
								},
							},
						},
					},
				},
			},
		},
	}

	stream := `---
apiVersion: example.org/v1alpha1
kind: ExtensibleConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-cfg
spec:
  forProvider:
    port: "not-an-int"
`
	err := ValidateRendered([]byte(stream), crds)
	if err == nil {
		t.Fatal("expected validation error for invalid port type and missing required name, got nil")
	}
}

func TestValidateRenderedObjectWithPropertiesAndAdditionalPropertiesMap(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:  "example.org",
			Kind:   "ExtensibleConfig",
			Plural: "extensibleconfigs",
			Scope:  "Namespaced",
			Versions: []schema.Version{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"port": map[string]any{"type": "integer"},
									},
									"additionalProperties": map[string]any{
										"type": "string",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// 1. Valid: port is int, undeclared extra is string
	validStream := `---
apiVersion: example.org/v1alpha1
kind: ExtensibleConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-cfg
spec:
  forProvider:
    port: 8080
    extra: "custom-value"
`
	if err := ValidateRendered([]byte(validStream), crds); err != nil {
		t.Fatalf("expected valid stream to pass, got: %v", err)
	}

	// 2. Invalid declared property: port is string (which would match additionalProperties, but must be validated against port schema)
	invalidPortStream := `---
apiVersion: example.org/v1alpha1
kind: ExtensibleConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-cfg
spec:
  forProvider:
    port: "8080"
`
	if err := ValidateRendered([]byte(invalidPortStream), crds); err == nil {
		t.Fatal("expected error for port being string instead of integer, got nil")
	}

	// 3. Invalid undeclared property: extra is integer, should fail additionalProperties string schema
	invalidExtraStream := `---
apiVersion: example.org/v1alpha1
kind: ExtensibleConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-cfg
spec:
  forProvider:
    port: 8080
    extra: 12345
`
	if err := ValidateRendered([]byte(invalidExtraStream), crds); err == nil {
		t.Fatal("expected error for extra being integer instead of string, got nil")
	}
}

func TestValidateRenderedObjectAdditionalPropertiesFalse(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:  "example.org",
			Kind:   "StrictConfig",
			Plural: "strictconfigs",
			Scope:  "Namespaced",
			Versions: []schema.Version{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": map[string]any{"type": "string"},
									},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
	}

	validStream := `---
apiVersion: example.org/v1alpha1
kind: StrictConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: strict-cfg
spec:
  forProvider:
    name: "valid"
`
	if err := ValidateRendered([]byte(validStream), crds); err != nil {
		t.Fatalf("expected valid stream to pass, got: %v", err)
	}

	invalidStream := `---
apiVersion: example.org/v1alpha1
kind: StrictConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: strict-cfg
spec:
  forProvider:
    name: "valid"
    unexpected: "rejected"
`
	if err := ValidateRendered([]byte(invalidStream), crds); err == nil {
		t.Fatal("expected error for unexpected field when additionalProperties: false, got nil")
	}
}

func TestValidateRenderedObjectPropertylessAdditionalPropertiesFalse(t *testing.T) {
	crds := []schema.CRD{
		{
			Group:  "example.org",
			Kind:   "EmptyConfig",
			Plural: "emptyconfigs",
			Scope:  "Namespaced",
			Versions: []schema.Version{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Properties: map[string]any{
						"spec": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"forProvider": map[string]any{
									"type":                 "object",
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
		},
	}

	stream := `---
apiVersion: example.org/v1alpha1
kind: EmptyConfig
metadata:
  annotations:
    crossplane.io/composition-resource-name: empty-cfg
spec:
  forProvider:
    anyField: "value"
`
	if err := ValidateRendered([]byte(stream), crds); err == nil {
		t.Fatal("expected error for any field when properties is empty and additionalProperties: false, got nil")
	}
}
