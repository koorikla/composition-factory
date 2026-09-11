package adopt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// dropsBeyondXRDless returns the drops that are not the per-parameter
// "without the XRD" entries an XRD-less adoption always records (CF-108).
func dropsBeyondXRDless(report *LossReport) []Drop {
	var out []Drop
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") && strings.HasPrefix(d.Reason, "without the XRD") {
			continue
		}
		out = append(out, d)
	}
	return out
}

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

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
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
		t.Errorf("expected parameter region in XRD parameters")
	}
	if _, ok := bp.Spec.XRD.Parameters["roleArn"]; !ok {
		t.Errorf("expected parameter roleArn in XRD parameters")
	}

	// Templates (defines)
	if _, ok := bp.Spec.Templates["cf.tags"]; !ok {
		t.Errorf("expected template cf.tags in Spec.Templates")
	}

	// Pipeline: inferred default auto-ready step must not be adopted as a custom pipeline step (CF-119)
	if len(bp.Spec.Pipeline) != 0 {
		t.Errorf("Pipeline = %+v, want empty (inferred default auto-ready must not become a custom step)", bp.Spec.Pipeline)
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

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
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

func TestAdoptFunctionPatchAndTransform(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: pt-buckets
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XBucket
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: s3-bucket
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider:
                  region: us-east-1
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.parameters.bucketName
                toFieldPath: spec.forProvider.name
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.Name != "s3-bucket" || res.Kind != "Bucket" {
		t.Errorf("resource = %s (%s), want s3-bucket (Bucket)", res.Name, res.Kind)
	}
	if res.Fields["name"].From != "params.bucketName" {
		t.Errorf("bucket name field = %+v, want From: params.bucketName", res.Fields["name"])
	}

	// Verify function-patch-and-transform and default auto-ready are not kept in pipeline (CF-119)
	if len(bp.Spec.Pipeline) != 0 {
		t.Errorf("pipeline = %+v, want empty pipeline (default inferred auto-ready dropped)", bp.Spec.Pipeline)
	}
}

func TestAdoptXRDFlatParametersAndNestedObjects(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xclusters.example.org
spec:
  group: example.org
  names:
    kind: XCluster
    plural: xclusters
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
              required:
                - clusterName
              properties:
                clusterName:
                  type: string
                  description: Name of the cluster
                nodeCount:
                  type: integer
                  default: 3
                  description: Number of worker nodes
                tuning:
                  type: object
                  required:
                    - maxPods
                  properties:
                    maxPods:
                      type: integer
                      default: 110
                    enableMonitoring:
                      type: boolean
                      default: true
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xcluster-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCluster
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
            apiVersion: eks.aws.upbound.io/v1beta1
            kind: Cluster
            metadata:
              name: main-cluster
            spec:
              forProvider:
                name: {{ $spec.clusterName }}
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	// Flat parameters parsed under spec
	pClusterName := bp.Spec.XRD.Parameters["clusterName"]
	if !pClusterName.Required || pClusterName.Description != "Name of the cluster" {
		t.Errorf("clusterName parameter = %+v", pClusterName)
	}
	pNodeCount := bp.Spec.XRD.Parameters["nodeCount"]
	if pNodeCount.Type != "integer" || pNodeCount.Default != "3" {
		t.Errorf("nodeCount parameter = %+v", pNodeCount)
	}

	// Nested object tuning
	pTuning := bp.Spec.XRD.Parameters["tuning"]
	if pTuning.Type != "object" || len(pTuning.Properties) != 2 {
		t.Fatalf("tuning parameter = %+v, want object with 2 properties", pTuning)
	}
	pMaxPods := pTuning.Properties["maxPods"]
	if !pMaxPods.Required || pMaxPods.Type != "integer" || pMaxPods.Default != "110" {
		t.Errorf("tuning.maxPods = %+v", pMaxPods)
	}
	pMon := pTuning.Properties["enableMonitoring"]
	if pMon.Type != "boolean" || pMon.Default != "true" {
		t.Errorf("tuning.enableMonitoring = %+v", pMon)
	}
}

func TestAdoptXRDUnsupportedDropped(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xnetworks.example.org
spec:
  group: example.org
  claimNames:
    kind: Network
    plural: networks
  connectionSecretKeys:
    - kubeconfig
  names:
    kind: XNetwork
    plural: xnetworks
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
                subnets:
                  type: array
                  description: List of subnets
                cidr:
                  type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xnetwork-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XNetwork
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
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: VPC
            metadata:
              name: main-vpc
            spec:
              forProvider:
                cidrBlock: {{ $spec.cidr }}
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to array, claimNames, connectionSecretKeys")
	}

	// Verify subnets array parameter is NOT in bp parameters
	if _, ok := bp.Spec.XRD.Parameters["subnets"]; ok {
		t.Errorf("expected array parameter subnets to be dropped")
	}
	if _, ok := bp.Spec.XRD.Parameters["cidr"]; !ok {
		t.Errorf("expected string parameter cidr to be preserved")
	}

	// Verify drops recorded in report
	dropStr := report.String()
	if !strings.Contains(dropStr, "xrd.parameters.subnets") {
		t.Errorf("report missing subnets drop: %s", dropStr)
	}
	if !strings.Contains(dropStr, "xrd.claimNames") {
		t.Errorf("report missing claimNames drop: %s", dropStr)
	}
}

func TestAdoptResourceNameNormalization(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-norm
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XNorm
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
              name: Bad_Name_With_Underscores
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: v1
            kind: ServiceAccount
            metadata:
              name: App_SA
              annotations:
                queue-url: {{ (index $observed "Bad_Name_With_Underscores").resource.status.atProvider.url }}
            spec: {}
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	_ = report

	q := bp.ResourceNamed("bad-name-with-underscores")
	if q == nil {
		t.Fatalf("expected normalized resource bad-name-with-underscores, got resources: %+v", bp.Spec.Resources)
	}

	sa := bp.ResourceNamed("app-sa")
	if sa == nil {
		t.Fatalf("expected normalized resource app-sa")
	}
	ann := sa.Annotations["queue-url"]
	if ann.From != "resources.bad-name-with-underscores.status.atProvider.url" {
		t.Errorf("status wire = %q, want From: resources.bad-name-with-underscores.status.atProvider.url", ann.From)
	}
}

func TestAdoptNonParamPatchDropped(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-non-params
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XNonParam
  resources:
    - name: role
      base:
        apiVersion: iam.aws.upbound.io/v1beta1
        kind: Role
        spec:
          forProvider:
            assumeRolePolicy: "{}"
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: metadata.uid
          toFieldPath: spec.forProvider.description
        - type: FromCompositeFieldPath
          fromFieldPath: status.eks.oidc
          toFieldPath: spec.forProvider.path
        - type: ToCompositeFieldPath
          fromFieldPath: status.atProvider.arn
          toFieldPath: status.arn
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to non-param patches")
	}

	// metadata.uid and status.eks.oidc should NOT be in parameters
	if _, ok := bp.Spec.XRD.Parameters["metadata"]; ok {
		t.Errorf("metadata should not be created as parameter")
	}
	if _, ok := bp.Spec.XRD.Parameters["status"]; ok {
		t.Errorf("status should not be created as parameter")
	}
}

func TestAdoptMultiLineScalarDropped(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-multiline
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XMulti
  resources:
    - name: policy
      base:
        apiVersion: iam.aws.upbound.io/v1beta1
        kind: Policy
        spec:
          forProvider:
            name: my-policy
            policy: |
              {
                "Version": "2012-10-17",
                "Statement": []
              }
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.IsLossy() {
		t.Fatalf("expected lossy report for multiline policy")
	}

	// Blueprint should validate cleanly because multiline field was dropped
	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validate failed: %v", err)
	}
}

