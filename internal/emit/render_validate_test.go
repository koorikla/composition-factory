package emit

import (
	"strings"
	"testing"

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
