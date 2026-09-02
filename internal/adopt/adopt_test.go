package adopt

import (
	"strings"
	"testing"
)

func TestAdoptGoTemplatingComposition(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
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
            {{- define "cf.tags" }}
            tags:
              ManagedBy: Crossplane
            {{- end }}
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
              annotations:
                iam.amazonaws.com/role: {{ $spec.roleArn }}
            spec:
              forProvider:
                region: {{ $spec.region }}
                maxMessageSize: 262144
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if bp.Metadata.Name != "xqueues.aws.example.org" {
		t.Errorf("Metadata.Name = %q, want xqueues.aws.example.org", bp.Metadata.Name)
	}
	if bp.Spec.XRD.Kind != "XQueue" {
		t.Errorf("XRD.Kind = %q, want XQueue", bp.Spec.XRD.Kind)
	}
	if bp.Spec.XRD.Group != "example.org" || bp.Spec.XRD.Version != "v1alpha1" {
		t.Errorf("XRD group/version = %s/%s, want example.org/v1alpha1", bp.Spec.XRD.Group, bp.Spec.XRD.Version)
	}

	// Parameters discovered from template
	if _, ok := bp.Spec.XRD.Parameters["region"]; !ok {
		t.Errorf("expected parameter 'region' in XRD parameters")
	}
	if _, ok := bp.Spec.XRD.Parameters["roleArn"]; !ok {
		t.Errorf("expected parameter 'roleArn' in XRD parameters")
	}

	// Templates (defines)
	if _, ok := bp.Spec.Templates["cf.tags"]; !ok {
		t.Errorf("expected template 'cf.tags' in Spec.Templates")
	}

	// Pipeline
	if len(bp.Spec.Pipeline) != 1 || bp.Spec.Pipeline[0].Name != "auto-ready" {
		t.Errorf("Pipeline = %+v, want [auto-ready]", bp.Spec.Pipeline)
	}

	// Resources
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Name != "main-queue" || r.Kind != "Queue" {
		t.Errorf("Resource = %s (%s), want main-queue (Queue)", r.Name, r.Kind)
	}
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
	if r.Annotations["iam.amazonaws.com/role"].From != "params.roleArn" {
		t.Errorf("role annotation = %+v, want From: params.roleArn", r.Annotations["iam.amazonaws.com/role"])
	}
}

func TestAdoptClassicComposition(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-queues
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XRQueue
  resources:
    - name: sqs-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.queueName
          toFieldPath: spec.forProvider.name
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.region
          toFieldPath: spec.forProvider.region
`

	bp, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if bp.Spec.XRD.Kind != "XRQueue" {
		t.Errorf("XRD.Kind = %q, want XRQueue", bp.Spec.XRD.Kind)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Name != "sqs-queue" || r.Kind != "Queue" {
		t.Errorf("Resource = %s (%s), want sqs-queue (Queue)", r.Name, r.Kind)
	}
	if r.Fields["name"].From != "params.queueName" {
		t.Errorf("name field = %+v, want From: params.queueName", r.Fields["name"])
	}
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
}

func TestAdoptMultiDocumentXRDAndComposition(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xdatabases.example.org
spec:
  group: example.org
  names:
    kind: XDatabase
    plural: xdatabases
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
                parameters:
                  type: object
                  required:
                    - dbName
                  properties:
                    dbName:
                      type: string
                      description: Database name
                    storageGB:
                      type: integer
                      default: 20
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xdatabase-composition
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XDatabase
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
            apiVersion: rds.aws.upbound.io/v1beta1
            kind: Instance
            metadata:
              name: db-instance
            spec:
              forProvider:
                allocatedStorage: {{ $spec.storageGB }}
                dbName: {{ $spec.dbName }}
`

	bp, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if bp.Spec.XRD.Kind != "XDatabase" || bp.Spec.XRD.Group != "example.org" {
		t.Errorf("XRD = %s.%s, want XDatabase.example.org", bp.Spec.XRD.Kind, bp.Spec.XRD.Group)
	}
	dbNameP := bp.Spec.XRD.Parameters["dbName"]
	if !dbNameP.Required || dbNameP.Description != "Database name" {
		t.Errorf("dbName parameter = %+v, want required=true, description='Database name'", dbNameP)
	}
	storageP := bp.Spec.XRD.Parameters["storageGB"]
	if storageP.Type != "integer" || storageP.Default != "20" {
		t.Errorf("storageGB parameter = %+v, want type=integer, default='20'", storageP)
	}
}

func TestAdoptCrossResourceStatus(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: irsa-demo
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
            apiVersion: iam.aws.upbound.io/v1beta1
            kind: Role
            metadata:
              name: app-role
            spec:
              forProvider:
                assumeRolePolicy: {}
            ---
            apiVersion: v1
            kind: ServiceAccount
            metadata:
              name: app-sa
              annotations:
                eks.amazonaws.com/role-arn: {{ (index $observed "app-role").resource.status.atProvider.arn }}
            spec: {}
`

	bp, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(bp.Spec.Resources))
	}

	sa := bp.ResourceNamed("app-sa")
	if sa == nil {
		t.Fatal("resource app-sa not found")
	}

	ann := sa.Annotations["eks.amazonaws.com/role-arn"]
	if ann.From != "resources.app-role.status.arn" {
		t.Errorf("annotation wire = %+v, want From: resources.app-role.status.arn", ann)
	}
}

func TestAdoptMalformedYAMLReportsUnmarshalError(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: broken
spec:
  [unclosed yaml
`
	_, err := Adopt([]byte(manifest), Options{})
	if err == nil {
		t.Fatalf("expected error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal document") && !strings.Contains(err.Error(), "yaml") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAdoptInvalidBlueprintFailsValidation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: bad_name_with_underscores
`
	_, err := Adopt([]byte(manifest), Options{})
	if err == nil {
		t.Fatalf("expected validation error for invalid resource name, got nil")
	}
	if !strings.Contains(err.Error(), "validate adopted blueprint") {
		t.Errorf("expected validate error prefix, got: %v", err)
	}
}