func TestAdoptLossReportComments(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-loss"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Kind:    "XTest",
				Version: "v1alpha1",
				Plural:  "xtests",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
		},
	}
	report := &LossReport{}
	report.Record("xrd.parameters.tags", "array parameter not supported")
	report.Record("resource.db.patches[0]", "ToCompositeFieldPath not supported")

	yamlBytes, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML: %v", err)
	}

	yamlStr := string(yamlBytes)
	if !strings.Contains(yamlStr, "# adopt: dropped xrd.parameters.tags (array parameter not supported)") {
		t.Errorf("missing tags drop comment in:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "# adopt: dropped resource.db.patches[0] (ToCompositeFieldPath not supported)") {
		t.Errorf("missing patches drop comment in:\n%s", yamlStr)
	}

	// Ensure the YAML parses as a valid blueprint
	parsed, err := blueprint.Parse(yamlBytes)
	if err != nil {
		t.Fatalf("Parse of commented YAML failed: %v", err)
	}
	if parsed.Metadata.Name != "test-loss" {
		t.Errorf("parsed name = %q, want test-loss", parsed.Metadata.Name)
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

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	_ = report

	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(bp.Spec.Resources))
	}

	sa := bp.ResourceNamed("app-sa")
	if sa == nil {
		t.Fatal("resource app-sa not found")
	}

	ann := sa.Annotations["eks.amazonaws.com/role-arn"]
	if ann.From != "resources.app-role.status.atProvider.arn" {
		t.Errorf("annotation wire = %+v, want From: resources.app-role.status.atProvider.arn", ann)
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
	_, _, err := Adopt([]byte(manifest), Options{})
	if err == nil {
		t.Fatalf("expected error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal document") && !strings.Contains(err.Error(), "yaml") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAdoptIgnoresCustomResourceDefinition(t *testing.T) {
	manifest := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: xqueues.aws.example.org
spec:
  group: aws.example.org
  names:
    kind: XQueue
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: aws.example.org/v1alpha1
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
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
`
	bp, _, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp.APIVersion != "factory.crossplane.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want factory.crossplane.io/v1alpha1", bp.APIVersion)
	}
	if bp.Kind != "Blueprint" {
		t.Errorf("Kind = %q, want Blueprint", bp.Kind)
	}
}

func TestAdoptSelfGeneratedComposition(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: aws.example.org/v1alpha1
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
            {{- $observed := .observed.resources -}}
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                {{- if hasKey $spec "region" }}
                region: '{{ $spec.region }}'
                {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
}

func TestAdoptStatusWireNewConciseGuard(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-new-status-guard
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            {{- $xrMeta := .observed.composite.resource.metadata -}}
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: QueuePolicy
            metadata:
              name: queue-policy
              annotations:
                example.com/queue-url: {{ (index $.observed.resources "main-queue").resource.status.atProvider.url | quote }}
            spec:
              forProvider:
                {{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "url" }}
                queueUrl: {{ (index $.observed.resources "main-queue").resource.status.atProvider.url }}
                {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}
	policy := bp.ResourceNamed("queue-policy")
	if policy == nil {
		t.Fatal("resource queue-policy not found")
	}
	if fld := policy.Fields["queueUrl"]; fld.From != "resources.main-queue.status.atProvider.url" {
		t.Errorf("queueUrl field = %+v, want From: resources.main-queue.status.atProvider.url", fld)
	}
	if ann := policy.Annotations["example.com/queue-url"]; ann.From != "resources.main-queue.status.atProvider.url" {
		t.Errorf("annotation = %+v, want From: resources.main-queue.status.atProvider.url", ann)
	}
}

func TestAdoptStatusWireLegacyGuard(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-legacy-status-guard
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            {{- $xrMeta := .observed.composite.resource.metadata -}}
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: QueuePolicy
            metadata:
              name: queue-policy
            spec:
              forProvider:
                {{- if (and (hasKey $.observed "resources") (kindIs "map" $.observed.resources) (hasKey $.observed.resources "main-queue") (kindIs "map" (index $.observed.resources "main-queue")) (hasKey (index $.observed.resources "main-queue") "resource") (kindIs "map" (index $.observed.resources "main-queue").resource) (hasKey (index $.observed.resources "main-queue").resource "status") (kindIs "map" (index $.observed.resources "main-queue").resource.status) (hasKey (index $.observed.resources "main-queue").resource.status "atProvider") (kindIs "map" (index $.observed.resources "main-queue").resource.status.atProvider) (hasKey (index $.observed.resources "main-queue").resource.status.atProvider "url")) }}
                queueUrl: {{ (index $.observed.resources "main-queue").resource.status.atProvider.url }}
                {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}
	policy := bp.ResourceNamed("queue-policy")
	if policy == nil {
		t.Fatal("resource queue-policy not found")
	}
	if fld := policy.Fields["queueUrl"]; fld.From != "resources.main-queue.status.atProvider.url" {
		t.Errorf("queueUrl field = %+v, want From: resources.main-queue.status.atProvider.url", fld)
	}
}

func TestAdoptCustomPipelineStepsWithInputs(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-custom-pipeline-steps
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  mode: Pipeline
  pipeline:
    - step: custom-pre-processor
      functionRef:
        name: function-pre-step
      input:
        apiVersion: fn.example.org/v1alpha1
        kind: PreStepInput
        package: example.org/functions/pre-step:v1.0.0
        spec:
          strictMode: true
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
    - step: custom-post-processor
      functionRef:
        name: function-post-step
      input:
        apiVersion: fn.example.org/v1alpha1
        kind: PostStepInput
        package: example.org/functions/post-step:v2.0.0
        spec:
          tagAll: true
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	if len(bp.Spec.Pipeline) != 2 {
		t.Fatalf("expected 2 custom pipeline steps, got %d", len(bp.Spec.Pipeline))
	}

	pre := bp.Spec.Pipeline[0]
	if pre.Name != "custom-pre-processor" || pre.Position != "before" || pre.Package != "example.org/functions/pre-step:v1.0.0" {
		t.Errorf("unexpected pre step: %+v", pre)
	}
	if !strings.Contains(pre.Input, "strictMode: true") {
		t.Errorf("expected input to contain strictMode: true, got:\n%s", pre.Input)
	}

	post := bp.Spec.Pipeline[1]
	if post.Name != "custom-post-processor" || post.Position != "after" || post.Package != "example.org/functions/post-step:v2.0.0" {
		t.Errorf("unexpected post step: %+v", post)
	}
	if !strings.Contains(post.Input, "tagAll: true") {
		t.Errorf("expected input to contain tagAll: true, got:\n%s", post.Input)
	}
}

func TestAdoptNativeKubernetesResources(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-native-workload
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XR
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: Service
            metadata:
              name: my-svc
            spec:
              selector:
                app: workload
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: my-config
            data:
              PORT: "8080"
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	svc := bp.ResourceNamed("my-svc")
	if svc == nil {
		t.Fatal("resource my-svc not found")
	}
	if svc.Provider != blueprint.NativeProvider {
		t.Errorf("svc provider = %q, want %q", svc.Provider, blueprint.NativeProvider)
	}
	if fld, ok := svc.Fields["spec.selector[app]"]; !ok || fld.Value != "workload" {
		t.Errorf("svc spec.selector[app] = %+v, want Value: workload", fld)
	}

	cm := bp.ResourceNamed("my-config")
	if cm == nil {
		t.Fatal("resource my-config not found")
	}
	if cm.Provider != blueprint.NativeProvider {
		t.Errorf("cm provider = %q, want %q", cm.Provider, blueprint.NativeProvider)
	}
	if fld, ok := cm.Fields["data[PORT]"]; !ok || fld.Value != "8080" {
		t.Errorf("cm data[PORT] = %+v, want Value: 8080", fld)
	}
}

func TestAdoptComposedResourceMetadataPreservation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: metalosses.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: MetaLoss
  mode: Pipeline
  pipeline:
  - step: render-resources
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          {{- $spec := .observed.composite.resource.spec -}}
          {{- $xr := .observed.composite.resource.metadata.name -}}
          ---
          apiVersion: v1
          kind: ServiceAccount
          metadata:
            name: {{ $xr }}-sa
            annotations:
              {{ setResourceNameAnnotation "sa" }}
              iam.example.com/role: {{ $spec.roleArn }}
            labels:
              app: {{ $spec.appName | quote }}
              tier: backend
          ---
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: {{ $xr }}-cm
            annotations:
              {{ setResourceNameAnnotation "cm" }}
            labels:
              app: {{ $spec.appName | quote }}
            namespace: custom-ns
          data:
            app: {{ $spec.appName | quote }}
  - step: auto-ready
    functionRef:
      name: function-auto-ready
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	sa := bp.ResourceNamed("sa")
	if sa == nil {
		t.Fatal("resource sa not found")
	}
	if fld, ok := sa.Fields["metadata.labels[app]"]; !ok || fld.From != "params.appName" {
		t.Errorf("sa metadata.labels[app] = %+v, want From: params.appName", fld)
	}
	if fld, ok := sa.Fields["metadata.labels[tier]"]; !ok || fld.Value != "backend" {
		t.Errorf("sa metadata.labels[tier] = %+v, want Value: backend", fld)
	}
	if ann, ok := sa.Annotations["iam.example.com/role"]; !ok || ann.From != "params.roleArn" {
		t.Errorf("sa annotation iam.example.com/role = %+v, want From: params.roleArn", ann)
	}

	cm := bp.ResourceNamed("cm")
	if cm == nil {
		t.Fatal("resource cm not found")
	}
	if fld, ok := cm.Fields["metadata.labels[app]"]; !ok || fld.From != "params.appName" {
		t.Errorf("cm metadata.labels[app] = %+v, want From: params.appName", fld)
	}
	if fld, ok := cm.Fields["metadata.namespace"]; !ok || fld.Value != "custom-ns" {
		t.Errorf("cm metadata.namespace = %+v, want Value: custom-ns", fld)
	}
	if fld, ok := cm.Fields["data[app]"]; !ok || fld.From != "params.appName" {
		t.Errorf("cm data[app] = %+v, want From: params.appName", fld)
	}
}

func TestAdoptClassicCompositionMetadataPatches(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-comp.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
  resources:
  - name: my-sa
    base:
      apiVersion: v1
      kind: ServiceAccount
      metadata:
        labels:
          environment: dev
    patches:
    - type: FromCompositeFieldPath
      fromFieldPath: spec.appName
      toFieldPath: metadata.labels.app
    - type: FromCompositeFieldPath
      fromFieldPath: spec.roleArn
      toFieldPath: metadata.annotations.iamRole
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	sa := bp.ResourceNamed("my-sa")
	if sa == nil {
		t.Fatal("resource my-sa not found")
	}
	if fld, ok := sa.Fields["metadata.labels[environment]"]; !ok || fld.Value != "dev" {
		t.Errorf("my-sa metadata.labels[environment] = %+v, want Value: dev", fld)
	}
	if fld, ok := sa.Fields["metadata.labels[app]"]; !ok || fld.From != "params.appName" {
		t.Errorf("my-sa metadata.labels[app] = %+v, want From: params.appName", fld)
	}
	if ann, ok := sa.Annotations["iamRole"]; !ok || ann.From != "params.roleArn" {
		t.Errorf("my-sa annotation iamRole = %+v, want From: params.roleArn", ann)
	}
}

func TestAdoptPluralInferenceWithoutXRD(t *testing.T) {
	tests := []struct {
		name       string
		compName   string
		kind       string
		group      string
		ctrPlural  string
		wantPlural string
	}{
		{
			name:       "deduce plural from composition metadata name and group (CF-075 MetaLoss)",
			compName:   "metalosses.platform.example.org",
			kind:       "MetaLoss",
			group:      "platform.example.org",
			wantPlural: "metalosses",
		},
		{
			name:       "deduce plural from compositeTypeRef plural override",
			compName:   "custom.platform.example.org",
			kind:       "CustomKind",
			group:      "platform.example.org",
			ctrPlural:  "myplurals",
			wantPlural: "myplurals",
		},
		{
			name:       "fallback to inferPlural for kinds ending in s",
			compName:   "unrelated-name",
			kind:       "MetaLoss",
			group:      "example.org",
			wantPlural: "metalosses",
		},
		{
			name:       "fallback to inferPlural for kinds ending in x",
			compName:   "unrelated-name",
			kind:       "XBox",
			group:      "example.org",
			wantPlural: "xboxes",
		},
		{
			name:       "fallback to inferPlural for kinds ending in y with consonant",
			compName:   "unrelated-name",
			kind:       "XPolicy",
			group:      "example.org",
			wantPlural: "xpolicies",
		},
		{
			name:       "fallback to inferPlural for standard kind",
			compName:   "unrelated-name",
			kind:       "XBucket",
			group:      "example.org",
			wantPlural: "xbuckets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluralLine := ""
			if tt.ctrPlural != "" {
				pluralLine = fmt.Sprintf("    plural: %s\n", tt.ctrPlural)
			}
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: %s
spec:
  compositeTypeRef:
    apiVersion: %s/v1alpha1
    kind: %s
%s  resources:
  - name: dummy
    base:
      apiVersion: v1
      kind: ConfigMap
`, tt.compName, tt.group, tt.kind, pluralLine)

			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if bp.Spec.XRD.Plural != tt.wantPlural {
				t.Errorf("XRD.Plural = %q, want %q", bp.Spec.XRD.Plural, tt.wantPlural)
			}
		})
	}
}

func TestCF111AdoptRefusesKCLAndPythonEnginesClearly(t *testing.T) {
	for _, engine := range []string{"function-kcl", "function-python"} {
		compYAML := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xdatabases.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XDatabase
  pipeline:
    - step: render-resources
      functionRef:
        name: %s
`, engine)
		_, _, err := Adopt([]byte(compYAML), Options{})
		if err == nil {
			t.Fatalf("expected error adopting %s composition, got nil", engine)
		}
		if strings.Contains(err.Error(), "collides with the built-in templating step's name") {
			t.Errorf("expected clear engine refusal for %s, got collision error: %v", engine, err)
		}
		if !strings.Contains(err.Error(), "function-go-templating") || !strings.Contains(err.Error(), engine) {
			t.Errorf("expected error to name %s and supported engines, got: %v", engine, err)
		}
	}
}

// CF-108 (#6): adopting a Composition without its XRD alongside must not
// silently retype every parameter as string and drop required/default/enum/
// description. The adopter must recover what the Composition itself proves
// (an unguarded reference is required, a quoted render is a string, a hasKey
// guard is optional) and name, per parameter, what it could not recover.
func TestCF108AdoptWithoutXRDNamesUnrecoveredParameterFacts(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xdatabases.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XDatabase
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            ---
            apiVersion: rds.aws.upbound.io/v1beta1
            kind: Instance
            metadata:
              name: {{ $xr }}-db
              annotations:
                {{ setResourceNameAnnotation "db" }}
            spec:
              forProvider:
                region: {{ $spec.region | quote }}
                {{- if hasKey $spec "storageGB" }}
                allocatedStorage: {{ $spec.storageGB }}
                {{- end }}
                {{- if hasKey $spec "engineVersion" }}
                engineVersion: {{ $spec.engineVersion | quote }}
                {{- end }}
                {{- if hasKey $spec "deletionProtection" }}
                deletionProtection: {{ $spec.deletionProtection }}
                {{- end }}
              providerConfigRef:
                name: {{ $spec.providerName | quote }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.HasTrueLoss() {
		t.Fatalf("adopting without the XRD must be reported as a true loss; report: %+v", report.Drops)
	}

	reasons := map[string]string{}
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") {
			reasons[strings.TrimPrefix(d.Path, "xrd.parameters.")] = d.Reason
		}
	}
	if _, ok := reasons["providerName"]; ok {
		t.Errorf("providerName is synthesised by the adopter, not lost: %q", reasons["providerName"])
	}
	for _, name := range []string{"region", "storageGB", "engineVersion", "deletionProtection"} {
		reason, ok := reasons[name]
		if !ok {
			t.Errorf("no loss entry for xrd.parameters.%s; drops: %+v", name, report.Drops)
			continue
		}
		if !strings.Contains(reason, "XRD") {
			t.Errorf("xrd.parameters.%s: reason must say the XRD was absent, got %q", name, reason)
		}
		for _, facet := range []string{"description", "default", "enum"} {
			if !strings.Contains(reason, facet) {
				t.Errorf("xrd.parameters.%s: reason must name %s as unrecovered, got %q", name, facet, reason)
			}
		}
	}

	// region is dereferenced unguarded and rendered quoted: required, string.
	region := bp.Spec.XRD.Parameters["region"]
	if !region.Required {
		t.Errorf("region is referenced without a hasKey guard, so it must be recovered as required; got %+v", region)
	}
	if region.Type != "string" {
		t.Errorf("region renders quoted, so its type is string; got %q", region.Type)
	}
	if strings.Contains(reasons["region"], "type") || strings.Contains(reasons["region"], "required") {
		t.Errorf("region's type and required are recoverable and must not be reported lost: %q", reasons["region"])
	}

	// engineVersion is guarded and quoted: optional, string; enum/default lost.
	ev := bp.Spec.XRD.Parameters["engineVersion"]
	if ev.Required || ev.Type != "string" {
		t.Errorf("engineVersion = %+v, want optional string", ev)
	}
	if strings.Contains(reasons["engineVersion"], "type") {
		t.Errorf("engineVersion renders quoted; its type must not be reported lost: %q", reasons["engineVersion"])
	}

	// storageGB and deletionProtection render unquoted: not strings, and the
	// exact type is not recoverable without the XRD. That must be said.
	for _, name := range []string{"storageGB", "deletionProtection"} {
		p := bp.Spec.XRD.Parameters[name]
		if p.Required {
			t.Errorf("%s is hasKey-guarded, so it is optional; got %+v", name, p)
		}
		if !strings.Contains(reasons[name], "type") {
			t.Errorf("%s renders unquoted; the report must say its type could not be recovered, got %q", name, reasons[name])
		}
	}

	// The on-screen form names every parameter.
	out := report.String()
	for _, name := range []string{"region", "storageGB", "engineVersion", "deletionProtection"} {
		if !strings.Contains(out, "xrd.parameters."+name) {
			t.Errorf("report.String() must name xrd.parameters.%s:\n%s", name, out)
		}
	}
}

// CF-119 (#7): Importing the Composition that Generate just wrote comes back
// with the inferred auto-ready step turned into a custom pipeline step whose
// kind 404s, and parameters' required flag lost.
// When adopting a Composition whose pipeline contains only the default inferred
// function-auto-ready step, cf must not adopt it as an explicit custom pipeline step.
func TestCF119ImportGeneratedCompositionRoundTrip(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata: blueprint.Metadata{
			Name: "xdatabases.platform.example.org",
		},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Version: "v1alpha1",
				Kind:    "XDatabase",
				Plural:  "xdatabases",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
					"region": {
						Type:     "string",
						Required: true,
						Default:  "eu-north-1",
					},
					"dbName": {
						Type:     "string",
						Required: true,
					},
					"instanceClass": {
						Type:     "string",
						Required: true,
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "db",
					Kind:     "Instance",
					Provider: "xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0",
					Fields: map[string]blueprint.Field{
						"region":            {From: "params.region"},
						"dbSubnetGroupName": {From: "params.dbName"},
						"instanceClass":     {From: "params.instanceClass"},
					},
				},
			},
		},
	}

	crdDoc := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: instances.rds.aws.upbound.io
spec:
  group: rds.aws.upbound.io
  names:
    kind: Instance
    plural: instances
    categories: [managed]
  scope: Namespaced
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              required: [forProvider]
              properties:
                forProvider:
                  type: object
                  required: [region, instanceClass]
                  properties:
                    region: {type: string}
                    instanceClass: {type: string}
                    dbSubnetGroupName: {type: string}
                providerConfigRef:
                  type: object
                  properties: {name: {type: string}}
`
	crds, err := schema.ParseCRDs(blueprint.SplitDocs([]byte(crdDoc)))
	if err != nil {
		t.Fatalf("parse CRD: %v", err)
	}
	outputs, err := emit.Generate(b, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatalf("no composition generated")
	}

	adoptedBP, report, err := Adopt(compYAML, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// The inferred auto-ready step in the default pipeline must NOT be
	// adopted as an explicit custom pipeline step.
	if len(adoptedBP.Spec.Pipeline) != 0 {
		t.Errorf("adopted pipeline = %+v, want empty (inferred default auto-ready must not become a custom step)", adoptedBP.Spec.Pipeline)
	}

	// Every parameter's required flag must survive if proven by the Composition.
	for _, name := range []string{"providerName", "region", "dbName", "instanceClass"} {
		p, ok := adoptedBP.Spec.XRD.Parameters[name]
		if !ok {
			t.Errorf("parameter %q missing from adopted blueprint", name)
			continue
		}
		if !p.Required {
			t.Errorf("parameter %q came back with required=false, want required=true", name)
		}
	}

	// Losses without the XRD must be accurately reported
	if !report.HasTrueLoss() {
		t.Fatalf("expected true loss report for XRD-less adoption")
	}
	reasons := map[string]string{}
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") {
			reasons[strings.TrimPrefix(d.Path, "xrd.parameters.")] = d.Reason
		}
	}
	if !strings.Contains(reasons["region"], "default") {
		t.Errorf("expected loss of default for region to be reported, got %q", reasons["region"])
	}

	// Round-trip emission must be reproducible
	reOutputs, err := emit.Generate(adoptedBP, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate on adopted blueprint: %v", err)
	}
	var reComp []byte
	for _, o := range reOutputs {
		if strings.Contains(o.Path, "compositions") {
			reComp = o.Body
			break
		}
	}
	if !bytes.Equal(compYAML, reComp) {
		t.Errorf("re-emitted composition differs:\n--- Orig ---\n%s\n--- Re-emitted ---\n%s", string(compYAML), string(reComp))
	}
}

func TestAdoptSliceEnvelopeFields(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xpostgresinstances.database.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: database.sparky.ee/v1alpha1
    kind: XPostgresInstance
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            ---
            apiVersion: rds.aws.m.upbound.io/v1beta1
            kind: Instance
            metadata:
              annotations:
                {{ setResourceNameAnnotation "db-instance" }}
            spec:
              forProvider:
                dbName: {{ $spec.dbName | quote }}
                region: {{ $spec.region | quote }}
              managementPolicies: ['Observe', 'Create', 'Update', 'Delete', 'LateInitialize']
              providerConfigRef:
                kind: ClusterProviderConfig
                name: {{ $spec.providerName }}
              writeConnectionSecretToRef:
                name: {{ $spec.dbName | quote }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if drops := dropsBeyondXRDless(report); len(drops) > 0 {
		t.Errorf("unexpected drops in loss report: %+v", drops)
	}

	res := bp.ResourceNamed("db-instance")
	if res == nil {
		t.Fatalf("resource db-instance not found in adopted blueprint")
	}

	for k := range res.Envelope {
		if strings.Contains(k, "[") || strings.Contains(k, "]") {
			t.Errorf("envelope key %q contains indexing brackets", k)
		}
	}

	fld, ok := res.Envelope["managementPolicies"]
	if !ok {
		t.Fatalf("managementPolicies missing from adopted envelope: %+v", res.Envelope)
	}
	wantVal := "Observe, Create, Update, Delete, LateInitialize"
	if fld.Value != wantVal {
		t.Errorf("managementPolicies.Value = %q, want %q", fld.Value, wantVal)
	}

	// Also test wildcard slice envelope: managementPolicies: ['*']
	manifestWildcard := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xpostgresinstances.database.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: database.sparky.ee/v1alpha1
    kind: XPostgresInstance
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            ---
            apiVersion: rds.aws.m.upbound.io/v1beta1
            kind: Instance
            metadata:
              annotations:
                {{ setResourceNameAnnotation "db-wildcard" }}
            spec:
              forProvider:
                dbName: {{ $spec.dbName | quote }}
              managementPolicies:
                - '*'
`
	bpWildcard, _, err := Adopt([]byte(manifestWildcard), Options{
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0",
	})
	if err != nil {
		t.Fatalf("Adopt with wildcard managementPolicies failed: %v", err)
	}
	resWildcard := bpWildcard.ResourceNamed("db-wildcard")
	if resWildcard == nil {
		t.Fatalf("resource db-wildcard not found")
	}
	for k := range resWildcard.Envelope {
		if strings.Contains(k, "[") || strings.Contains(k, "]") {
			t.Errorf("envelope key %q contains indexing brackets", k)
		}
	}
	fldWildcard, ok := resWildcard.Envelope["managementPolicies"]
	if !ok {
		t.Fatalf("managementPolicies missing from adopted envelope: %+v", resWildcard.Envelope)
	}
	if fldWildcard.Value != "*" {
		t.Errorf("wildcard managementPolicies.Value = %q, want '*'", fldWildcard.Value)
	}
}

func TestInferProviderReusesMemoizedStoreAcrossResources(t *testing.T) {
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	ref := "xpkg.upbound.io/upbound/provider-custom-test:v1.0.0"
	crds := []schema.CRD{
		{
			Group: "custom.test.io",
			Kind:  "ResourceA",
		},
		{
			Group: "custom.test.io",
			Kind:  "ResourceB",
		},
	}
	if err := store.SaveCRDs(ref, "sha256:abc123456", crds); err != nil {
		t.Fatalf("SaveCRDs failed: %v", err)
	}

	// First resource lookup loads provider schemas into store's memo.
	p1 := inferProvider("custom.test.io/v1", "ResourceA", "", store, nil)
	if p1 != ref {
		t.Fatalf("inferProvider(ResourceA) = %q, want %q", p1, ref)
	}

	// Corrupt crds.json on disk so that any subsequent disk read/unmarshal will fail.
	// We keep the "ref" field at the beginning so store.List() still discovers the provider,
	// but store.Load(ref) would fail if it attempted to re-read and unmarshal from disk.
	matches, err := filepath.Glob(filepath.Join(cacheDir, "*", "crds.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("failed to locate cached crds.json: %v", err)
	}
	corrupted := fmt.Sprintf("{\"ref\": %q, \"crds\": [INVALID_JSON_CORRUPTED", ref)
	if err := os.WriteFile(matches[0], []byte(corrupted), 0o644); err != nil {
		t.Fatalf("failed to write corrupted crds.json: %v", err)
	}

	// Second lookup on the SAME store must hit the in-memory memo and succeed without reading disk.
	p2 := inferProvider("custom.test.io/v1", "ResourceB", "", store, nil)
	if p2 != ref {
		t.Fatalf("inferProvider(ResourceB) = %q, want %q; expected memoized store to be reused", p2, ref)
	}

	// Full adoption verification: adopting a Composition with multiple resources
	// reuses the memoized Store across all resources.
	cacheDir2 := t.TempDir()
	store2 := cache.New(cacheDir2)
	if err := store2.SaveCRDs(ref, "sha256:abc123456", crds); err != nil {
		t.Fatalf("SaveCRDs failed: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-multi-resource-memoized
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCustom
  resources:
    - name: res-a
      base:
        apiVersion: custom.test.io/v1
        kind: ResourceA
    - name: res-b
      base:
        apiVersion: custom.test.io/v1
        kind: ResourceB
`
	bp, _, err := Adopt([]byte(manifest), Options{Store: store2, CacheDir: cacheDir2})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Provider != ref {
		t.Errorf("resource 0 provider = %q, want %q", bp.Spec.Resources[0].Provider, ref)
	}
	if bp.Spec.Resources[1].Provider != ref {
		t.Errorf("resource 1 provider = %q, want %q", bp.Spec.Resources[1].Provider, ref)
	}

	// Verify that passing CacheDir without pre-initialized Store also initializes
	// a single store once and correctly infers providers across multiple resources.
	bpCacheOnly, _, err := Adopt([]byte(manifest), Options{CacheDir: cacheDir2})
	if err != nil {
		t.Fatalf("Adopt with CacheDir failed: %v", err)
	}
	if len(bpCacheOnly.Spec.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(bpCacheOnly.Spec.Resources))
	}
	if bpCacheOnly.Spec.Resources[0].Provider != ref {
		t.Errorf("resource 0 provider = %q, want %q", bpCacheOnly.Spec.Resources[0].Provider, ref)
	}
	if bpCacheOnly.Spec.Resources[1].Provider != ref {
		t.Errorf("resource 1 provider = %q, want %q", bpCacheOnly.Spec.Resources[1].Provider, ref)
	}
}

func TestCF207_AdoptCompositionSpecUnsupportedFields_LossReport(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XApp
    plural: xapps
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
            required:
            - providerName
            properties:
              providerName:
                type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  writeConnectionSecretsToNamespace: crossplane-system
  publishConnectionDetailsWithStoreConfigRef:
    name: default
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: patch-and-transform
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: queue
        base:
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          spec:
            forProvider:
              region: eu-west-1
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatalf("expected blueprint, got nil")
	}

	if !report.HasTrueLoss() {
		t.Fatalf("expected true loss for unsupported Composition spec fields, got drops: %+v", report.Drops)
	}

	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if reason, ok := dropsByPath["spec.writeConnectionSecretsToNamespace"]; !ok {
		t.Errorf("expected drop for spec.writeConnectionSecretsToNamespace, got drops: %+v", report.Drops)
	} else if !strings.Contains(reason, "not supported in blueprint") {
		t.Errorf("unexpected reason for spec.writeConnectionSecretsToNamespace: %q", reason)
	}

	if reason, ok := dropsByPath["spec.publishConnectionDetailsWithStoreConfigRef"]; !ok {
		t.Errorf("expected drop for spec.publishConnectionDetailsWithStoreConfigRef, got drops: %+v", report.Drops)
	} else if !strings.Contains(reason, "not supported in blueprint") {
		t.Errorf("unexpected reason for spec.publishConnectionDetailsWithStoreConfigRef: %q", reason)
	}

	outYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped spec.writeConnectionSecretsToNamespace") {
		t.Errorf("expected comment for spec.writeConnectionSecretsToNamespace in YAML:\n%s", string(outYAML))
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped spec.publishConnectionDetailsWithStoreConfigRef") {
		t.Errorf("expected comment for spec.publishConnectionDetailsWithStoreConfigRef in YAML:\n%s", string(outYAML))
	}
}

func TestAdoptPatchTransformsLossy(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XApp
    plural: xapps
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
            required:
            - environment
            properties:
              environment:
                type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: patch-and-transform
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: queue
        base:
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          spec:
            forProvider: {}
        patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.environment
          toFieldPath: spec.forProvider.region
          transforms:
          - type: map
            map:
              prod: eu-west-1
              stage: eu-central-1
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to patch transforms")
	}
	if !report.HasTrueLoss() {
		t.Fatalf("expected HasTrueLoss to be true due to patch transforms")
	}

	found := false
	for _, d := range report.Drops {
		if d.Path == "resource.queue.patches[0].transforms" && d.Reason == "patch transforms are not supported in blueprint" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected drop for resource.queue.patches[0].transforms, got drops: %+v", report.Drops)
	}

	outBytes, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outBytes), "# adopt: dropped resource.queue.patches[0].transforms (patch transforms are not supported in blueprint)") {
		t.Errorf("missing drop comment in output yaml:\n%s", string(outBytes))
	}
}

func TestAdoptClassicCompositionPatchTransformsLossy(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XApp
    plural: xapps
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
            required:
            - appName
            properties:
              appName:
                type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  resources:
  - name: bucket
    base:
      apiVersion: s3.aws.upbound.io/v1beta1
      kind: Bucket
      spec:
        forProvider: {}
    patches:
    - type: FromCompositeFieldPath
      fromFieldPath: spec.parameters.appName
      toFieldPath: spec.forProvider.region
      transforms:
      - type: string
        string:
          fmt: "%s-bucket"
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to patch transforms in classic composition")
	}
	if !report.HasTrueLoss() {
		t.Fatalf("expected HasTrueLoss to be true due to patch transforms in classic composition")
	}

	found := false
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0].transforms" && d.Reason == "patch transforms are not supported in blueprint" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected drop for resource.bucket.patches[0].transforms, got drops: %+v", report.Drops)
	}

	outBytes, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outBytes), "# adopt: dropped resource.bucket.patches[0].transforms (patch transforms are not supported in blueprint)") {
		t.Errorf("missing drop comment in output yaml:\n%s", string(outBytes))
	}
}

func TestAdoptPatchWithoutTransformsNotLossy(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.platform.example.org
spec:
  group: platform.example.org
  names:
    kind: XApp
    plural: xapps
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
            required:
            - environment
            properties:
              environment:
                type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
  - step: patch-and-transform
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: queue
        base:
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          spec:
            forProvider: {}
        patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.environment
          toFieldPath: spec.forProvider.region
`
	_, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.IsLossy() {
		t.Errorf("expected non-lossy report for patch without transforms, got drops: %+v", report.Drops)
	}
}

func TestAdoptUnknownForProviderFieldsReportedInLossReport(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  maxMessageSize: {type: number}
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-unknown-fields
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
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                crossplane.io/composition-resource-name: main-queue
            spec:
              forProvider:
                region: us-east-1
                visibilityTimeoutBogus: 42
                nested:
                  unknownFieldPath: true
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.Lossy() {
		t.Fatalf("expected report.Lossy() to be true, got drops: %+v", report.Drops)
	}

	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.main-queue.fields.visibilityTimeoutBogus"]; !ok {
		t.Errorf("expected drop for resource.main-queue.fields.visibilityTimeoutBogus, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["resource.main-queue.fields.nested.unknownFieldPath"]; !ok {
		t.Errorf("expected drop for resource.main-queue.fields.nested.unknownFieldPath, got drops: %+v", report.Drops)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if _, ok := res.Fields["visibilityTimeoutBogus"]; ok {
		t.Errorf("dropped field visibilityTimeoutBogus should not be present in res.Fields")
	}
	if _, ok := res.Fields["nested.unknownFieldPath"]; ok {
		t.Errorf("dropped field nested.unknownFieldPath should not be present in res.Fields")
	}
	if fld, ok := res.Fields["region"]; !ok || fld.Value != "us-east-1" {
		t.Errorf("expected known field region to be preserved, got: %+v", res.Fields["region"])
	}
}

func TestPruneUnknownWiredFields(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  maxMessageSize: {type: number}
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-wired-unknown
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
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                crossplane.io/composition-resource-name: main-queue
            spec:
              forProvider:
                region: us-east-1
                bogusSetting: 'yes'
                {{- if hasKey $spec "retentionDays" }}
                retentionDays: {{ $spec.retentionDays }}
                {{- end }}
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.main-queue.fields.bogusSetting"]; !ok {
		t.Errorf("expected drop for bogusSetting, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["resource.main-queue.fields.retentionDays"]; !ok {
		t.Errorf("expected drop for wired unknown field retentionDays, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["xrd.parameters.retentionDays"]; !ok {
		t.Errorf("expected drop for orphaned parameter retentionDays, got drops: %+v", report.Drops)
	}

	res := bp.ResourceNamed("main-queue")
	if res == nil {
		t.Fatalf("missing main-queue resource")
	}
	if _, ok := res.Fields["retentionDays"]; ok {
		t.Errorf("retentionDays should be pruned from main-queue fields")
	}
	if _, ok := res.Fields["bogusSetting"]; ok {
		t.Errorf("bogusSetting should be pruned from main-queue fields")
	}

	if _, ok := bp.Spec.XRD.Parameters["retentionDays"]; ok {
		t.Errorf("orphaned parameter retentionDays should have been dropped, but remains in bp.Spec.XRD.Parameters")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}

	if _, err := emit.Generate(bp, crds, ""); err != nil {
		t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
	}
}

func TestAdoptClassicCompositionUnknownWiredForProviderFieldsPruned(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  maxMessageSize: {type: number}
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-unknown-wired
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  resources:
    - name: classic-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.retentionDays
          toFieldPath: spec.forProvider.bogusField
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.classic-queue.fields.bogusField"]; !ok {
		t.Errorf("expected drop for resource.classic-queue.fields.bogusField, got drops: %+v", report.Drops)
	}

	res := bp.ResourceNamed("classic-queue")
	if res == nil {
		t.Fatalf("missing classic-queue resource")
	}
	if _, ok := res.Fields["bogusField"]; ok {
		t.Errorf("bogusField should be pruned from classic-queue fields")
	}

	if _, ok := bp.Spec.XRD.Parameters["retentionDays"]; ok {
		t.Errorf("orphaned parameter retentionDays should have been dropped, but remains in bp.Spec.XRD.Parameters")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}

	if _, err := emit.Generate(bp, crds, ""); err != nil {
		t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
	}
}

func TestPruneUnknownWiredFields_BaseBlueprintParameterPreserved(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	baseBP := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Parameters: map[string]blueprint.Parameter{
					"retentionDays": {Type: "integer", Required: false},
				},
			},
		},
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-wired-unknown-base
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
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                crossplane.io/composition-resource-name: main-queue
            spec:
              forProvider:
                region: us-east-1
                {{- if hasKey $spec "retentionDays" }}
                retentionDays: {{ $spec.retentionDays }}
                {{- end }}
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:         store,
		CacheDir:      cacheDir,
		BaseBlueprint: baseBP,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.main-queue.fields.retentionDays"]; !ok {
		t.Errorf("expected drop for wired unknown field retentionDays, got drops: %+v", report.Drops)
	}

	// Parameter retentionDays was defined in BaseBlueprint, so it must be preserved
	if _, ok := bp.Spec.XRD.Parameters["retentionDays"]; !ok {
		t.Errorf("parameter retentionDays from BaseBlueprint should be preserved")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}

	if _, err := emit.Generate(bp, crds, ""); err != nil {
		t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
	}
}

func TestPruneUnknownWiredFields_NestedParameterMemberPruned(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-nested-param
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
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                crossplane.io/composition-resource-name: main-queue
            spec:
              forProvider:
                region: {{ $spec.tuning.region }}
                retentionDays: {{ $spec.tuning.retentionDays }}
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.main-queue.fields.retentionDays"]; !ok {
		t.Errorf("expected drop for wired unknown field retentionDays, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["xrd.parameters.tuning.properties.retentionDays"]; !ok {
		t.Errorf("expected drop for orphaned property tuning.retentionDays, got drops: %+v", report.Drops)
	}

	tuning, ok := bp.Spec.XRD.Parameters["tuning"]
	if !ok {
		t.Fatalf("expected tuning parameter to exist")
	}
	if _, ok := tuning.Properties["retentionDays"]; ok {
		t.Errorf("tuning.Properties[retentionDays] should be pruned")
	}
	if _, ok := tuning.Properties["region"]; !ok {
		t.Errorf("tuning.Properties[region] should be preserved")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}

	if _, err := emit.Generate(bp, crds, ""); err != nil {
		t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
	}
}

func TestAdoptClassicCompositionUnknownForProviderFieldsReportedInLossReport(t *testing.T) {
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  scope: Namespaced
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                  maxMessageSize: {type: number}
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-classic-unknown-fields
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  resources:
    - name: classic-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
            visibilityTimeoutBogus: 42
            nested:
              unknownFieldPath: true
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if !report.Lossy() {
		t.Fatalf("expected report.Lossy() to be true, got drops: %+v", report.Drops)
	}

	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		dropsByPath[d.Path] = d.Reason
	}

	if _, ok := dropsByPath["resource.classic-queue.fields.visibilityTimeoutBogus"]; !ok {
		t.Errorf("expected drop for resource.classic-queue.fields.visibilityTimeoutBogus, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["resource.classic-queue.fields.nested.unknownFieldPath"]; !ok {
		t.Errorf("expected drop for resource.classic-queue.fields.nested.unknownFieldPath, got drops: %+v", report.Drops)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if _, ok := res.Fields["visibilityTimeoutBogus"]; ok {
		t.Errorf("dropped field visibilityTimeoutBogus should not be present in res.Fields")
	}
	if _, ok := res.Fields["nested.unknownFieldPath"]; ok {
		t.Errorf("dropped field nested.unknownFieldPath should not be present in res.Fields")
	}
	if fld, ok := res.Fields["region"]; !ok || fld.Value != "us-east-1" {
		t.Errorf("expected known field region to be preserved, got: %+v", res.Fields["region"])
	}
}

func TestAdoptClassicCompositionPatchSets(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-patchsets
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XRQueue
  patchSets:
    - name: common-params
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.region
          toFieldPath: spec.forProvider.region
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.tags
          toFieldPath: metadata.annotations.env
  resources:
    - name: sqs-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider: {}
      patches:
        - type: PatchSet
          patchSetName: common-params
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.queueName
          toFieldPath: spec.forProvider.name
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond XRD-less parameter report, got drops: %+v", d)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
	if r.Annotations["env"].From != "params.tags" {
		t.Errorf("annotations[env] field = %+v, want From: params.tags", r.Annotations["env"])
	}
	if r.Fields["name"].From != "params.queueName" {
		t.Errorf("name field = %+v, want From: params.queueName", r.Fields["name"])
	}
	if _, ok := bp.Spec.XRD.Parameters["region"]; !ok {
		t.Errorf("expected parameter 'region' to be declared in XRD")
	}
	if _, ok := bp.Spec.XRD.Parameters["tags"]; !ok {
		t.Errorf("expected parameter 'tags' to be declared in XRD")
	}
}

func TestAdoptPipelineCompositionPatchSets(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: pt-patchsets
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XBucket
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        patchSets:
          - name: common-params
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.parameters.region
                toFieldPath: spec.forProvider.region
        resources:
          - name: s3-bucket
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider: {}
            patches:
              - type: PatchSet
                patchSetName: common-params
              - type: FromCompositeFieldPath
                fromFieldPath: spec.parameters.bucketName
                toFieldPath: spec.forProvider.name
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond XRD-less parameter report, got drops: %+v", d)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
	if r.Fields["name"].From != "params.bucketName" {
		t.Errorf("name field = %+v, want From: params.bucketName", r.Fields["name"])
	}
	if _, ok := bp.Spec.XRD.Parameters["region"]; !ok {
		t.Errorf("expected parameter 'region' to be declared in XRD")
	}
	if _, ok := bp.Spec.XRD.Parameters["bucketName"]; !ok {
		t.Errorf("expected parameter 'bucketName' to be declared in XRD")
	}
}

func TestAdoptPatchSetNotFound(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-patchset-missing
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
          forProvider: {}
      patches:
        - type: PatchSet
          patchSetName: non-existent
`
	_, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	found := false
	for _, d := range report.Drops {
		if d.Path == "resource.sqs-queue.patches[0]" && strings.Contains(d.Reason, `patchSet "non-existent" not found`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected drop for missing patchSet, got drops: %+v", report.Drops)
	}
}

func TestAdoptPatchSetUnsupportedPatch(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-patchset-unsupported
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XRQueue
  patchSets:
    - name: common-params
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.region
          toFieldPath: spec.forProvider.region
          transforms:
            - type: map
              map:
                dev: us-east-1
  resources:
    - name: sqs-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider: {}
      patches:
        - type: PatchSet
          patchSetName: common-params
`
	_, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	found := false
	for _, d := range report.Drops {
		if strings.Contains(d.Reason, "patch transforms are not supported in blueprint") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected drop for unsupported patch transform inside patchSet, got drops: %+v", report.Drops)
	}
}

func TestCF226_AdoptDynamicMapKeyExpressionsDroppedAndReported(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: repro-cf-expr
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: go-templating
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              prefix-{{ .observed.composite.resource.spec.env }}: value
              static-key: static-val
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatalf("expected blueprint, got nil")
	}

	// 1. Verify that __CF_EXPR never appears anywhere in the blueprint fields, envelope, annotations, or serialized YAML
	bpYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if strings.Contains(string(bpYAML), "__CF_EXPR") {
		t.Errorf("adopted blueprint YAML contains leaked __CF_EXPR token: %s", string(bpYAML))
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	for fName := range res.Fields {
		if strings.Contains(fName, "__CF_EXPR") {
			t.Errorf("field path %q contains leaked __CF_EXPR token", fName)
		}
	}

	// 2. The dynamic map key is dropped from blueprint fields
	for fName := range res.Fields {
		if strings.Contains(fName, "prefix-") {
			t.Errorf("dynamic map key should have been dropped, but found field %q", fName)
		}
	}
	if val, ok := res.Fields["data[static-key]"]; !ok || val.Value != "static-val" {
		t.Errorf("expected static key to be preserved, got: %+v", val)
	}

	// 3. The dynamic map key is reported in LossReport
	if !report.Lossy() {
		t.Fatalf("expected report.Lossy() to be true, got drops: %+v", report.Drops)
	}

	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "__CF_EXPR") {
			t.Errorf("loss report drop path %q contains leaked __CF_EXPR token", d.Path)
		}
		dropsByPath[d.Path] = d.Reason
	}

	expectedDropPath := "resource.test-cm.fields.data[prefix-{{ .observed.composite.resource.spec.env }}]"
	reason, ok := dropsByPath[expectedDropPath]
	if !ok {
		t.Errorf("expected drop for %q, got drops: %+v", expectedDropPath, report.Drops)
	} else if !strings.Contains(reason, "not supported in blueprint") {
		t.Errorf("expected reason to explain unsupported in blueprint, got %q", reason)
	}
}

func TestCF226_AdoptDynamicMapKeyExpressionsInManagedResource(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: repro-cf-expr-mr
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  pipeline:
    - step: go-templating
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: test-queue
              annotations:
                prefix-{{ .observed.composite.resource.spec.env }}: ann-val
                static-ann: static-ann-val
            spec:
              forProvider:
                region: us-east-1
                dynamic-{{ .observed.composite.resource.spec.env }}: dyn-val
                tags:
                  tag-{{ .observed.composite.resource.spec.env }}: custom
                  static-tag: static-val
              envelope-{{ .observed.composite.resource.spec.env }}: env-val
              staticEnvelope: static-env-val
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatalf("expected blueprint, got nil")
	}

	// 1. Verify that __CF_EXPR never appears in YAML
	bpYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if strings.Contains(string(bpYAML), "__CF_EXPR") {
		t.Errorf("adopted blueprint YAML contains leaked __CF_EXPR token: %s", string(bpYAML))
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	for fName := range res.Fields {
		if strings.Contains(fName, "__CF_EXPR") {
			t.Errorf("field path %q contains leaked __CF_EXPR token", fName)
		}
	}
	for aName := range res.Annotations {
		if strings.Contains(aName, "__CF_EXPR") {
			t.Errorf("annotation key %q contains leaked __CF_EXPR token", aName)
		}
	}
	for eName := range res.Envelope {
		if strings.Contains(eName, "__CF_EXPR") {
			t.Errorf("envelope key %q contains leaked __CF_EXPR token", eName)
		}
	}

	// 2. Static fields/annotations/envelope are preserved
	if val, ok := res.Annotations["static-ann"]; !ok || val.Value != "static-ann-val" {
		t.Errorf("expected static annotation preserved, got: %+v", val)
	}
	if val, ok := res.Fields["region"]; !ok || val.Value != "us-east-1" {
		t.Errorf("expected region field preserved, got: %+v", val)
	}
	if val, ok := res.Fields["tags[static-tag]"]; !ok || val.Value != "static-val" {
		t.Errorf("expected static tag preserved, got: %+v", val)
	}
	if val, ok := res.Envelope["staticEnvelope"]; !ok || val.Value != "static-env-val" {
		t.Errorf("expected static envelope preserved, got: %+v", val)
	}

	// 3. Dynamic map keys are dropped from blueprint
	for fName := range res.Fields {
		if strings.Contains(fName, "dynamic-") || strings.Contains(fName, "tag-") {
			t.Errorf("dynamic map key should have been dropped, but found field %q", fName)
		}
	}
	for aName := range res.Annotations {
		if strings.Contains(aName, "prefix-") {
			t.Errorf("dynamic annotation key should have been dropped, but found %q", aName)
		}
	}
	for eName := range res.Envelope {
		if strings.Contains(eName, "envelope-") {
			t.Errorf("dynamic envelope key should have been dropped, but found %q", eName)
		}
	}

	// 4. Dynamic keys reported in LossReport without __CF_EXPR
	dropsByPath := make(map[string]string)
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "__CF_EXPR") {
			t.Errorf("loss report drop path %q contains leaked __CF_EXPR token", d.Path)
		}
		dropsByPath[d.Path] = d.Reason
	}

	expectedDrops := []string{
		"resource.test-queue.annotations[prefix-{{ .observed.composite.resource.spec.env }}]",
		"resource.test-queue.fields.dynamic-{{ .observed.composite.resource.spec.env }}",
		"resource.test-queue.fields.tags[tag-{{ .observed.composite.resource.spec.env }}]",
		"resource.test-queue.envelope.envelope-{{ .observed.composite.resource.spec.env }}",
	}
	for _, expectedPath := range expectedDrops {
		reason, ok := dropsByPath[expectedPath]
		if !ok {
			t.Errorf("expected drop for %q, got drops: %+v", expectedPath, report.Drops)
		} else if !strings.Contains(reason, "not supported in blueprint") {
			t.Errorf("expected reason for %q to mention unsupported in blueprint, got %q", expectedPath, reason)
		}
	}
}

func TestAdoptIntegerLiteralPreservesWholeNumberFormat(t *testing.T) {
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
                messageRetentionSeconds: 1209600
                oneMillion: 1000000
                largeThreshold: 3000000
                negativeVal: -1209600
                zeroVal: 0
                delaySeconds: 900
                maxMessageSize: 262144
                retryIntervals:
                  - 1209600
                  - 1000000
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, _, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	expectedFields := map[string]string{
		"region":                  "us-east-1",
		"messageRetentionSeconds": "1209600",
		"oneMillion":              "1000000",
		"largeThreshold":          "3000000",
		"negativeVal":             "-1209600",
		"zeroVal":                 "0",
		"delaySeconds":            "900",
		"maxMessageSize":          "262144",
		"retryIntervals[0]":       "1209600",
		"retryIntervals[1]":       "1000000",
	}

	for k, want := range expectedFields {
		f, ok := res.Fields[k]
		if !ok {
			t.Errorf("field %q missing from adopted resource", k)
			continue
		}
		if f.Value != want {
			t.Errorf("field %q value = %q, want %q", k, f.Value, want)
		}
	}
}

func TestAdoptIntegerLiteralRoundTripEmission(t *testing.T) {
	crdDoc := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  names:
    kind: Queue
    plural: queues
    categories: [managed]
  scope: Namespaced
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              properties:
                forProvider:
                  type: object
                  properties:
                    region: {type: string}
                    messageRetentionSeconds: {type: integer}
                    delaySeconds: {type: integer}
`
	crds, err := schema.ParseCRDs(blueprint.SplitDocs([]byte(crdDoc)))
	if err != nil {
		t.Fatalf("parse CRD: %v", err)
	}

	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata: blueprint.Metadata{
			Name: "queues.aws.example.org",
		},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0"},
			},
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XQueue",
				Plural:  "xqueues",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "main-queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
					Fields: map[string]blueprint.Field{
						"region":                  {Value: "us-east-1"},
						"messageRetentionSeconds": {Value: "1209600"},
						"delaySeconds":            {Value: "900"},
					},
				},
			},
		},
	}

	outputs, err := emit.Generate(b, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatalf("no composition generated")
	}

	adoptedBP, _, err := Adopt(compYAML, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	reOutputs, err := emit.Generate(adoptedBP, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate on adopted blueprint: %v", err)
	}
	var reComp []byte
	for _, o := range reOutputs {
		if strings.Contains(o.Path, "compositions") {
			reComp = o.Body
			break
		}
	}
	if len(reComp) == 0 {
		t.Fatalf("no re-composition generated")
	}

	if strings.Contains(string(reComp), "1.2096e+06") {
		t.Errorf("regenerated composition contains scientific notation 1.2096e+06:\n%s", string(reComp))
	}
	if !strings.Contains(string(reComp), "messageRetentionSeconds: 1209600") {
		t.Errorf("regenerated composition missing 'messageRetentionSeconds: 1209600':\n%s", string(reComp))
	}
}

func TestAdoptNativeTopLevelWires(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: sa-wires-demo
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
            {{- $spec := .observed.composite.resource.spec -}}
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
            automountServiceAccountToken: {{ $spec.automountToken }}
            customStatusWire: {{ (index $observed "app-role").resource.status.atProvider.arn }}
            customEnvWire: {{ $env.myFlag }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	sa := bp.ResourceNamed("app-sa")
	if sa == nil {
		t.Fatal("resource app-sa not found")
	}

	if got := sa.Fields["automountServiceAccountToken"].From; got != "params.automountToken" {
		t.Errorf("automountServiceAccountToken wire = %+v, want From: params.automountToken", sa.Fields["automountServiceAccountToken"])
	}
	if got := sa.Fields["customStatusWire"].From; got != "resources.app-role.status.atProvider.arn" {
		t.Errorf("customStatusWire wire = %+v, want From: resources.app-role.status.atProvider.arn", sa.Fields["customStatusWire"])
	}
	if got := sa.Fields["customEnvWire"].From; got != "env.myFlag" {
		t.Errorf("customEnvWire wire = %+v, want From: env.myFlag", sa.Fields["customEnvWire"])
	}

	// Round-trip verification: emitting an adopted blueprint with a boolean parameter wired to
	// automountServiceAccountToken must succeed without structured emission errors (CF-233).
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	origBP := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "sa-rt"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSa", Plural: "xsas",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"automountToken": {Type: "boolean", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"automountServiceAccountToken": {From: "params.automountToken"},
					},
				},
			},
		},
	}
	outputs, err := emit.Generate(origBP, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate origBP: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatalf("no composition generated for origBP")
	}

	adopted, _, err := Adopt(compYAML, Options{})
	if err != nil {
		t.Fatalf("Adopt generated composition: %v", err)
	}
	adoptedSA := adopted.ResourceNamed("sa")
	if adoptedSA == nil {
		t.Fatal("resource sa not found in adopted blueprint")
	}
	if got := adoptedSA.Fields["automountServiceAccountToken"].From; got != "params.automountToken" {
		t.Errorf("roundtrip adopted automountServiceAccountToken wire = %+v, want From: params.automountToken", adoptedSA.Fields["automountServiceAccountToken"])
	}
	// Re-generation from adopted blueprint must succeed (the emitter refused the literal value {{ $spec.automountToken }} on boolean field)
	if _, err := emit.Generate(adopted, nativeCRDs, ""); err != nil {
		t.Fatalf("emit.Generate on adopted blueprint failed: %v", err)
	}
}

func TestAdoptNamedTemplateParameterDiscovered(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.aws.example.org
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
            {{- define "trust-policy" }}
            {"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Federated":"{{ .spec.oidcProviderArn }}"},"Action":"sts:AssumeRoleWithWebIdentity"}]}
            {{- end }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: main-cm
            data:
              policy: |
                {{ include "trust-policy" . }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, _, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-iam:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if _, ok := bp.Spec.XRD.Parameters["oidcProviderArn"]; !ok {
		t.Fatalf("expected parameter oidcProviderArn to be discovered from named template, got parameters: %+v", bp.Spec.XRD.Parameters)
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	if _, err := emit.Generate(bp, nativeCRDs, ""); err != nil {
		t.Fatalf("emit.Generate() failed: %v", err)
	}
}

func TestAdoptStatusForEachPreserved(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueuefans.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueueFan
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
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                {{ setResourceNameAnnotation "main-queue" }}
            spec:
              forProvider:
                region: 'eu-north-1'
            {{- if hasKey (dig "resources" "main-queue" "resource" "status" "atProvider" dict $.observed) "maxMessageSize" }}
            {{- range $i := until (int (index $.observed.resources "main-queue").resource.status.atProvider.maxMessageSize) }}
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                {{ setResourceNameAnnotation (printf "replica-queue-%d" $i) }}
            spec:
              forProvider:
                region: 'eu-north-1'
            {{- end }}
            {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	bp, _, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	var replica *blueprint.Resource
	for i := range bp.Spec.Resources {
		if bp.Spec.Resources[i].Name == "replica-queue" {
			replica = &bp.Spec.Resources[i]
			break
		}
	}
	if replica == nil {
		t.Fatalf("replica-queue resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	wantForEach := "resources.main-queue.status.atProvider.maxMessageSize"
	if replica.ForEach != wantForEach {
		t.Errorf("replica-queue.ForEach = %q, want %q", replica.ForEach, wantForEach)
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed on adopted blueprint: %v", err)
	}

	// Also test dot notation syntax:
	// {{- range $i := until (int $.observed.resources.main-queue.resource.status.atProvider.maxMessageSize) }}
	manifestDot := strings.Replace(manifest, `(index $.observed.resources "main-queue")`, `$.observed.resources.main-queue`, 1)
	bpDot, _, err := Adopt([]byte(manifestDot), Options{
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
	})
	if err != nil {
		t.Fatalf("Adopt dot notation failed: %v", err)
	}
	var replicaDot *blueprint.Resource
	for i := range bpDot.Spec.Resources {
		if bpDot.Spec.Resources[i].Name == "replica-queue" {
			replicaDot = &bpDot.Spec.Resources[i]
			break
		}
	}
	if replicaDot == nil || replicaDot.ForEach != wantForEach {
		t.Errorf("replicaDot.ForEach = %v, want %q", replicaDot, wantForEach)
	}
}

func TestAdopt_TruncatedGoTemplateRefusedOrLossReported(t *testing.T) {
	goldenManifest, err := os.ReadFile(filepath.Join("..", "..", "testdata", "xqueue-pipeline.composition.golden.yaml"))
	if err != nil {
		t.Fatalf("read golden composition: %v", err)
	}
	if len(goldenManifest) < 700 {
		t.Fatalf("golden manifest too short: %d bytes", len(goldenManifest))
	}
	truncated := goldenManifest[:700]

	bp, report, err := Adopt(truncated, Options{})
	if err == nil {
		if report == nil || len(report.Drops) == 0 {
			t.Fatalf("expected error or non-empty LossReport when adopting truncated go-template composition, got err=nil, drops=0, bp.resources=%d", len(bp.Spec.Resources))
		}
	} else {
		if !strings.Contains(err.Error(), "malformed go template") && !strings.Contains(err.Error(), "parse go template") {
			t.Fatalf("expected template parse error, got: %v", err)
		}
	}
}

func TestAdopt_MalformedGoTemplateChunkLossReported(t *testing.T) {
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: valid-queue
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                {{ setResourceNameAnnotation "broken-queue" }}
            spec:
              forProvider:
                invalid: [unclosed
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}
	if len(bp.Spec.Resources) != 1 || bp.Spec.Resources[0].Name != "valid-queue" {
		t.Fatalf("expected valid-queue to be adopted, got resources: %+v", bp.Spec.Resources)
	}
	if report == nil || !report.HasTrueLoss() {
		t.Fatalf("expected non-empty loss report for broken chunk, got: %+v", report)
	}
	foundBrokenDrop := false
	for _, d := range report.Drops {
		if d.Path == "template.resource.broken-queue" && strings.Contains(d.Reason, "failed to parse chunk YAML") {
			foundBrokenDrop = true
			break
		}
	}
	if !foundBrokenDrop {
		t.Fatalf("expected drop for template.resource.broken-queue, got drops: %+v", report.Drops)
	}
}

func TestAdopt_NonEmptyGoTemplateZeroResourcesLossReported(t *testing.T) {
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
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}
	if len(bp.Spec.Resources) != 0 {
		t.Fatalf("expected 0 resources, got: %d", len(bp.Spec.Resources))
	}
	if report == nil || !report.HasTrueLoss() {
		t.Fatalf("expected non-empty loss report when template yields zero resources, got: %+v", report)
	}
	foundZeroLoss := false
	for _, d := range report.Drops {
		if d.Path == "template.body" && strings.Contains(d.Reason, "no resources could be recovered") {
			foundZeroLoss = true
			break
		}
	}
	if !foundZeroLoss {
		t.Fatalf("expected drop for template.body, got drops: %+v", report.Drops)
	}
}

func TestAdopt_TemplateIncludeFieldPreserved(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xroles.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XRole
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
            {{- define "trust-policy" }}
            {"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}
            {{- end }}
            ---
            apiVersion: iam.aws.upbound.io/v1beta1
            kind: Role
            metadata:
              name: main-role
              annotations:
                policy-type: '{{ include "trust-policy" . }}'
            spec:
              forProvider:
                assumeRolePolicy: '{{ include "trust-policy" (dict "spec" $spec "xr" $xr "xrMeta" $xrMeta "observed" $.observed "resource" "role" "field" "assumeRolePolicy") | trim | nindent 6 }}'
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-iam:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report != nil && report.HasTrueLoss() {
		t.Fatalf("unexpected loss: %+v", report.Drops)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	f, ok := r.Fields["assumeRolePolicy"]
	if !ok {
		t.Fatalf("expected assumeRolePolicy field on resource, got fields: %+v", r.Fields)
	}
	if f.Template != "trust-policy" {
		t.Errorf("expected assumeRolePolicy.Template == 'trust-policy', got %q (Raw: %q)", f.Template, f.Raw)
	}
	if f.Raw != "" {
		t.Errorf("expected assumeRolePolicy.Raw == '', got %q", f.Raw)
	}
	ann, ok := r.Annotations["policy-type"]
	if !ok {
		t.Fatalf("expected policy-type annotation on resource, got annotations: %+v", r.Annotations)
	}
	if ann.Template != "trust-policy" {
		t.Errorf("expected policy-type.Template == 'trust-policy', got %q (Raw: %q)", ann.Template, ann.Raw)
	}
	if ann.Raw != "" {
		t.Errorf("expected policy-type.Raw == '', got %q", ann.Raw)
	}

	// Verify round-trip: emit blueprint to composition, then re-adopt
	crdDoc := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: roles.iam.aws.upbound.io
spec:
  group: iam.aws.upbound.io
  scope: Namespaced
  names:
    kind: Role
    plural: roles
    categories:
      - crossplane
      - managed
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                forProvider:
                  type: object
                  properties:
                    assumeRolePolicy: {type: string}
`
	crds, err := schema.ParseCRDs(blueprint.SplitDocs([]byte(crdDoc)))
	if err != nil {
		t.Fatalf("parse CRD: %v", err)
	}
	outputs, err := emit.Generate(bp, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatalf("no composition generated in outputs")
	}
	reAdopted, reReport, err := Adopt(compYAML, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-iam:v1.14.0",
	})
	if err != nil {
		t.Fatalf("re-Adopt failed: %v", err)
	}
	if reReport != nil && reReport.HasTrueLoss() {
		t.Fatalf("unexpected loss on re-adopt: %+v", reReport.Drops)
	}
	if len(reAdopted.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource on re-adopt, got %d", len(reAdopted.Spec.Resources))
	}
	reF := reAdopted.Spec.Resources[0].Fields["assumeRolePolicy"]
	if reF.Template != "trust-policy" || reF.Raw != "" {
		t.Fatalf("round-trip failed: expected Template == 'trust-policy' and Raw == '', got %+v", reF)
	}
}

func TestAdopt_TemplateIncludeUndefinedReported(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xroles.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XRole
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
              name: main-role
            spec:
              forProvider:
                assumeRolePolicy: '{{ include "undefined-policy" . }}'
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-iam:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	f := r.Fields["assumeRolePolicy"]
	if f.Template != "" {
		t.Errorf("expected assumeRolePolicy.Template == '', got %q", f.Template)
	}
	if !strings.Contains(f.Raw, "undefined-policy") {
		t.Errorf("expected assumeRolePolicy.Raw to preserve raw expression, got %q", f.Raw)
	}
	if report == nil || !report.HasTrueLoss() {
		t.Fatalf("expected loss report for undefined template, got: %+v", report)
	}
	foundLoss := false
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "assumeRolePolicy") && strings.Contains(d.Reason, "undefined-policy") {
			foundLoss = true
			break
		}
	}
	if !foundLoss {
		t.Fatalf("expected drop mentioning undefined-policy on assumeRolePolicy, got drops: %+v", report.Drops)
	}
}

func TestAdopt_TemplateIncludeOnNativeResourcePreservesRaw(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xconfigs.k8s.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XConfig
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
            {{- define "cm-data" }}
            key: value
            {{- end }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: main-cm
            data:
              config: '{{ include "cm-data" . }}'
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report != nil && report.HasTrueLoss() {
		t.Fatalf("unexpected loss: %+v", report.Drops)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	f := r.Fields["data[config]"]
	if f.Template != "" {
		t.Errorf("expected native resource field Template == '', got %q", f.Template)
	}
	if !strings.Contains(f.Raw, "cm-data") {
		t.Errorf("expected native resource field Raw to contain 'cm-data', got %q", f.Raw)
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate failed: %v", err)
	}
}

func TestAdopt_YAMLParamNames(t *testing.T) {
	t.Run("Pipeline", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xservices.example.org
spec:
  group: example.org
  names:
    kind: XService
    plural: xservices
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
                "on":
                  type: boolean
                "off":
                  type: boolean
                "yes":
                  type: boolean
                "no":
                  type: boolean
                "y":
                  type: string
                "n":
                  type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xservices.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XService
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: test-bucket
            spec:
              forProvider:
                "on": {{ $spec.on }}
                "off": {{ $spec.off }}
                "yes": {{ $spec.yes }}
                "no": {{ $spec.no }}
                "y": {{ $spec.y }}
                "n": {{ $spec.n }}
`
		bp, report, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(report.Drops) > 0 {
			t.Fatalf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
		}

		for _, name := range []string{"on", "off", "yes", "no", "y", "n"} {
			if _, ok := bp.Spec.XRD.Parameters[name]; !ok {
				t.Errorf("expected parameter %q in XRD parameters", name)
			}
		}

		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		res := bp.Spec.Resources[0]
		for _, name := range []string{"on", "off", "yes", "no", "y", "n"} {
			if f, ok := res.Fields[name]; !ok || f.From != "params."+name {
				t.Errorf("field %q = %+v, want From: params.%s", name, f, name)
			}
		}
	})

	t.Run("ClassicPatches", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xservices.example.org
spec:
  group: example.org
  names:
    kind: XService
    plural: xservices
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
                  properties:
                    "on":
                      type: boolean
                    "off":
                      type: boolean
                    "yes":
                      type: boolean
                    "no":
                      type: boolean
                    "y":
                      type: string
                    "n":
                      type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xservices.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XService
  resources:
    - name: test-bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider: {}
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.on
          toFieldPath: spec.forProvider.on
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.off
          toFieldPath: spec.forProvider.off
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.yes
          toFieldPath: spec.forProvider.yes
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.no
          toFieldPath: spec.forProvider.no
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.y
          toFieldPath: spec.forProvider.y
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.n
          toFieldPath: spec.forProvider.n
`
		bp, report, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(report.Drops) > 0 {
			t.Fatalf("expected 0 loss drops, got %d: %+v", len(report.Drops), report.Drops)
		}

		for _, name := range []string{"on", "off", "yes", "no", "y", "n"} {
			if _, ok := bp.Spec.XRD.Parameters[name]; !ok {
				t.Errorf("expected parameter %q in XRD parameters", name)
			}
		}

		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		res := bp.Spec.Resources[0]
		for _, name := range []string{"on", "off", "yes", "no", "y", "n"} {
			if f, ok := res.Fields[name]; !ok || f.From != "params."+name {
				t.Errorf("field %q = %+v, want From: params.%s", name, f, name)
			}
		}
	})
}

func TestAdopt_InitProvider(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-initprovider
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
  pipeline:
  - step: patch-and-transform
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: bucket
        base:
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          spec:
            forProvider:
              region: us-east-1
            initProvider:
              tags:
                Environment: dev
        patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.tags
          toFieldPath: spec.initProvider.tags
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	// res.Envelope must NOT contain initProvider or initProvider.tags
	for k := range res.Envelope {
		if strings.HasPrefix(k, "initProvider") {
			t.Errorf("res.Envelope contains unexpected initProvider entry: %q", k)
		}
	}

	// report must have true loss
	if !report.HasTrueLoss() {
		t.Error("expected report.HasTrueLoss() to be true")
	}

	// report must record the dropped base initProvider field and the unsupported patch
	var droppedBase, droppedPatch bool
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.initProvider.tags" {
			droppedBase = true
		}
		if strings.Contains(d.Path, "spec.initProvider") || strings.Contains(d.Reason, "initProvider") {
			droppedPatch = true
		}
	}
	if !droppedBase {
		t.Errorf("expected drop for resource.bucket.initProvider.tags in report, got: %+v", report.Drops)
	}
	if !droppedPatch {
		t.Errorf("expected drop for initProvider patch in report, got: %+v", report.Drops)
	}

	// Validate blueprint
	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}
}

func TestAdopt_HelmSourceComment(t *testing.T) {
	manifest := `# Source: mychart/templates/composition.yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: my-comp
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
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp.Metadata.Name != "my-comp" {
		t.Errorf("expected Metadata.Name to be 'my-comp', got %q", bp.Metadata.Name)
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}

	// Also verify that a valid DNS subdomain in # Source: is accepted
	manifestValid := `# Source: valid-bp-name
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-name
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
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
`
	bpValid, _, err := Adopt([]byte(manifestValid), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bpValid.Metadata.Name != "valid-bp-name" {
		t.Errorf("expected Metadata.Name to be 'valid-bp-name', got %q", bpValid.Metadata.Name)
	}

	// Verify keywords and invalid DNS subdomains in # Source: fall back to metadata.name
	for _, invalidSource := range []string{"true", "yes", "null", "invalid_name", "-starts-with-dash", "blueprint"} {
		m := fmt.Sprintf("# Source: %s\n%s", invalidSource, manifest[strings.Index(manifest, "apiVersion"):])
		bpBad, _, err := Adopt([]byte(m), Options{})
		if err != nil {
			t.Fatalf("Adopt failed for invalid source %q: %v", invalidSource, err)
		}
		if bpBad.Metadata.Name != "my-comp" {
			t.Errorf("expected Metadata.Name for source %q to fall back to 'my-comp', got %q", invalidSource, bpBad.Metadata.Name)
		}
		if err := bpBad.Validate(); err != nil {
			t.Errorf("bpBad.Validate() failed for source %q: %v", invalidSource, err)
		}
	}

	// Verbatim reproduction from issue description
	verbatim := `# Source: mychart/templates/composition.yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: my-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XMyResource
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-auto-ready
`
	bpVerbatim, _, err := Adopt([]byte(verbatim), Options{})
	if err != nil {
		t.Fatalf("Adopt failed for verbatim repro: %v", err)
	}
	if bpVerbatim.Metadata.Name != "my-comp" {
		t.Errorf("expected Metadata.Name for verbatim repro to be 'my-comp', got %q", bpVerbatim.Metadata.Name)
	}
	if err := bpVerbatim.Validate(); err != nil {
		t.Errorf("bpVerbatim.Validate() failed: %v", err)
	}
}

func TestAdopt_DotNotationMapPatch(t *testing.T) {
	crdYAML := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  names:
    kind: Queue
    plural: queues
    categories: [managed]
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
                    tags:
                      type: object
                      additionalProperties:
                        type: string
`
	crds, err := schema.ParseCRDs([][]byte{[]byte(crdYAML)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	providerRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	if err := store.SaveCRDs(providerRef, "sha256:test", crds); err != nil {
		t.Fatalf("SaveCRDs: %v", err)
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-dot-map-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  resources:
    - name: q
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.env
          toFieldPath: spec.forProvider.tags.env
`
	bp, report, err := Adopt([]byte(manifest), Options{
		Store:    store,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// 1. bp.Spec.Resources["q"].Fields["tags[env]"] is populated with From: "params.env".
	var q *blueprint.Resource
	for i := range bp.Spec.Resources {
		if bp.Spec.Resources[i].Name == "q" {
			q = &bp.Spec.Resources[i]
			break
		}
	}
	if q == nil {
		t.Fatalf("expected resource q in bp.Spec.Resources")
	}
	f, ok := q.Fields["tags[env]"]
	if !ok {
		t.Errorf("expected q.Fields[\"tags[env]\"], got fields: %+v", q.Fields)
	} else if f.From != "params.env" {
		t.Errorf("expected q.Fields[\"tags[env]\"].From == \"params.env\", got %q", f.From)
	}

	// 2. bp.Spec.XRD.Parameters["env"] is preserved.
	if bp.Spec.XRD.Parameters == nil {
		t.Fatalf("expected bp.Spec.XRD.Parameters to be populated")
	}
	if _, ok := bp.Spec.XRD.Parameters["env"]; !ok {
		t.Errorf("expected parameter 'env' to be preserved in bp.Spec.XRD.Parameters, got: %+v", bp.Spec.XRD.Parameters)
	}

	// 3. No schema drop warning is issued for tags.env or tags[env].
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "tags") || strings.Contains(d.Reason, "tags") {
			t.Errorf("unexpected drop recorded for tags: %+v", d)
		}
	}

	// 4. Round-trip through emit.Generate
	genOut, err := emit.Generate(bp)
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}
	if !strings.Contains(string(genOut.Composition), "tags") {
		t.Errorf("expected Composition output to contain tags, got:\n%s", string(genOut.Composition))
	}
}
