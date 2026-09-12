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
// "without the XRD" entries an XRD-less adoption always records (CF-108)
// or the function package entries an adoption without functions.yaml records (CF-236).
func dropsBeyondXRDless(report *LossReport) []Drop {
	var out []Drop
	for _, d := range report.Drops {
		if strings.HasPrefix(d.Path, "xrd.parameters.") && strings.HasPrefix(d.Reason, "without the XRD") {
			continue
		}
		if strings.HasPrefix(d.Path, "pipeline.") && strings.Contains(d.Reason, "without functions.yaml") {
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

func TestAdoptPipeline_EnvironmentConfigsForcedBefore(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-after
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
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
    - step: custom-env
      functionRef:
        name: function-environment-configs
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("Adopted blueprint failed validation: %v", err)
	}
	if len(bp.Spec.Pipeline) != 1 {
		t.Fatalf("expected 1 pipeline step, got %d", len(bp.Spec.Pipeline))
	}
	if bp.Spec.Pipeline[0].Position != "before" {
		t.Errorf("expected Position: before for function-environment-configs, got %q", bp.Spec.Pipeline[0].Position)
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
	var compYAML, fnsYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
		} else if strings.Contains(o.Path, "functions") {
			fnsYAML = o.Body
		}
	}
	if len(compYAML) == 0 {
		t.Fatalf("no composition generated in outputs")
	}
	reAdoptManifest := compYAML
	if len(fnsYAML) > 0 {
		reAdoptManifest = append(reAdoptManifest, []byte("\n---\n")...)
		reAdoptManifest = append(reAdoptManifest, fnsYAML...)
	}
	reAdopted, reReport, err := Adopt(reAdoptManifest, Options{
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
		t.Fatalf("no composition generated")
	}
	if !strings.Contains(string(compYAML), "tags") {
		t.Errorf("expected Composition output to contain tags, got:\n%s", string(compYAML))
	}
}

func TestCF236_AdoptFunctionPackageLossReport(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: us-east-1
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	// 1. Adopting composition alone (without functions.yaml) must record an unrecovered
	// function package in LossReport, naming the step and the assumed default package.
	_, report, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report == nil || !report.IsLossy() {
		t.Fatalf("expected loss report when adopting composition without functions.yaml, got nil or empty")
	}

	var foundDrop *Drop
	for _, d := range report.Drops {
		if d.Path == "pipeline.auto-ready" {
			foundDrop = &d
			break
		}
	}
	if foundDrop == nil {
		t.Fatalf("expected drop with path 'pipeline.auto-ready' in LossReport, got drops: %+v", report.Drops)
	}
	if !strings.Contains(foundDrop.Reason, "without functions.yaml") {
		t.Errorf("expected drop reason to mention 'without functions.yaml', got: %q", foundDrop.Reason)
	}
	if !strings.Contains(foundDrop.Reason, "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0") {
		t.Errorf("expected drop reason to name assumed package 'xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0', got: %q", foundDrop.Reason)
	}

	// 2. Adopting with functions.yaml (specifying the pinned package) must recover the package
	// and not record a loss for the function package.
	multiDocManifest := compYAML + `---
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-auto-ready
spec:
  package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.1
`
	_, reportWithFn, err := Adopt([]byte(multiDocManifest), Options{})
	if err != nil {
		t.Fatalf("Adopt with functions.yaml failed: %v", err)
	}
	if reportWithFn != nil {
		for _, d := range reportWithFn.Drops {
			if strings.HasPrefix(d.Path, "pipeline.") {
				t.Errorf("unexpected pipeline loss when functions.yaml provided: %+v", d)
			}
		}
	}
}

func TestAdoptNativeResourceSpecPatchFields(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-native-deployment
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: deployment
      base:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          replicas: 1
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
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.replicas
          toFieldPath: spec.replicas
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.image
          toFieldPath: spec.template.spec.containers[0].image
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if len(res.Envelope) > 0 {
		t.Errorf("expected Envelope to be nil or empty, got: %+v", res.Envelope)
	}
	if got := res.Fields["spec.replicas"].From; got != "params.replicas" {
		t.Errorf("spec.replicas From = %q, want %q", got, "params.replicas")
	}
	if got := res.Fields["spec.template.spec.containers[0].image"].From; got != "params.image" {
		t.Errorf("spec.template.spec.containers[0].image From = %q, want %q", got, "params.image")
	}

	if got := res.Fields["spec.template.spec.containers[0].name"].Value; got != "web" {
		t.Errorf("spec.template.spec.containers[0].name Value = %q, want %q", got, "web")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}

	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds() failed: %v", err)
	}
	outputs, err := emit.Generate(bp, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate() failed: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatal("emit.Generate() produced no composition output")
	}
	compStr := string(compYAML)
	if !strings.Contains(compStr, "{{ $spec.replicas }}") {
		t.Errorf("expected emitted composition to contain {{ $spec.replicas }}, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "{{ $spec.image | quote }}") {
		t.Errorf("expected emitted composition to contain {{ $spec.image | quote }}, got:\n%s", compStr)
	}
}

func TestAdoptNativeResourceSimpleSpecPatchField(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-native-scalar
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: deployment
      base:
        apiVersion: apps/v1
        kind: Deployment
        spec:
          replicas: 1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.replicas
          toFieldPath: spec.replicas
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if len(res.Envelope) > 0 {
		t.Errorf("expected Envelope to be nil or empty, got: %+v", res.Envelope)
	}
	if got := res.Fields["spec.replicas"].From; got != "params.replicas" {
		t.Errorf("spec.replicas From = %q, want %q", got, "params.replicas")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}

	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds() failed: %v", err)
	}
	outputs, err := emit.Generate(bp, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate() failed: %v", err)
	}
	var compYAML []byte
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			compYAML = o.Body
			break
		}
	}
	if len(compYAML) == 0 {
		t.Fatal("emit.Generate() produced no composition output")
	}
	compStr := string(compYAML)
	if !strings.Contains(compStr, "{{ $spec.replicas }}") {
		t.Errorf("expected emitted composition to contain {{ $spec.replicas }}, got:\n%s", compStr)
	}
}

func TestAdoptClassicComposition_NestedObjectParameters(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-nested
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XNetwork
  resources:
    - name: vpc-res
      base:
        apiVersion: ec2.aws.upbound.io/v1beta1
        kind: VPC
        spec:
          forProvider:
            cidrBlock: 10.0.0.0/16
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.network.vpc.id
          toFieldPath: spec.forProvider.vpcId
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
	r := bp.Spec.Resources[0]
	if got := r.Fields["vpcId"].From; got != "params.network.vpc.id" {
		t.Errorf("vpcId From = %q, want %q", got, "params.network.vpc.id")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestAdoptGoTemplate_NestedObjectParameters(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: gotemplate-nested
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
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: Subnet
            metadata:
              name: app-subnet
            spec:
              forProvider:
                vpcId: {{ $spec.network.vpc.id }}
                cidrBlock: {{ $spec.network.vpc.cidr }}
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if d := dropsBeyondXRDless(report); len(d) > 0 {
		t.Errorf("expected no loss beyond the XRD-less parameter report, got: %+v", d)
	}

	networkParam, ok := bp.Spec.XRD.Parameters["network"]
	if !ok {
		t.Fatalf("expected network parameter to exist")
	}
	if networkParam.Type != "object" {
		t.Errorf("network parameter type = %q, want object", networkParam.Type)
	}
	vpcProp, ok := networkParam.Properties["vpc"]
	if !ok {
		t.Fatalf("expected network.Properties[vpc] to exist")
	}
	if vpcProp.Type != "object" {
		t.Errorf("network.vpc type = %q, want object", vpcProp.Type)
	}
	idProp, ok := vpcProp.Properties["id"]
	if !ok {
		t.Errorf("expected network.vpc.Properties[id] to exist")
	} else if idProp.Type != "string" {
		t.Errorf("network.vpc.id type = %q, want string", idProp.Type)
	}
	cidrProp, ok := vpcProp.Properties["cidr"]
	if !ok {
		t.Errorf("expected network.vpc.Properties[cidr] to exist")
	} else if cidrProp.Type != "string" {
		t.Errorf("network.vpc.cidr type = %q, want string", cidrProp.Type)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if got := r.Fields["vpcId"].From; got != "params.network.vpc.id" {
		t.Errorf("vpcId From = %q, want %q", got, "params.network.vpc.id")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestAdoptGoTemplate_NestedObjectParameters_OrphanPruning(t *testing.T) {
	crdYAML := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: queues.sqs.aws.upbound.io
spec:
  group: sqs.aws.upbound.io
  names:
    kind: Queue
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
  name: test-nested-param-pruning
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
                region: {{ $spec.network.vpc.region }}
                badField: {{ $spec.network.vpc.badField }}
                anotherBad: {{ $spec.isolated.deep.unknown }}
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

	if _, ok := dropsByPath["resource.main-queue.fields.badField"]; !ok {
		t.Errorf("expected drop for wired unknown field badField, got drops: %+v", report.Drops)
	}
	if _, ok := dropsByPath["xrd.parameters.network.properties.vpc.properties.badField"]; !ok {
		t.Errorf("expected drop for orphaned property network.vpc.badField, got drops: %+v", report.Drops)
	}

	network, ok := bp.Spec.XRD.Parameters["network"]
	if !ok {
		t.Fatalf("expected network parameter to exist")
	}
	vpc, ok := network.Properties["vpc"]
	if !ok {
		t.Fatalf("expected network.Properties[vpc] to exist")
	}
	if _, ok := vpc.Properties["badField"]; ok {
		t.Errorf("network.vpc.Properties[badField] should be pruned")
	}
	if _, ok := vpc.Properties["region"]; !ok {
		t.Errorf("network.vpc.Properties[region] should be preserved")
	}

	if _, ok := bp.Spec.XRD.Parameters["isolated"]; ok {
		t.Errorf("isolated parameter should have been completely pruned, but remains")
	}
	if _, ok := dropsByPath["xrd.parameters.isolated"]; !ok {
		t.Errorf("expected drop for orphaned parameter isolated, got drops: %+v", report.Drops)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("adopted blueprint failed validation: %v", err)
	}
}

func TestCF274_AdoptWholeObjectParamDrop(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-obj-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: spec.forProvider.objectLockEnabled
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config.region
          toFieldPath: spec.forProvider.region
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	// 3. Verify that res.Fields["objectLockEnabled"] is not wired to params.config.
	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.config" {
		t.Errorf("expected objectLockEnabled not to be wired to params.config, got: %+v", f)
	}

	// 4. Verify that res.Fields["region"] is wired to params.config.region.
	if got := res.Fields["region"].From; got != "params.config.region" {
		t.Errorf("res.Fields[\"region\"].From = %q, want %q", got, "params.config.region")
	}

	// 5. Verify that report.Drops contains an entry recording the whole-object patch drop.
	foundDrop := false
	expectedReason := `unsupported whole-object parameter wire from "spec.parameters.config" to "spec.forProvider.objectLockEnabled"; wire individual object members instead`
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0]" && d.Reason == expectedReason {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop on resource.bucket.patches[0] with reason %q, got drops: %+v", expectedReason, report.Drops)
	}

	// 6. Verify that bp.Validate() succeeds.
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestCF274_AdoptWholeObjectParamDrop_WithXRD(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.example.org
spec:
  group: example.org
  names:
    kind: XApp
    plural: xapps
  claimNames:
    kind: App
    plural: apps
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
                    config:
                      type: object
                      properties:
                        region:
                          type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-obj-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: spec.forProvider.objectLockEnabled
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.config" {
		t.Errorf("expected objectLockEnabled not to be wired to params.config, got: %+v", f)
	}

	foundDrop := false
	expectedReason := `unsupported whole-object parameter wire from "spec.parameters.config" to "spec.forProvider.objectLockEnabled"; wire individual object members instead`
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0]" && d.Reason == expectedReason {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop on resource.bucket.patches[0] with reason %q, got drops: %+v", expectedReason, report.Drops)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestCF274_AdoptWholeObjectParamDrop_PatchSetAndEnvelopeAndAnnotations(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-patchset-obj
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  patchSets:
    - name: common-patches
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: spec.forProvider.objectLockEnabled
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: spec.providerConfigRef.name
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: metadata.annotations["example.com/config"]
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config.tier
          toFieldPath: spec.forProvider.tier
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: PatchSet
          patchSetName: common-patches
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.config" {
		t.Errorf("expected objectLockEnabled not to be wired to params.config, got: %+v", f)
	}
	if f, exists := res.Envelope["providerConfigRef.name"]; exists && f.From == "params.config" {
		t.Errorf("expected providerConfigRef.name not to be wired to params.config, got: %+v", f)
	}
	if f, exists := res.Annotations["example.com/config"]; exists && f.From == "params.config" {
		t.Errorf("expected example.com/config annotation not to be wired to params.config, got: %+v", f)
	}
	if got := res.Fields["tier"].From; got != "params.config.tier" {
		t.Errorf("res.Fields[\"tier\"].From = %q, want %q", got, "params.config.tier")
	}

	if len(report.Drops) < 3 {
		t.Errorf("expected at least 3 drops, got %d: %+v", len(report.Drops), report.Drops)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestCF274_AdoptWholeObjectParamDrop_ReverseOrder(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-obj-patch-rev
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config.region
          toFieldPath: spec.forProvider.region
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.config
          toFieldPath: spec.forProvider.objectLockEnabled
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.config" {
		t.Errorf("expected objectLockEnabled not to be wired to params.config, got: %+v", f)
	}
	if got := res.Fields["region"].From; got != "params.config.region" {
		t.Errorf("res.Fields[\"region\"].From = %q, want %q", got, "params.config.region")
	}

	foundDrop := false
	expectedReason := `unsupported whole-object parameter wire from "spec.parameters.config" to "spec.forProvider.objectLockEnabled"; wire individual object members instead`
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[1]" && d.Reason == expectedReason {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop on resource.bucket.patches[1] with reason %q, got drops: %+v", expectedReason, report.Drops)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestCF274_AdoptScalarParameter_NotDropped(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-scalar-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.region
          toFieldPath: spec.forProvider.region
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	if got := res.Fields["region"].From; got != "params.region" {
		t.Errorf("res.Fields[\"region\"].From = %q, want %q", got, "params.region")
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestCF277_AdoptDotSpecDotObservedSyntax(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if $.spec.enabled }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: conditioned
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          {{- range $i := until (int $.spec.count) }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: replicated
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: wired
          spec:
            forProvider:
              region: "{{ $.spec.region }}"
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var conditioned, replicated, wired *blueprint.Resource
	for i := range bp.Spec.Resources {
		switch bp.Spec.Resources[i].Name {
		case "conditioned":
			conditioned = &bp.Spec.Resources[i]
		case "replicated":
			replicated = &bp.Spec.Resources[i]
		case "wired":
			wired = &bp.Spec.Resources[i]
		}
	}

	if conditioned == nil {
		t.Fatalf("conditioned resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	if conditioned.When != "params.enabled" {
		t.Errorf("conditioned.When = %q, want %q", conditioned.When, "params.enabled")
	}

	if replicated == nil {
		t.Fatalf("replicated resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	if replicated.ForEach != "params.count" {
		t.Errorf("replicated.ForEach = %q, want %q", replicated.ForEach, "params.count")
	}

	if wired == nil {
		t.Fatalf("wired resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	if got := wired.Fields["region"].From; got != "params.region" {
		t.Errorf("wired.Fields[\"region\"].From = %q, want %q (field: %+v)", got, "params.region", wired.Fields["region"])
	}

	for _, p := range []string{"enabled", "count", "region"} {
		if _, ok := bp.Spec.XRD.Parameters[p]; !ok {
			t.Errorf("expected parameter %q in bp.Spec.XRD.Parameters, got: %+v (report drops: %+v)", p, bp.Spec.XRD.Parameters, report.Drops)
		}
	}

	manifestObs := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-obs
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if $.observed.composite.resource.spec.enabled }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: conditioned
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          {{- range $i := until (int $.observed.composite.resource.spec.count) }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: replicated
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: wired
          spec:
            forProvider:
              region: "{{ $.observed.composite.resource.spec.region }}"
`

	bpObs, reportObs, err := Adopt([]byte(manifestObs), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var conditionedObs, replicatedObs, wiredObs *blueprint.Resource
	for i := range bpObs.Spec.Resources {
		switch bpObs.Spec.Resources[i].Name {
		case "conditioned":
			conditionedObs = &bpObs.Spec.Resources[i]
		case "replicated":
			replicatedObs = &bpObs.Spec.Resources[i]
		case "wired":
			wiredObs = &bpObs.Spec.Resources[i]
		}
	}

	if conditionedObs == nil {
		t.Fatalf("conditioned resource not found in adopted blueprint: %+v", bpObs.Spec.Resources)
	}
	if conditionedObs.When != "params.enabled" {
		t.Errorf("conditionedObs.When = %q, want %q", conditionedObs.When, "params.enabled")
	}

	if replicatedObs == nil {
		t.Fatalf("replicated resource not found in adopted blueprint: %+v", bpObs.Spec.Resources)
	}
	if replicatedObs.ForEach != "params.count" {
		t.Errorf("replicatedObs.ForEach = %q, want %q", replicatedObs.ForEach, "params.count")
	}

	if wiredObs == nil {
		t.Fatalf("wired resource not found in adopted blueprint: %+v", bpObs.Spec.Resources)
	}
	if got := wiredObs.Fields["region"].From; got != "params.region" {
		t.Errorf("wiredObs.Fields[\"region\"].From = %q, want %q (field: %+v)", got, "params.region", wiredObs.Fields["region"])
	}

	for _, p := range []string{"enabled", "count", "region"} {
		if _, ok := bpObs.Spec.XRD.Parameters[p]; !ok {
			t.Errorf("expected parameter %q in bpObs.Spec.XRD.Parameters, got: %+v (report drops: %+v)", p, bpObs.Spec.XRD.Parameters, reportObs.Drops)
		}
	}
}

func TestCF277_AdoptDotSpecEqualityGuards(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-eq
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if eq $.spec.tier "pro" }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: pro-bucket
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          {{- if ne $.observed.composite.resource.spec.tier "basic" }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: nonbasic-bucket
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var proBucket, nonbasicBucket *blueprint.Resource
	for i := range bp.Spec.Resources {
		switch bp.Spec.Resources[i].Name {
		case "pro-bucket":
			proBucket = &bp.Spec.Resources[i]
		case "nonbasic-bucket":
			nonbasicBucket = &bp.Spec.Resources[i]
		}
	}

	if proBucket == nil {
		t.Fatalf("pro-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if proBucket.When != "params.tier == \"pro\"" {
		t.Errorf("proBucket.When = %q, want %q", proBucket.When, "params.tier == \"pro\"")
	}

	if nonbasicBucket == nil {
		t.Fatalf("nonbasic-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if nonbasicBucket.When != "params.tier != \"basic\"" {
		t.Errorf("nonbasicBucket.When = %q, want %q", nonbasicBucket.When, "params.tier != \"basic\"")
	}

	if _, ok := bp.Spec.XRD.Parameters["tier"]; !ok {
		t.Errorf("expected parameter 'tier' in XRD parameters, got: %+v (drops: %+v)", bp.Spec.XRD.Parameters, report.Drops)
	}
}

func TestAdoptGoTemplate_GuardSplitting(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if $spec.enableCache }}
          apiVersion: redis.aws.upbound.io/v1beta1
          kind: Cluster
          metadata:
            annotations:
              crossplane.io/composition-resource-name: cache
          spec:
            forProvider:
              engine: redis
          {{- end }}
          ---
          apiVersion: apps/v1
          kind: Deployment
          metadata:
            annotations:
              crossplane.io/composition-resource-name: deployment
          spec:
            replicas: 1
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var cache, deployment *blueprint.Resource
	for i := range bp.Spec.Resources {
		switch bp.Spec.Resources[i].Name {
		case "cache":
			cache = &bp.Spec.Resources[i]
		case "deployment":
			deployment = &bp.Spec.Resources[i]
		}
	}

	if cache == nil {
		t.Fatalf("cache resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	if cache.When != "params.enableCache" {
		t.Errorf("cache.When = %q, want %q", cache.When, "params.enableCache")
	}

	if deployment == nil {
		t.Fatalf("deployment resource not found in adopted blueprint: %+v", bp.Spec.Resources)
	}
	if deployment.When != "" {
		t.Errorf("deployment.When = %q, want empty string", deployment.When)
	}

	if _, ok := bp.Spec.XRD.Parameters["enableCache"]; !ok {
		t.Errorf("expected parameter 'enableCache' in bp.Spec.XRD.Parameters, got: %+v (drops: %+v)", bp.Spec.XRD.Parameters, report.Drops)
	}

	manifestSingle := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-single
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if $spec.enableCache }}
          apiVersion: redis.aws.upbound.io/v1beta1
          kind: Cluster
          metadata:
            annotations:
              crossplane.io/composition-resource-name: cache
          spec:
            forProvider:
              engine: redis
          {{- end }}
`

	bpSingle, reportSingle, err := Adopt([]byte(manifestSingle), Options{})
	if err != nil {
		t.Fatalf("Adopt single failed: %v", err)
	}

	var cacheSingle *blueprint.Resource
	for i := range bpSingle.Spec.Resources {
		if bpSingle.Spec.Resources[i].Name == "cache" {
			cacheSingle = &bpSingle.Spec.Resources[i]
			break
		}
	}
	if cacheSingle == nil {
		t.Fatalf("cacheSingle not found in adopted blueprint: %+v", bpSingle.Spec.Resources)
	}
	if cacheSingle.When != "params.enableCache" {
		t.Errorf("cacheSingle.When = %q, want %q", cacheSingle.When, "params.enableCache")
	}
	if _, ok := bpSingle.Spec.XRD.Parameters["enableCache"]; !ok {
		t.Errorf("expected parameter 'enableCache' in bpSingle.Spec.XRD.Parameters, got: %+v (drops: %+v)", bpSingle.Spec.XRD.Parameters, reportSingle.Drops)
	}

	manifestForEach := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-foreach
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- range $i := until (int $spec.count) }}
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: replicated
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
          ---
          apiVersion: apps/v1
          kind: Deployment
          metadata:
            annotations:
              crossplane.io/composition-resource-name: deployment
          spec:
            replicas: 1
`

	bpForEach, reportForEach, err := Adopt([]byte(manifestForEach), Options{})
	if err != nil {
		t.Fatalf("Adopt forEach failed: %v", err)
	}

	var replicated, deployment2 *blueprint.Resource
	for i := range bpForEach.Spec.Resources {
		switch bpForEach.Spec.Resources[i].Name {
		case "replicated":
			replicated = &bpForEach.Spec.Resources[i]
		case "deployment":
			deployment2 = &bpForEach.Spec.Resources[i]
		}
	}

	if replicated == nil {
		t.Fatalf("replicated resource not found in adopted blueprint: %+v", bpForEach.Spec.Resources)
	}
	if replicated.ForEach != "params.count" {
		t.Errorf("replicated.ForEach = %q, want %q", replicated.ForEach, "params.count")
	}

	if deployment2 == nil {
		t.Fatalf("deployment resource not found in adopted blueprint: %+v", bpForEach.Spec.Resources)
	}
	if deployment2.ForEach != "" {
		t.Errorf("deployment2.ForEach = %q, want empty string", deployment2.ForEach)
	}

	if _, ok := bpForEach.Spec.XRD.Parameters["count"]; !ok {
		t.Errorf("expected parameter 'count' in bpForEach.Spec.XRD.Parameters, got: %+v (drops: %+v)", bpForEach.Spec.XRD.Parameters, reportForEach.Drops)
	}

	manifestForEachSingle := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-single-foreach
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- range $i := until (int $spec.count) }}
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: replicated
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
`

	bpForEachSingle, reportForEachSingle, err := Adopt([]byte(manifestForEachSingle), Options{})
	if err != nil {
		t.Fatalf("Adopt forEach single failed: %v", err)
	}

	var replicatedSingle *blueprint.Resource
	for i := range bpForEachSingle.Spec.Resources {
		if bpForEachSingle.Spec.Resources[i].Name == "replicated" {
			replicatedSingle = &bpForEachSingle.Spec.Resources[i]
			break
		}
	}
	if replicatedSingle == nil {
		t.Fatalf("replicatedSingle not found in adopted blueprint: %+v", bpForEachSingle.Spec.Resources)
	}
	if replicatedSingle.ForEach != "params.count" {
		t.Errorf("replicatedSingle.ForEach = %q, want %q", replicatedSingle.ForEach, "params.count")
	}
	if _, ok := bpForEachSingle.Spec.XRD.Parameters["count"]; !ok {
		t.Errorf("expected parameter 'count' in bpForEachSingle.Spec.XRD.Parameters, got: %+v (drops: %+v)", bpForEachSingle.Spec.XRD.Parameters, reportForEachSingle.Drops)
	}
}

func TestAdoptObservedStatus(t *testing.T) {
	t.Run("AcceptanceScenario", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
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
              name: role
              annotations:
                crossplane.io/composition-resource-name: role
            spec:
              forProvider:
                assumeRolePolicy: '{}'
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: bucket
              annotations:
                crossplane.io/composition-resource-name: bucket
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: queue
              annotations:
                crossplane.io/composition-resource-name: queue
            spec:
              forProvider:
                region: us-east-1
                redrivePolicy: '{{ (index .observed.resources "bucket").resource.status.atProvider.arn }}'
            ---
            apiVersion: v1
            kind: ServiceAccount
            metadata:
              name: sa
              annotations:
                crossplane.io/composition-resource-name: sa
                eks.amazonaws.com/role-arn: '{{ (index .observed.resources "role").resource.status.atProvider.arn }}'
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}

		queue := bp.ResourceNamed("queue")
		if queue == nil {
			t.Fatalf("resource queue not found in adopted blueprint")
		}
		if got := queue.Fields["redrivePolicy"]; got.From != "resources.bucket.status.atProvider.arn" || got.Raw != "" {
			t.Errorf("queue.Fields[redrivePolicy] = %+v, want From: resources.bucket.status.atProvider.arn, Raw empty", got)
		}

		sa := bp.ResourceNamed("sa")
		if sa == nil {
			t.Fatalf("resource sa not found in adopted blueprint")
		}
		if got := sa.Annotations["eks.amazonaws.com/role-arn"]; got.From != "resources.role.status.atProvider.arn" || got.Raw != "" {
			t.Errorf("sa.Annotations[eks.amazonaws.com/role-arn] = %+v, want From: resources.role.status.atProvider.arn, Raw empty", got)
		}

		// Multi-engine check: verify that adopting structured status wires allows KCL validation/planning
		bp.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
		for _, r := range bp.Spec.Resources {
			for fName, f := range r.Fields {
				if strings.Contains(f.Raw, "{{") {
					t.Errorf("resource %q field %q leaked Go-template raw string %q", r.Name, fName, f.Raw)
				}
			}
			for aName, a := range r.Annotations {
				if strings.Contains(a.Raw, "{{") {
					t.Errorf("resource %q annotation %q leaked Go-template raw string %q", r.Name, aName, a.Raw)
				}
			}
			for eName, e := range r.Envelope {
				if strings.Contains(e.Raw, "{{") {
					t.Errorf("resource %q envelope %q leaked Go-template raw string %q", r.Name, eName, e.Raw)
				}
			}
		}
	})

	t.Run("PrefixVariants", func(t *testing.T) {
		tests := []struct {
			name   string
			prefix string // e.g. ".observed.resources", "$.observed.resources", "$observed.resources"
		}{
			{name: "DotObservedResources", prefix: ".observed.resources"},
			{name: "DollarDotObservedResources", prefix: "$.observed.resources"},
			{name: "DollarObservedResources", prefix: "$observed.resources"},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: target-bucket
              annotations:
                crossplane.io/composition-resource-name: target-bucket
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: test-queue
              annotations:
                crossplane.io/composition-resource-name: test-queue
                example.com/status-wire: '{{ (index %s "target-bucket").resource.status.atProvider.arn }}'
            spec:
              forProvider:
                region: us-east-1
                redrivePolicy: '{{ (index %s "target-bucket").resource.status.atProvider.arn }}'
`, tc.prefix, tc.prefix)

				bp, _, err := Adopt([]byte(manifest), Options{})
				if err != nil {
					t.Fatalf("Adopt failed: %v", err)
				}

				res := bp.ResourceNamed("test-queue")
				if res == nil {
					t.Fatalf("test-queue not found in adopted blueprint")
				}

				if got := res.Fields["redrivePolicy"]; got.From != "resources.target-bucket.status.atProvider.arn" || got.Raw != "" {
					t.Errorf("[%s] Fields[redrivePolicy] = %+v, want From: resources.target-bucket.status.atProvider.arn, Raw empty", tc.name, got)
				}
				if got := res.Annotations["example.com/status-wire"]; got.From != "resources.target-bucket.status.atProvider.arn" || got.Raw != "" {
					t.Errorf("[%s] Annotations[example.com/status-wire] = %+v, want From: resources.target-bucket.status.atProvider.arn, Raw empty", tc.name, got)
				}
			})
		}
	})

	t.Run("DotAccessSyntax", func(t *testing.T) {
		manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: target-bucket
              annotations:
                crossplane.io/composition-resource-name: target-bucket
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: test-queue
              annotations:
                crossplane.io/composition-resource-name: test-queue
                example.com/dot-wire: '{{ .observed.resources.target_bucket.resource.status.atProvider.arn }}'
            spec:
              forProvider:
                region: us-east-1
                redrivePolicy: '{{ .observed.resources.target_bucket.resource.status.atProvider.arn }}'
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}

		res := bp.ResourceNamed("test-queue")
		if res == nil {
			t.Fatalf("test-queue not found in adopted blueprint")
		}

		if got := res.Fields["redrivePolicy"]; got.From != "resources.target-bucket.status.atProvider.arn" || got.Raw != "" {
			t.Errorf("Fields[redrivePolicy] = %+v, want From: resources.target-bucket.status.atProvider.arn", got)
		}
		if got := res.Annotations["example.com/dot-wire"]; got.From != "resources.target-bucket.status.atProvider.arn" || got.Raw != "" {
			t.Errorf("Annotations[example.com/dot-wire] = %+v, want From: resources.target-bucket.status.atProvider.arn", got)
		}
	})
}

// CF-270 (#158): cf adopt silently drops unrecognized manifests in multi-document streams without loss reporting
func TestCF270_AdoptUnhandledManifestLoss(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
type: Opaque
data:
  token: ZXhhbXBsZQ==
---
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
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report == nil {
		t.Fatalf("expected non-nil LossReport")
	}

	// 1. Verify report.Drops contains an entry identifying Secret/my-secret as an unhandled omitted manifest
	var secretDrop *Drop
	var configDrop *Drop
	for i := range report.Drops {
		if report.Drops[i].Path == "manifest.Secret/my-secret" {
			secretDrop = &report.Drops[i]
		}
		if report.Drops[i].Path == "manifest.ConfigMap/my-config" {
			configDrop = &report.Drops[i]
		}
	}
	if secretDrop == nil {
		t.Fatalf("expected drop entry for manifest.Secret/my-secret, got drops: %+v", report.Drops)
	}
	if !strings.Contains(secretDrop.Reason, "unhandled resource kind") {
		t.Errorf("expected drop reason to mention unhandled resource kind, got: %q", secretDrop.Reason)
	}
	if configDrop == nil {
		t.Fatalf("expected drop entry for manifest.ConfigMap/my-config, got drops: %+v", report.Drops)
	}
	if !strings.Contains(configDrop.Reason, "unhandled resource kind") {
		t.Errorf("expected drop reason to mention unhandled resource kind, got: %q", configDrop.Reason)
	}
	if !report.HasTrueLoss() {
		t.Errorf("expected report.HasTrueLoss() == true for unhandled manifest drop")
	}

	// 2. Verify that the adopted blueprint comments include # adopt: dropped manifest.Secret/my-secret
	outBytes, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	outStr := string(outBytes)
	if !strings.Contains(outStr, "# adopt: dropped manifest.Secret/my-secret") {
		t.Errorf("expected adopted blueprint comments to include '# adopt: dropped manifest.Secret/my-secret', got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "# adopt: dropped manifest.ConfigMap/my-config") {
		t.Errorf("expected adopted blueprint comments to include '# adopt: dropped manifest.ConfigMap/my-config', got:\n%s", outStr)
	}
}

func TestCF270_AdoptUnhandledManifestWithoutName(t *testing.T) {
	manifest := `
apiVersion: v1
kind: Secret
type: Opaque
---
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
`

	_, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	var secretDrop *Drop
	for i := range report.Drops {
		if report.Drops[i].Path == "manifest.Secret" {
			secretDrop = &report.Drops[i]
			break
		}
	}
	if secretDrop == nil {
		t.Fatalf("expected drop entry for manifest.Secret, got drops: %+v", report.Drops)
	}
}

func TestCF257_AdoptMultiCompositionStreamAmbiguousError(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-a
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResourceA
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: res-a
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider:
                  region: us-east-1
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-b
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResourceB
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: res-b
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider:
                  region: us-west-2
`
	_, _, err := Adopt([]byte(manifest), Options{})
	if err == nil {
		t.Fatalf("expected error when adopting multi-composition stream without target selector, got nil")
	}
	if !strings.Contains(err.Error(), "comp-a") || !strings.Contains(err.Error(), "comp-b") {
		t.Errorf("expected error to identify 'comp-a' and 'comp-b', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ambiguous") && !strings.Contains(err.Error(), "multi-composition") {
		t.Errorf("expected error to report ambiguous multi-composition input, got: %v", err)
	}
}

func TestCF257_AdoptMultiCompositionStreamSelectedTarget(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-a
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResourceA
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: res-a
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider:
                  region: us-east-1
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-b
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResourceB
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: res-b
            base:
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              spec:
                forProvider:
                  region: us-west-2
`
	// 1. Select comp-a
	bpA, reportA, err := Adopt([]byte(manifest), Options{TargetComposition: "comp-a"})
	if err != nil {
		t.Fatalf("Adopt with TargetComposition comp-a failed: %v", err)
	}
	if bpA.Metadata.Name != "comp-a" {
		t.Errorf("expected bp name 'comp-a', got %q", bpA.Metadata.Name)
	}
	if len(bpA.Spec.Resources) != 1 || bpA.Spec.Resources[0].Name != "res-a" {
		t.Errorf("expected 1 resource 'res-a', got %+v", bpA.Spec.Resources)
	}
	if reportA == nil || !reportA.HasTrueLoss() {
		t.Errorf("expected report to record true loss for omitted comp-b")
	}
	foundDropB := false
	for _, d := range reportA.Drops {
		if d.Path == "manifest.Composition/comp-b" {
			foundDropB = true
			if !strings.Contains(d.Reason, "omitted") {
				t.Errorf("expected drop reason to mention omitted, got %q", d.Reason)
			}
			break
		}
	}
	if !foundDropB {
		t.Errorf("expected drop entry for manifest.Composition/comp-b, got drops: %+v", reportA.Drops)
	}

	// 2. Select comp-b using TargetComposition
	bpB, reportB, err := Adopt([]byte(manifest), Options{TargetComposition: "comp-b"})
	if err != nil {
		t.Fatalf("Adopt with TargetComposition comp-b failed: %v", err)
	}
	if bpB.Metadata.Name != "comp-b" {
		t.Errorf("expected bp name 'comp-b', got %q", bpB.Metadata.Name)
	}
	if len(bpB.Spec.Resources) != 1 || bpB.Spec.Resources[0].Name != "res-b" {
		t.Errorf("expected 1 resource 'res-b', got %+v", bpB.Spec.Resources)
	}
	if reportB == nil || !reportB.HasTrueLoss() {
		t.Errorf("expected report to record true loss for omitted comp-a")
	}
	foundDropA := false
	for _, d := range reportB.Drops {
		if d.Path == "manifest.Composition/comp-a" {
			foundDropA = true
			break
		}
	}
	if !foundDropA {
		t.Errorf("expected drop entry for manifest.Composition/comp-a, got drops: %+v", reportB.Drops)
	}

	outYAML, err := FormatAdoptedYAML(bpB, reportB)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped manifest.Composition/comp-a") {
		t.Errorf("expected output YAML to contain comment '# adopt: dropped manifest.Composition/comp-a', got:\n%s", string(outYAML))
	}

	// 3. Select non-existent comp-c
	_, _, err = Adopt([]byte(manifest), Options{TargetComposition: "comp-c"})
	if err == nil {
		t.Fatalf("expected error for non-existent composition comp-c, got nil")
	}
	if !strings.Contains(err.Error(), "comp-c") {
		t.Errorf("expected error to mention 'comp-c', got: %v", err)
	}
}

func TestCF290_AdoptMultiXRDStreamMatchingAndLossReport(t *testing.T) {
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
                properties:
                  dbStorage:
                    type: integer
---
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xothers.example.org
spec:
  group: example.org
  names:
    kind: XOther
    plural: xothers
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
                  unrelatedKey:
                    type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-database
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XDatabase
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// 1. Verify blueprint binds XDatabase's parameter (dbStorage) and plural (xdatabases)
	if bp.Spec.XRD.Kind != "XDatabase" {
		t.Errorf("expected XRD kind XDatabase, got %q", bp.Spec.XRD.Kind)
	}
	if bp.Spec.XRD.Plural != "xdatabases" {
		t.Errorf("expected XRD plural xdatabases, got %q", bp.Spec.XRD.Plural)
	}
	if _, ok := bp.Spec.XRD.Parameters["dbStorage"]; !ok {
		t.Errorf("expected parameter dbStorage from XDatabase XRD to be bound")
	}

	// 2. Verify unrelatedKey and xothers from XOther XRD are NOT bound
	if _, ok := bp.Spec.XRD.Parameters["unrelatedKey"]; ok {
		t.Errorf("unrelatedKey from non-matching XRD must not be bound")
	}
	if bp.Spec.XRD.Plural == "xothers" {
		t.Errorf("plural xothers from non-matching XRD must not be bound")
	}

	// 3. Verify LossReport records the dropped/unmatched XRD
	if report == nil {
		t.Fatalf("expected non-nil LossReport")
	}
	var foundOtherDrop bool
	for _, d := range report.Drops {
		if d.Path == "manifest.CompositeResourceDefinition/xothers.example.org" || d.Path == "CompositeResourceDefinition/xothers.example.org" {
			foundOtherDrop = true
			if !strings.Contains(d.Reason, "unmatched XRD") && !strings.Contains(d.Reason, "omitted") {
				t.Errorf("expected drop reason to indicate unmatched/omitted XRD, got %q", d.Reason)
			}
			break
		}
	}
	if !foundOtherDrop {
		t.Errorf("expected drop entry for manifest.CompositeResourceDefinition/xothers.example.org in LossReport, got drops: %+v", report.Drops)
	}

	// Verify XDatabase was NOT dropped
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "xdatabases.example.org") {
			t.Errorf("matching XRD xdatabases.example.org must not be recorded as dropped, got drop: %+v", d)
		}
	}

	outYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped manifest.CompositeResourceDefinition/xothers.example.org") {
		t.Errorf("expected output YAML to contain comment '# adopt: dropped manifest.CompositeResourceDefinition/xothers.example.org', got:\n%s", string(outYAML))
	}
}

func TestCF290_AdoptMultiXRDStreamReverseOrder(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xothers.example.org
spec:
  group: example.org
  names:
    kind: XOther
    plural: xothers
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
                  unrelatedKey:
                    type: string
---
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
                properties:
                  dbStorage:
                    type: integer
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-database
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XDatabase
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if bp.Spec.XRD.Kind != "XDatabase" {
		t.Errorf("expected XRD kind XDatabase, got %q", bp.Spec.XRD.Kind)
	}
	if bp.Spec.XRD.Plural != "xdatabases" {
		t.Errorf("expected XRD plural xdatabases, got %q", bp.Spec.XRD.Plural)
	}
	if _, ok := bp.Spec.XRD.Parameters["dbStorage"]; !ok {
		t.Errorf("expected parameter dbStorage from XDatabase XRD to be bound")
	}
	if _, ok := bp.Spec.XRD.Parameters["unrelatedKey"]; ok {
		t.Errorf("unrelatedKey from non-matching XRD must not be bound")
	}

	if report == nil {
		t.Fatalf("expected non-nil LossReport")
	}
	var foundOtherDrop bool
	for _, d := range report.Drops {
		if d.Path == "manifest.CompositeResourceDefinition/xothers.example.org" || d.Path == "CompositeResourceDefinition/xothers.example.org" {
			foundOtherDrop = true
			break
		}
	}
	if !foundOtherDrop {
		t.Errorf("expected drop entry for manifest.CompositeResourceDefinition/xothers.example.org in LossReport, got drops: %+v", report.Drops)
	}
}

func TestCF290_AdoptNonMatchingXRDOnlyInStream(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xothers.example.org
spec:
  group: example.org
  names:
    kind: XOther
    plural: xothers
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
                  unrelatedKey:
                    type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: comp-database
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XDatabase
  mode: Pipeline
  pipeline:
  - step: render
    functionRef:
      name: function-auto-ready
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// 1. Verify non-matching XRD is NOT bound to bp.Spec.XRD
	if bp.Spec.XRD.Kind != "XDatabase" {
		t.Errorf("expected XRD kind XDatabase, got %q", bp.Spec.XRD.Kind)
	}
	if bp.Spec.XRD.Plural == "xothers" {
		t.Errorf("plural xothers from non-matching XRD must not be bound")
	}
	if _, ok := bp.Spec.XRD.Parameters["unrelatedKey"]; ok {
		t.Errorf("unrelatedKey from non-matching XRD must not be bound")
	}

	// 2. Verify LossReport records the dropped non-matching XRD
	if report == nil {
		t.Fatalf("expected non-nil LossReport")
	}
	var foundOtherDrop bool
	for _, d := range report.Drops {
		if d.Path == "manifest.CompositeResourceDefinition/xothers.example.org" || d.Path == "CompositeResourceDefinition/xothers.example.org" {
			foundOtherDrop = true
			if !strings.Contains(d.Reason, "unmatched XRD") && !strings.Contains(d.Reason, "omitted") {
				t.Errorf("expected drop reason to indicate unmatched/omitted XRD, got %q", d.Reason)
			}
			break
		}
	}
	if !foundOtherDrop {
		t.Errorf("expected drop entry for manifest.CompositeResourceDefinition/xothers.example.org in LossReport, got drops: %+v", report.Drops)
	}

	outYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped manifest.CompositeResourceDefinition/xothers.example.org") {
		t.Errorf("expected output YAML to contain comment '# adopt: dropped manifest.CompositeResourceDefinition/xothers.example.org', got:\n%s", string(outYAML))
	}
}

func TestCF296_AdoptEnvironmentWhenComparisonQuotes(t *testing.T) {
	t.Run("eq basic", func(t *testing.T) {
		manifest := []byte(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-when-comp
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
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
            {{- if eq $env.stage "prod" }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              env: "prod"
            {{- end }}
`)

		bp, _, err := Adopt(manifest, Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		wantWhen := `env.stage == "prod"`
		if bp.Spec.Resources[0].When != wantWhen {
			t.Errorf("resource when = %q, want %q", bp.Spec.Resources[0].When, wantWhen)
		}
	})

	t.Run("ne index default", func(t *testing.T) {
		manifest := []byte(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-when-comp-ne
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
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
            {{- if ne (default "" (index $env "stage")) "dev" }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              env: "not-dev"
            {{- end }}
`)

		bp, _, err := Adopt(manifest, Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		wantWhen := `env.stage != "dev"`
		if bp.Spec.Resources[0].When != wantWhen {
			t.Errorf("resource when = %q, want %q", bp.Spec.Resources[0].When, wantWhen)
		}
	})
}

func TestCF293_ManagedResourceWithoutForProvider(t *testing.T) {
	manifest := []byte(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
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
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: test-bucket
          spec:
            deletionPolicy: Orphan
            writeConnectionSecretToRef:
              name: test-secret
              namespace: default
`)
	bp, report, err := Adopt(manifest, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.Envelope["deletionPolicy"].Value != "Orphan" {
		t.Errorf("expected envelope deletionPolicy Orphan, got %q", res.Envelope["deletionPolicy"].Value)
	}
	if res.Envelope["writeConnectionSecretToRef.name"].Value != "test-secret" {
		t.Errorf("expected writeConnectionSecretToRef.name test-secret, got %+v", res.Envelope["writeConnectionSecretToRef.name"])
	}
	if len(res.Fields) != 0 {
		t.Errorf("expected 0 fields in Fields map, got %+v", res.Fields)
	}
	if len(report.Drops) != 0 {
		t.Errorf("expected 0 dropped fields, got %v", report.Drops)
	}
}

func TestCF297_AdoptNestedObjectParams(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-nested-obj-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.network.vpc
          toFieldPath: spec.forProvider.objectLockEnabled
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.network.vpc.id
          toFieldPath: spec.forProvider.vpcId
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	// 1. Verify that res.Fields["objectLockEnabled"] is not wired to params.network.vpc.
	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.network.vpc" {
		t.Errorf("expected objectLockEnabled not to be wired to params.network.vpc, got: %+v", f)
	}

	// 2. Verify that res.Fields["vpcId"] is wired to params.network.vpc.id.
	if got := res.Fields["vpcId"].From; got != "params.network.vpc.id" {
		t.Errorf("res.Fields[\"vpcId\"].From = %q, want %q", got, "params.network.vpc.id")
	}

	// 3. Verify that report.Drops contains an entry recording the whole-object patch drop.
	foundDrop := false
	expectedReason := `unsupported whole-object parameter wire from "spec.parameters.network.vpc" to "spec.forProvider.objectLockEnabled"; wire individual object members instead`
	for _, d := range report.Drops {
		if d.Path == "resource.bucket.patches[0]" && d.Reason == expectedReason {
			foundDrop = true
			break
		}
	}
	if !foundDrop {
		t.Errorf("expected drop on resource.bucket.patches[0] with reason %q, got drops: %+v", expectedReason, report.Drops)
	}

	// 4. Verify that bp.Validate() succeeds.
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestAdoptGoTemplate_ConditionalResources_EmptyStringComparison(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if ne $spec.customDomain "" }}
          ---
          apiVersion: cert-manager.io/v1
          kind: Certificate
          metadata:
            annotations:
              crossplane.io/composition-resource-name: custom-cert
          spec:
            dnsNames:
              - example.com
          {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.When != `params.customDomain != ""` {
		t.Errorf("res.When = %q, want %q", res.When, `params.customDomain != ""`)
	}
}

func TestAdoptGoTemplate_ConditionalResources_EmptyStringComparison_AllVariants(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
	}{
		{
			name:      "param eq empty string",
			condition: `eq $spec.customDomain ""`,
			wantWhen:  `params.customDomain == ""`,
		},
		{
			name:      "param ne empty string",
			condition: `ne $spec.customDomain ""`,
			wantWhen:  `params.customDomain != ""`,
		},
		{
			name:      "env eq empty string bare",
			condition: `eq $env.stage ""`,
			wantWhen:  `env.stage == ""`,
		},
		{
			name:      "env eq empty string hasKey",
			condition: `and (hasKey $env "stage") (eq $env.stage "")`,
			wantWhen:  `env.stage == ""`,
		},
		{
			name:      "env eq empty string index default",
			condition: `eq (default "" (index $env "stage")) ""`,
			wantWhen:  `env.stage == ""`,
		},
		{
			name:      "env ne empty string bare",
			condition: `ne $env.stage ""`,
			wantWhen:  `env.stage != ""`,
		},
		{
			name:      "env ne empty string hasKey",
			condition: `or (not (hasKey $env "stage")) (ne $env.stage "")`,
			wantWhen:  `env.stage != ""`,
		},
		{
			name:      "env ne empty string index default",
			condition: `ne (default "" (index $env "stage")) ""`,
			wantWhen:  `env.stage != ""`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if %s }}
          ---
          apiVersion: cert-manager.io/v1
          kind: Certificate
          metadata:
            annotations:
              crossplane.io/composition-resource-name: custom-cert
          spec:
            dnsNames:
              - example.com
          {{- end }}
`, tc.condition)

			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			res := bp.Spec.Resources[0]
			if res.When != tc.wantWhen {
				t.Errorf("res.When = %q, want %q", res.When, tc.wantWhen)
			}
			if err := bp.Validate(); err != nil {
				t.Errorf("bp.Validate() failed: %v", err)
			}
		})
	}
}

func TestAdoptGoTemplate_ConditionalResources_EmptyStringComparison_RoundTrip(t *testing.T) {
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	origBP := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata: blueprint.Metadata{
			Name: "test-when-roundtrip",
		},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"customDomain": {Type: "string", Required: true},
					"tier":         {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"stage":  {Type: "string"},
				"region": {Type: "string", Default: "us-east-1"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "cm-domain",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					When:     `params.customDomain != ""`,
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "domain-cm"},
					},
				},
				{
					Name:     "cm-tier",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					When:     `params.tier == ""`,
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "tier-cm"},
					},
				},
				{
					Name:     "cm-stage",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					When:     `env.stage != ""`,
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "stage-cm"},
					},
				},
				{
					Name:     "cm-region",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					When:     `env.region == ""`,
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "region-cm"},
					},
				},
			},
		},
	}

	outputs, err := emit.Generate(origBP, nativeCRDs, "")
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
		t.Fatal("emit.Generate produced no composition output")
	}

	adoptedBP, _, err := Adopt(compYAML, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(adoptedBP.Spec.Resources) != len(origBP.Spec.Resources) {
		t.Fatalf("expected %d resources, got %d", len(origBP.Spec.Resources), len(adoptedBP.Spec.Resources))
	}

	wantWhens := map[string]string{
		"cm-domain": `params.customDomain != ""`,
		"cm-tier":   `params.tier == ""`,
		"cm-stage":  `env.stage != ""`,
		"cm-region": `env.region == ""`,
	}

	for _, r := range adoptedBP.Spec.Resources {
		want, ok := wantWhens[r.Name]
		if !ok {
			t.Errorf("unexpected resource %q", r.Name)
			continue
		}
		if r.When != want {
			t.Errorf("resource %q: When = %q, want %q", r.Name, r.When, want)
		}
	}
}

func TestAdoptGoTemplate_ReversedWhenGuard(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: reversed-when-guard
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XReversed
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
            {{- if eq "prod" $.spec.tier }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: pro-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
            {{- if ne "basic" $.observed.composite.resource.spec.tier }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: nonbasic-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var proBucket, nonbasicBucket *blueprint.Resource
	for i := range bp.Spec.Resources {
		switch bp.Spec.Resources[i].Name {
		case "pro-bucket":
			proBucket = &bp.Spec.Resources[i]
		case "nonbasic-bucket":
			nonbasicBucket = &bp.Spec.Resources[i]
		}
	}

	if proBucket == nil {
		t.Fatalf("pro-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if proBucket.When != `params.tier == "prod"` {
		t.Errorf("proBucket.When = %q, want %q", proBucket.When, `params.tier == "prod"`)
	}

	if nonbasicBucket == nil {
		t.Fatalf("nonbasic-bucket resource not found: %+v", bp.Spec.Resources)
	}
	if nonbasicBucket.When != `params.tier != "basic"` {
		t.Errorf("nonbasicBucket.When = %q, want %q", nonbasicBucket.When, `params.tier != "basic"`)
	}
}

func TestAdoptGoTemplate_ReversedWhenGuard_AllVariants(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantParam string
		wantEnv   string
	}{
		// Param eq/ne with all prefix variants
		{
			name:      "param eq reversed $spec",
			condition: `eq "prod" $spec.tier`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed $.spec",
			condition: `eq "prod" $.spec.tier`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed .spec",
			condition: `eq "prod" .spec.tier`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed $.observed.composite.resource.spec",
			condition: `eq "prod" $.observed.composite.resource.spec.tier`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed .observed.composite.resource.spec",
			condition: `eq "prod" .observed.composite.resource.spec.tier`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed $spec",
			condition: `ne "dev" $spec.tier`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed $.spec",
			condition: `ne "dev" $.spec.tier`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed .spec",
			condition: `ne "dev" .spec.tier`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed $.observed.composite.resource.spec",
			condition: `ne "dev" $.observed.composite.resource.spec.tier`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed .observed.composite.resource.spec",
			condition: `ne "dev" .observed.composite.resource.spec.tier`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		// Empty string comparisons
		{
			name:      "param eq empty reversed",
			condition: `eq "" $.spec.customDomain`,
			wantWhen:  `params.customDomain == ""`,
			wantParam: "customDomain",
		},
		{
			name:      "param ne empty reversed",
			condition: `ne "" $.spec.customDomain`,
			wantWhen:  `params.customDomain != ""`,
			wantParam: "customDomain",
		},
		// Parenthesized
		{
			name:      "param eq reversed parenthesized",
			condition: `(eq "prod" $.spec.tier)`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed parenthesized",
			condition: `(ne "dev" $.spec.tier)`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		// Env comparisons reversed
		{
			name:      "env eq reversed bare",
			condition: `eq "prod" $env.stage`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq reversed hasKey",
			condition: `and (hasKey $env "stage") (eq "prod" $env.stage)`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq reversed index default",
			condition: `eq "prod" (default "" (index $env "stage"))`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne reversed bare",
			condition: `ne "dev" $env.stage`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne reversed hasKey",
			condition: `or (not (hasKey $env "stage")) (ne "dev" $env.stage)`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne reversed index default",
			condition: `ne "dev" (default "" (index $env "stage"))`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq empty reversed bare",
			condition: `eq "" $env.stage`,
			wantWhen:  `env.stage == ""`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne empty reversed bare",
			condition: `ne "" $env.stage`,
			wantWhen:  `env.stage != ""`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq empty reversed index default",
			condition: `eq "" (default "" (index $env "stage"))`,
			wantWhen:  `env.stage == ""`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne empty reversed index default",
			condition: `ne "" (default "" (index $env "stage"))`,
			wantWhen:  `env.stage != ""`,
			wantEnv:   "stage",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- if %s }}
          ---
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: test-cm
          data:
            key: value
          {{- end }}
`, tc.condition)

			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			res := bp.Spec.Resources[0]
			if res.When != tc.wantWhen {
				t.Errorf("res.When = %q, want %q", res.When, tc.wantWhen)
			}
			if tc.wantParam != "" {
				if _, ok := bp.Spec.XRD.Parameters[tc.wantParam]; !ok {
					t.Errorf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
				}
			}
			if tc.wantEnv != "" {
				if _, ok := bp.Spec.Environment[tc.wantEnv]; !ok {
					t.Errorf("environment key %q not declared in bp.Spec.Environment: %+v", tc.wantEnv, bp.Spec.Environment)
				}
			}
		})
	}
}

func TestAdoptGoTemplate_ReversedWhenGuard_RoundTrip(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: reversed-when-roundtrip
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XReversedApp
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
            {{- if eq "prod" $.spec.tier }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: prod-cm
              annotations:
                crossplane.io/composition-resource-name: prod-cm
            data:
              env: "prod"
            {{- end }}
            {{- if ne "basic" $.observed.composite.resource.spec.tier }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: nonbasic-cm
              annotations:
                crossplane.io/composition-resource-name: nonbasic-cm
            data:
              env: "nonbasic"
            {{- end }}
            {{- if eq "prod" $env.stage }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: env-prod-cm
              annotations:
                crossplane.io/composition-resource-name: env-prod-cm
            data:
              env: "prod-stage"
            {{- end }}
`

	adoptedBP, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Initial Adopt failed: %v", err)
	}

	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	outputs, err := emit.Generate(adoptedBP, nativeCRDs, "")
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
		t.Fatal("emit.Generate produced no composition output")
	}

	reAdoptedBP, _, err := Adopt(compYAML, Options{})
	if err != nil {
		t.Fatalf("Re-Adopt failed: %v", err)
	}

	wantWhens := map[string]string{
		"prod-cm":     `params.tier == "prod"`,
		"nonbasic-cm": `params.tier != "basic"`,
		"env-prod-cm": `env.stage == "prod"`,
	}

	if len(reAdoptedBP.Spec.Resources) != len(wantWhens) {
		t.Fatalf("expected %d resources, got %d", len(wantWhens), len(reAdoptedBP.Spec.Resources))
	}

	for _, r := range reAdoptedBP.Spec.Resources {
		want, ok := wantWhens[r.Name]
		if !ok {
			t.Errorf("unexpected resource %q", r.Name)
			continue
		}
		if r.When != want {
			t.Errorf("resource %q: When = %q, want %q", r.Name, r.When, want)
		}
	}
}

func TestAdoptGoTemplate_InterpolatedStringField(t *testing.T) {
	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}

	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-interpolated-string
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            data:
              arn: "arn:aws:s3:::{{ $spec.bucketName }}/*"
              prefixName: "prefix-{{ $spec.bucketName }}"
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	arnField, ok := r.Fields["data[arn]"]
	if !ok {
		arnField, ok = r.Fields["data.arn"]
	}
	if !ok {
		t.Fatalf("data.arn field missing from fields: %+v", r.Fields)
	}
	if arnField.Raw != `"arn:aws:s3:::{{ $spec.bucketName }}/*"` && arnField.Raw != `arn:aws:s3:::{{ $spec.bucketName }}/*` {
		t.Errorf("arnField.Raw = %q, want interpolated raw string; From was %q", arnField.Raw, arnField.From)
	}

	prefixField, ok := r.Fields["data[prefixName]"]
	if !ok {
		prefixField, ok = r.Fields["data.prefixName"]
	}
	if !ok {
		t.Fatalf("data.prefixName field missing from fields: %+v", r.Fields)
	}
	if prefixField.Raw != `"prefix-{{ $spec.bucketName }}"` && prefixField.Raw != `prefix-{{ $spec.bucketName }}` {
		t.Errorf("prefixField.Raw = %q, want interpolated raw string; From was %q", prefixField.Raw, prefixField.From)
	}

	// Downstream round-trip check: emitting this blueprint produces valid template output
	outputs, err := emit.Generate(bp, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}
	for _, o := range outputs {
		if strings.Contains(o.Path, "compositions") {
			s := string(o.Body)
			if !strings.Contains(s, "arn:aws:s3:::") {
				t.Errorf("emitted composition lost 'arn:aws:s3:::' prefix! Content:\n%s", s)
			}
			if !strings.Contains(s, "prefix-") {
				t.Errorf("emitted composition lost 'prefix-' prefix! Content:\n%s", s)
			}
		}
	}
}

func TestAdoptGoTemplate_InterpolatedAndPureVariants(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-variants
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: dep
              annotations:
                crossplane.io/composition-resource-name: dep
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
                interp-ann: "prefix-{{ $spec.annVal }}"
                pure-ann: "{{ $spec.pureAnnVal }}"
            data:
              pureParam: "{{ $spec.bucketName }}"
              pureEnv: "{{ $env.stage }}"
              interpEnv: "stage-{{ $env.stage }}-val"
              pureStatus: '{{ (index .observed.resources "dep").resource.status.atProvider.id }}'
              interpStatus: 'https://{{ (index .observed.resources "dep").resource.status.atProvider.endpoint }}/api'
              pureXRRef: '{{ $xr }}-dep'
              interpXRRef: 'prefix-{{ $xr }}-dep'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	var r blueprint.Resource
	found := false
	for _, res := range bp.Spec.Resources {
		if res.Name == "test-cm" {
			r = res
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("test-cm resource not found in %+v", bp.Spec.Resources)
	}

	// Check annotations
	if ann := r.Annotations["pure-ann"]; ann.From != "params.pureAnnVal" {
		t.Errorf("pure-ann From = %q, want params.pureAnnVal", ann.From)
	}
	if ann := r.Annotations["interp-ann"]; ann.Raw == "" || ann.From != "" {
		t.Errorf("interp-ann Raw = %q, From = %q; want Raw non-empty and From empty", ann.Raw, ann.From)
	}

	// Pure param
	if f := r.Fields["data[pureParam]"]; f.From != "params.bucketName" {
		t.Errorf("pureParam From = %q, want params.bucketName", f.From)
	}

	// Pure env
	if f := r.Fields["data[pureEnv]"]; f.From != "env.stage" {
		t.Errorf("pureEnv From = %q, want env.stage", f.From)
	}

	// Interp env
	if f := r.Fields["data[interpEnv]"]; f.Raw == "" || f.From != "" {
		t.Errorf("interpEnv Raw = %q, From = %q; want Raw set and From empty", f.Raw, f.From)
	}

	// Pure status
	if f := r.Fields["data[pureStatus]"]; f.From != "resources.dep.status.atProvider.id" {
		t.Errorf("pureStatus From = %q, want resources.dep.status.atProvider.id", f.From)
	}

	// Interp status
	if f := r.Fields["data[interpStatus]"]; f.Raw == "" || f.From != "" {
		t.Errorf("interpStatus Raw = %q, From = %q; want Raw set and From empty", f.Raw, f.From)
	}

	// Pure XR ref
	if f := r.Fields["data[pureXRRef]"]; f.From != "resources.dep.metadata.name" {
		t.Errorf("pureXRRef From = %q, want resources.dep.metadata.name", f.From)
	}

	// Interp XR ref
	if f := r.Fields["data[interpXRRef]"]; f.Raw == "" || f.From != "" {
		t.Errorf("interpXRRef Raw = %q, From = %q; want Raw set and From empty", f.Raw, f.From)
	}
}

func TestAdoptGoTemplate_DirectXRRef(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-direct-xr-ref
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: dep
              annotations:
                crossplane.io/composition-resource-name: dep
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            data:
              directXRRef: '{{ .observed.composite.resource.metadata.name }}-dep'
              dollarXRRef: '{{ $.observed.composite.resource.metadata.name }}-dep'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	cm := bp.ResourceNamed("test-cm")
	if f := cm.Fields["data[directXRRef]"]; f.From != "resources.dep.metadata.name" {
		t.Errorf("directXRRef From = %q, want resources.dep.metadata.name", f.From)
	}
	if f := cm.Fields["data[dollarXRRef]"]; f.From != "resources.dep.metadata.name" {
		t.Errorf("dollarXRRef From = %q, want resources.dep.metadata.name", f.From)
	}
}

func TestAdoptGoTemplate_DirectXRRef_AnnotationsAndSlices(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-direct-xr-ref-ann-slice
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: dep
              annotations:
                crossplane.io/composition-resource-name: dep
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: '{{ $.observed.composite.resource.metadata.name }}-inferred-dollar'
              annotations:
                directAnn: '{{ .observed.composite.resource.metadata.name }}-dep'
                dollarAnn: '{{ $.observed.composite.resource.metadata.name }}-dep'
            data:
              sliceRefs:
                - '{{ .observed.composite.resource.metadata.name }}-dep'
                - '{{ $.observed.composite.resource.metadata.name }}-dep'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	res := bp.ResourceNamed("inferred-dollar")
	if res == nil {
		t.Fatalf("inferred-dollar resource not found in %+v", bp.Spec.Resources)
	}
	if a := res.Annotations["directAnn"]; a.From != "resources.dep.metadata.name" {
		t.Errorf("directAnn From = %q, want resources.dep.metadata.name", a.From)
	}
	if a := res.Annotations["dollarAnn"]; a.From != "resources.dep.metadata.name" {
		t.Errorf("dollarAnn From = %q, want resources.dep.metadata.name", a.From)
	}
	if f := res.Fields["data[sliceRefs][0]"]; f.From != "resources.dep.metadata.name" {
		t.Errorf("sliceRefs[0] From = %q, want resources.dep.metadata.name", f.From)
	}
	if f := res.Fields["data[sliceRefs][1]"]; f.From != "resources.dep.metadata.name" {
		t.Errorf("sliceRefs[1] From = %q, want resources.dep.metadata.name", f.From)
	}
}

func TestReXRResourceRef_DirectComposite(t *testing.T) {
	cases := []struct {
		input   string
		wantRes string
	}{
		{"{{ $xr }}-dep", "dep"},
		{"{{- $xr -}}-dep", "dep"},
		{"{{ .observed.composite.resource.metadata.name }}-dep", "dep"},
		{"{{- .observed.composite.resource.metadata.name -}}-dep", "dep"},
		{"{{ $.observed.composite.resource.metadata.name }}-dep", "dep"},
		{"{{- $.observed.composite.resource.metadata.name -}}-dep", "dep"},
	}
	for _, tc := range cases {
		m := reXRResourceRef.FindStringSubmatch(tc.input)
		if len(m) < 2 || m[1] != tc.wantRes {
			t.Errorf("reXRResourceRef.FindStringSubmatch(%q) = %v, want resource %q", tc.input, m, tc.wantRes)
		}
	}
}

func TestReXRNameSuffix_DirectComposite(t *testing.T) {
	cases := []struct {
		input   string
		wantRes string
	}{
		{"{{ $xr }}-my-res", "my-res"},
		{"{{- $xr -}}-my-res", "my-res"},
		{"{{ .observed.composite.resource.metadata.name }}-my-res", "my-res"},
		{"{{- .observed.composite.resource.metadata.name -}}-my-res", "my-res"},
		{"{{ $.observed.composite.resource.metadata.name }}-my-res", "my-res"},
		{"{{- $.observed.composite.resource.metadata.name -}}-my-res", "my-res"},
	}
	for _, tc := range cases {
		m := reXRNameSuffix.FindStringSubmatch(tc.input)
		if len(m) < 2 || m[1] != tc.wantRes {
			t.Errorf("reXRNameSuffix.FindStringSubmatch(%q) = %v, want resource %q", tc.input, m, tc.wantRes)
		}
	}
}

func TestAdoptGoTemplate_InterpolatedSliceElements(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-slice
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            items:
              - "prefix-{{ $spec.foo }}"
              - "{{ $spec.foo }}"
              - "env-{{ $env.bar }}"
              - "{{ $env.bar }}"
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	r := bp.Spec.Resources[0]
	if f := r.Fields["items[0]"]; f.Raw == "" || f.From != "" {
		t.Errorf("items[0] Raw = %q, From = %q; want Raw set and From empty", f.Raw, f.From)
	}
	if f := r.Fields["items[1]"]; f.From != "params.foo" {
		t.Errorf("items[1] From = %q, want params.foo", f.From)
	}
	if f := r.Fields["items[2]"]; f.Raw == "" || f.From != "" {
		t.Errorf("items[2] Raw = %q, From = %q; want Raw set and From empty", f.Raw, f.From)
	}
	if f := r.Fields["items[3]"]; f.From != "env.bar" {
		t.Errorf("items[3] From = %q, want env.bar", f.From)
	}
}

func TestAdoptGoTemplate_GotemplatingAnnotation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XBucket
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: my-bucket
            spec:
              forProvider:
                region: us-east-1
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "my-bucket" {
		t.Errorf("expected resource name %q, got %q", "my-bucket", bp.Spec.Resources[0].Name)
	}
	if _, exists := bp.Spec.Resources[0].Annotations["gotemplating.fn.crossplane.io/composition-resource-name"]; exists {
		t.Errorf("expected gotemplating.fn.crossplane.io/composition-resource-name to be stripped from res.Annotations")
	}
}

func TestAdoptGoTemplate_GotemplatingAnnotation_QuotedAndBrokenChunk(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XBucket
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                "gotemplating.fn.crossplane.io/composition-resource-name": "valid-bucket"
            spec:
              forProvider:
                region: us-east-1
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: broken-bucket
            spec:
              forProvider:
                broken: [unclosed
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "valid-bucket" {
		t.Errorf("expected resource name %q, got %q", "valid-bucket", bp.Spec.Resources[0].Name)
	}
	if report == nil || !report.HasTrueLoss() {
		t.Fatalf("expected loss report for broken chunk, got %+v", report)
	}
	foundBrokenDrop := false
	for _, d := range report.Drops {
		if d.Path == "template.resource.broken-bucket" && strings.Contains(d.Reason, "failed to parse chunk YAML") {
			foundBrokenDrop = true
			break
		}
	}
	if !foundBrokenDrop {
		t.Fatalf("expected drop for template.resource.broken-bucket, got drops: %+v", report.Drops)
	}
}

func TestAdoptGoTemplate_IndexSpecParamRef(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-bucket
          spec:
            forProvider:
              region: {{ index $spec "region" }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	f, ok := bp.Spec.Resources[0].Fields["region"]
	if !ok {
		t.Fatalf("field region not found: %+v", bp.Spec.Resources[0].Fields)
	}
	if f.From != "params.region" {
		t.Errorf("field region From = %q, Raw = %q, want From = %q", f.From, f.Raw, "params.region")
	}
	if _, ok := bp.Spec.XRD.Parameters["region"]; !ok {
		t.Errorf("parameter region not found in XRD parameters: %+v", bp.Spec.XRD.Parameters)
	}
}

func TestAdoptGoTemplate_IndexSpecParamRef_Variants(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-index-variants
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-bucket
              custom-ann: '{{ index $spec "annParam" }}'
          spec:
            forProvider:
              region: {{ (index $spec "region") }}
              acl: {{ (index .observed.composite.resource.spec "acl") | quote }}
              tier: {{ index .spec 'tier' }}
              loggingEnabled: {{ (index $spec "enableLogging") }}
              interpField: "arn:aws:s3:::{{ index $spec "bucketName" }}/*"
            writeConnectionSecretToRef:
              name: {{ index $.spec "secretName" }}
              namespace: {{ (index $spec "secretNs" | quote) }}
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: BucketLogging
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-bucket-logging
          spec:
            forProvider:
              targetBucket: {{ (index $spec "targetBucket") }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	// Check fields
	if f := r.Fields["region"]; f.From != "params.region" {
		t.Errorf("region From = %q, want params.region", f.From)
	}
	if f := r.Fields["acl"]; f.From != "params.acl" {
		t.Errorf("acl From = %q, want params.acl", f.From)
	}
	if f := r.Fields["tier"]; f.From != "params.tier" {
		t.Errorf("tier From = %q, want params.tier", f.From)
	}
	if f := r.Fields["loggingEnabled"]; f.From != "params.enableLogging" {
		t.Errorf("loggingEnabled From = %q, want params.enableLogging", f.From)
	}
	if f := r.Fields["interpField"]; f.Raw == "" || f.From != "" {
		t.Errorf("interpField Raw = %q, From = %q, want Raw populated and From empty", f.Raw, f.From)
	}

	// Check second resource
	r2 := bp.Spec.Resources[1]
	if f := r2.Fields["targetBucket"]; f.From != "params.targetBucket" {
		t.Errorf("targetBucket From = %q, want params.targetBucket", f.From)
	}

	// Check annotations
	if ann := r.Annotations["custom-ann"]; ann.From != "params.annParam" {
		t.Errorf("custom-ann From = %q, want params.annParam", ann.From)
	}

	// Check envelope
	if f := r.Envelope["writeConnectionSecretToRef.name"]; f.From != "params.secretName" {
		t.Errorf("secretName From = %q, want params.secretName", f.From)
	}
	if f := r.Envelope["writeConnectionSecretToRef.namespace"]; f.From != "params.secretNs" {
		t.Errorf("secretNs From = %q, want params.secretNs", f.From)
	}

	// Check parameters declared in XRD
	expectedParams := []string{
		"region",
		"acl",
		"tier",
		"bucketName",
		"annParam",
		"secretName",
		"secretNs",
		"enableLogging",
		"targetBucket",
	}
	for _, p := range expectedParams {
		if _, ok := bp.Spec.XRD.Parameters[p]; !ok {
			t.Errorf("parameter %q not declared in XRD parameters: %+v", p, bp.Spec.XRD.Parameters)
		}
	}
}

func TestAdoptGoTemplate_BooleanWhenGuard(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
	}{
		{
			name:      "eq spec true",
			condition: `eq $spec.enabled true`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "eq true spec",
			condition: `eq true $spec.enabled`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "dot spec eq true",
			condition: `eq .spec.enabled true`,
			wantWhen:  "params.enabled",
		},
		{
			name:      "default false spec",
			condition: `default false $spec.enabled`,
			wantWhen:  "params.enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-boolean-when
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            {{- if %s }}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
            data:
              key: value
            {{- end }}
`, tc.condition)

			bp, report, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			res := bp.Spec.Resources[0]
			if res.When != tc.wantWhen {
				t.Errorf("res.When = %q, want %q (report drops: %+v)", res.When, tc.wantWhen, report.Drops)
			}
		})
	}
}

func TestAdoptGoTemplate_WhenParamIndexSpec(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantParam string
	}{
		{
			name:      "param eq index $spec",
			condition: `eq (index $spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq index .spec",
			condition: `eq (index .spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param eq index $.spec",
			condition: `eq (index $.spec "tier") "prod"`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne index $spec",
			condition: `ne (index $spec "tier") "dev"`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
		{
			name:      "param eq reversed index $spec",
			condition: `eq "prod" (index $spec "tier")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
		},
		{
			name:      "param ne reversed index $spec",
			condition: `ne "dev" (index $spec "tier")`,
			wantWhen:  `params.tier != "dev"`,
			wantParam: "tier",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-when-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- if %s }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			if _, ok := bp.Spec.XRD.Parameters[tc.wantParam]; !ok {
				t.Errorf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
			}
		})
	}
}

func TestAdoptGoTemplate_WhenParamIndexSpec_Truthiness(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantParam string
	}{
		{
			name:      "param truthiness index $spec",
			condition: `(index $spec "enabled")`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
		},
		{
			name:      "param truthiness index .spec",
			condition: `(index .spec "enabled")`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
		},
		{
			name:      "param truthiness index $.spec",
			condition: `(index $.spec "enabled")`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
		},
		{
			name:      "param truthiness unparenthesized index $spec",
			condition: `index $spec "enabled"`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-when-index-truthiness
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- if %s }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			param, ok := bp.Spec.XRD.Parameters[tc.wantParam]
			if !ok {
				t.Fatalf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
			}
			if param.Type != "boolean" {
				t.Errorf("param.Type = %q, want boolean", param.Type)
			}
		})
	}
}

func TestAdoptGoTemplate_SingleQuotedEnvVarWire(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-single-quoted-env-wire
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
      source: Inline
      inline:
        template: |
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              region: {{ index $env 'region' }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	f := res.Fields["region"]
	wantFrom := "env.region"
	if f.From != wantFrom {
		t.Errorf("region.From = %q, want %q (got Raw: %q)", f.From, wantFrom, f.Raw)
	}
	if _, ok := bp.Spec.Environment["region"]; !ok {
		t.Errorf("expected region in bp.Spec.Environment, got %+v", bp.Spec.Environment)
	}
}

func TestAdoptGoTemplate_SingleQuotedEnvVar_Variations(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-single-quoted-variations
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
      source: Inline
      inline:
        template: |
          {{- if eq (default 'dev' (index $env 'stage')) 'prod' }}
          {{- range $i := until (int (default 1 (index $env 'count'))) }}
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              r1: {{ (index $env 'region1') }}
              r2: {{ default 'us-east-1' (index $env 'region2') }}
              r3: {{ default "us-west-2" (index $env 'region3') }}
          {{- end }}
          {{- end }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.When != `env.stage == "prod"` {
		t.Errorf("res.When = %q, want %q", res.When, `env.stage == "prod"`)
	}
	if res.ForEach != "env.count" {
		t.Errorf("res.ForEach = %q, want %q", res.ForEach, "env.count")
	}
	for field, wantFrom := range map[string]string{
		"r1": "env.region1",
		"r2": "env.region2",
		"r3": "env.region3",
	} {
		f := res.Fields[field]
		if f.From != wantFrom {
			t.Errorf("%s.From = %q, want %q (Raw: %q)", field, f.From, wantFrom, f.Raw)
		}
	}
	for _, expectedEnv := range []string{"stage", "count", "region1", "region2", "region3"} {
		if _, ok := bp.Spec.Environment[expectedEnv]; !ok {
			t.Errorf("expected %q in bp.Spec.Environment, got %+v", expectedEnv, bp.Spec.Environment)
		}
	}
}

func TestAdoptGoTemplate_HasKeyParamWhenGuard(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- $spec := .observed.composite.resource.spec }}
            {{- if and (hasKey $spec "tier") (eq $spec.tier "prod") }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	wantWhen := `params.tier == "prod"`
	if r.When != wantWhen {
		t.Errorf("r.When = %q, want %q", r.When, wantWhen)
	}
}

func TestAdoptGoTemplate_HasKeyParamWhenGuard_Variants(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantParam string
		wantType  string
	}{
		{
			name:      "and hasKey eq dot spec",
			condition: `and (hasKey $spec "tier") (eq $spec.tier "prod")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey eq index spec",
			condition: `and (hasKey $spec "tier") (eq (index $spec "tier") "prod")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey eq rev dot spec",
			condition: `and (hasKey $spec "tier") (eq "prod" $spec.tier)`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey eq rev index spec",
			condition: `and (hasKey $spec "tier") (eq "prod" (index $spec "tier"))`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "or not hasKey ne dot spec",
			condition: `or (not (hasKey $spec "tier")) (ne $spec.tier "prod")`,
			wantWhen:  `params.tier != "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "or not hasKey ne index spec",
			condition: `or (not (hasKey $spec "tier")) (ne (index $spec "tier") "prod")`,
			wantWhen:  `params.tier != "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "or not hasKey ne rev dot spec",
			condition: `or (not (hasKey $spec "tier")) (ne "prod" $spec.tier)`,
			wantWhen:  `params.tier != "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "or not hasKey ne rev index spec",
			condition: `or (not (hasKey $spec "tier")) (ne "prod" (index $spec "tier"))`,
			wantWhen:  `params.tier != "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey ne dot spec",
			condition: `and (hasKey $spec "tier") (ne $spec.tier "prod")`,
			wantWhen:  `params.tier != "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey simple truthiness",
			condition: `and (hasKey $spec "enabled") $spec.enabled`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
		{
			name:      "and hasKey simple index truthiness",
			condition: `and (hasKey $spec "enabled") (index $spec "enabled")`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
		{
			name:      "and hasKey dot spec prefix",
			condition: `and (hasKey .spec "tier") (eq .spec.tier "prod")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey dollar dot spec prefix",
			condition: `and (hasKey $.spec "tier") (eq $.spec.tier "prod")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey full composite path prefix",
			condition: `and (hasKey .observed.composite.resource.spec "tier") (eq .observed.composite.resource.spec.tier "prod")`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "outer parentheses",
			condition: `(and (hasKey $spec "tier") (eq $spec.tier "prod"))`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "single quotes in hasKey and value",
			condition: `and (hasKey $spec 'tier') (eq $spec.tier 'prod')`,
			wantWhen:  `params.tier == "prod"`,
			wantParam: "tier",
			wantType:  "string",
		},
		{
			name:      "and hasKey bool eq true",
			condition: `and (hasKey $spec "enabled") (eq $spec.enabled true)`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
		{
			name:      "and hasKey bool eq true rev",
			condition: `and (hasKey $spec "enabled") (eq true $spec.enabled)`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
		{
			name:      "or not hasKey bool ne false",
			condition: `or (not (hasKey $spec "enabled")) (ne $spec.enabled false)`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
		{
			name:      "or not hasKey bool ne false rev",
			condition: `or (not (hasKey $spec "enabled")) (ne false $spec.enabled)`,
			wantWhen:  `params.enabled`,
			wantParam: "enabled",
			wantType:  "boolean",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-haskey-when
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- $spec := .observed.composite.resource.spec }}
            {{- if %s }}
            ---
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)

			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			p, ok := bp.Spec.XRD.Parameters[tc.wantParam]
			if !ok {
				t.Fatalf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
			}
			if p.Type != tc.wantType {
				t.Errorf("parameter %q type = %q, want %q", tc.wantParam, p.Type, tc.wantType)
			}
			if !p.Required {
				t.Errorf("parameter %q Required = false, want true", tc.wantParam)
			}
		})
	}
}

func TestCF299_AdoptForEachNormalizedStatusReference(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: VPC
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_vpc
            name: my-vpc
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/16
          ---
          {{- range $i := until (int (index $.observed.resources "my_vpc").resource.status.atProvider.nodeCount) }}
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-subnet
            name: my-subnet
          spec:
            forProvider:
              vpcId: test
          {{- end }}
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	var subnetForEach string
	for _, r := range bp.Spec.Resources {
		if r.Name == "my-subnet" {
			subnetForEach = r.ForEach
		}
	}
	want := "resources.my-vpc.status.atProvider.nodeCount"
	if subnetForEach != want {
		t.Errorf("expected subnet ForEach to be %q, got %q", want, subnetForEach)
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}
}

func TestCF299_AdoptForEachNormalizedStatusReference_Uppercase(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-uppercase
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: Main_Queue
            name: main-queue
          spec:
            forProvider:
              delaySeconds: 0
          ---
          {{- range $i := until (int (index $.observed.resources "Main_Queue").resource.status.atProvider.maxMessageSize) }}
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: Replica_Queue
            name: replica-queue
          spec:
            forProvider:
              delaySeconds: 0
          {{- end }}
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt error: %v", err)
	}

	var replicaForEach string
	for _, r := range bp.Spec.Resources {
		if r.Name == "replica-queue" {
			replicaForEach = r.ForEach
		}
	}
	want := "resources.main-queue.status.atProvider.maxMessageSize"
	if replicaForEach != want {
		t.Errorf("expected replica ForEach to be %q, got %q", want, replicaForEach)
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}
}

func TestAdoptPipeline_EnvironmentConfigsAfterEngineStep_NormalizedToBefore(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
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
          - name: role
            base:
              apiVersion: iam.aws.m.upbound.io/v1beta1
              kind: Role
              spec:
                forProvider: {}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
    - step: custom-env
      functionRef:
        name: function-environment-configs
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}

	var foundEnvStep bool
	for _, s := range bp.Spec.Pipeline {
		if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName || s.Name == "custom-env" {
			foundEnvStep = true
			if s.Position != blueprint.PositionBefore {
				t.Errorf("expected function-environment-configs position to be %q, got %q", blueprint.PositionBefore, s.Position)
			}
		}
	}
	if !foundEnvStep {
		t.Errorf("custom-env step not found in adopted pipeline: %+v", bp.Spec.Pipeline)
	}
}

func TestCF401_AdoptPreservesCustomEnvironmentConfigsPipelineStep(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-custom
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: environment-configs
      functionRef:
        name: function-environment-configs
      input:
        apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
        kind: Input
        spec:
          environmentConfigs:
            - ref:
                name: my-custom-env
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              region: {{ $env.region }}
`

	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.EnvironmentConfigs) != 1 || bp.Spec.EnvironmentConfigs[0].Name != "my-custom-env" {
		t.Fatalf("expected EnvironmentConfigs to have 1 entry with name 'my-custom-env', got: %+v", bp.Spec.EnvironmentConfigs)
	}

	// The custom environment-configs step should NOT be retained in bp.Spec.Pipeline,
	// because it is canonicalized into bp.Spec.EnvironmentConfigs.
	for _, s := range bp.Spec.Pipeline {
		if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName {
			t.Fatalf("expected function-environment-configs to be pruned from bp.Spec.Pipeline, but found: %+v", s)
		}
	}

	crds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds failed: %v", err)
	}

	// Update bp.Spec.EnvironmentConfigs and verify emit reflects the updated config
	bp.Spec.EnvironmentConfigs[0].Name = "prod-env"
	compBytes, err := emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
	compStr := string(compBytes)
	if !strings.Contains(compStr, "name: prod-env") {
		t.Errorf("expected emitted Composition to contain 'name: prod-env', got:\n%s", compStr)
	}
	if strings.Contains(compStr, "my-custom-env") {
		t.Errorf("emitted Composition still contains old 'my-custom-env', got:\n%s", compStr)
	}

	// Also verify with selector environment configs
	compSelectorYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-selector
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: environment-configs
      functionRef:
        name: function-environment-configs
      input:
        apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
        kind: Input
        spec:
          environmentConfigs:
            - type: Selector
              selector:
                matchLabels:
                  stage: prod
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
            data:
              region: {{ $env.region }}
`
	bpSel, _, err := Adopt([]byte(compSelectorYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt selector failed: %v", err)
	}
	if len(bpSel.Spec.EnvironmentConfigs) != 1 || bpSel.Spec.EnvironmentConfigs[0].Selector == nil || bpSel.Spec.EnvironmentConfigs[0].Selector.MatchLabels["stage"] != "prod" {
		t.Fatalf("expected 1 selector config with stage=prod, got: %+v", bpSel.Spec.EnvironmentConfigs)
	}
	for _, s := range bpSel.Spec.Pipeline {
		if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName {
			t.Fatalf("expected function-environment-configs to be pruned from bpSel.Spec.Pipeline, but found: %+v", s)
		}
	}
	bpSel.Spec.EnvironmentConfigs[0].Selector.MatchLabels["stage"] = "staging"
	compSelBytes, err := emit.Composition(bpSel, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
	compSelStr := string(compSelBytes)
	if !strings.Contains(compSelStr, "stage: staging") {
		t.Errorf("expected emitted Composition to contain 'stage: staging', got:\n%s", compSelStr)
	}
	if strings.Contains(compSelStr, "stage: prod") {
		t.Errorf("emitted Composition still contains old 'stage: prod', got:\n%s", compSelStr)
	}
}

func TestCF306_AdoptGoTemplateDuplicateResourceNames(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-duplicate-names
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XStorage
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
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            labels:
              app: primary
          spec:
            forProvider:
              region: us-east-1
          ---
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            labels:
              app: secondary
          spec:
            forProvider:
              region: us-west-2
`
	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}

	if bp.Spec.Resources[0].Name == bp.Spec.Resources[1].Name {
		t.Errorf("resources must have distinct names, both are %q", bp.Spec.Resources[0].Name)
	}

	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed on adopted resources: %v", err)
	}
}

func TestCF306_AdoptGoTemplateDuplicateResourceNames_StatusReferenceRewriting(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-duplicate-names-status-rewrite
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XStorage
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
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-queue
          spec:
            forProvider:
              delaySeconds: 0
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: My_Queue
          spec:
            forProvider:
              delaySeconds: 10
          ---
          apiVersion: sqs.aws.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              crossplane.io/composition-resource-name: consumer
          spec:
            forProvider:
              redrivePolicy: '{{ (index $.observed.resources "My_Queue").resource.status.atProvider.arn }}'
`
	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(bp.Spec.Resources))
	}

	if bp.Spec.Resources[0].Name != "my-queue" {
		t.Errorf("expected resource 0 name 'my-queue', got %q", bp.Spec.Resources[0].Name)
	}
	if bp.Spec.Resources[1].Name != "my-queue-2" {
		t.Errorf("expected resource 1 name 'my-queue-2', got %q", bp.Spec.Resources[1].Name)
	}
	if bp.Spec.Resources[2].Name != "consumer" {
		t.Errorf("expected resource 2 name 'consumer', got %q", bp.Spec.Resources[2].Name)
	}

	consumer := bp.ResourceNamed("consumer")
	if consumer == nil {
		t.Fatalf("resource 'consumer' not found")
	}
	field, ok := consumer.Fields["redrivePolicy"]
	if !ok {
		t.Fatalf("expected field 'redrivePolicy' on consumer")
	}
	wantFrom := "resources.my-queue-2.status.atProvider.arn"
	if field.From != wantFrom {
		t.Errorf("expected From wire %q, got %q", wantFrom, field.From)
	}

	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}
}

func TestCF298_AdoptGoTemplateObjectParamDrop(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xtests.example.org
spec:
  group: example.org
  names:
    kind: XTest
    plural: xtests
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
                config:
                  type: object
                  properties:
                    region:
                      type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
  mode: Pipeline
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
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: test-bucket
              annotations:
                example.com/ann: "{{ .observed.composite.resource.spec.config }}"
            spec:
              deletionPolicy: "{{ .observed.composite.resource.spec.config }}"
              forProvider:
                objectLockEnabled: "{{ .observed.composite.resource.spec.config }}"
                region: "{{ .observed.composite.resource.spec.config.region }}"
                items:
                  - "{{ .observed.composite.resource.spec.config }}"
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]

	// 1. Verify that res.Fields["objectLockEnabled"] is not wired to params.config.
	if f, exists := res.Fields["objectLockEnabled"]; exists && f.From == "params.config" {
		t.Errorf("expected objectLockEnabled not to be wired to params.config, got: %+v", f)
	}

	// 2. Verify that res.Fields["region"] is wired to params.config.region.
	if got := res.Fields["region"].From; got != "params.config.region" {
		t.Errorf("res.Fields[\"region\"].From = %q, want %q", got, "params.config.region")
	}

	// 3. Verify that res.Annotations["example.com/ann"] is not wired to params.config.
	if a, exists := res.Annotations["example.com/ann"]; exists && a.From == "params.config" {
		t.Errorf("expected annotation not to be wired to params.config, got: %+v", a)
	}

	// 4. Verify that res.Envelope["deletionPolicy"] is not wired to params.config.
	if e, exists := res.Envelope["deletionPolicy"]; exists && e.From == "params.config" {
		t.Errorf("expected envelope deletionPolicy not to be wired to params.config, got: %+v", e)
	}

	// 5. Verify that res.Fields["items[0]"] is not wired to params.config.
	if it, exists := res.Fields["items[0]"]; exists && it.From == "params.config" {
		t.Errorf("expected items[0] not to be wired to params.config, got: %+v", it)
	}

	// 6. Verify that report.Drops contains the recorded drops.
	expectedDrops := map[string]string{
		"resource.test-bucket.spec.objectLockEnabled":               `unsupported whole-object parameter wire from "config"; wire individual object members instead`,
		"resource.test-bucket.metadata.annotations.example.com/ann": `unsupported whole-object parameter wire from "config"; wire individual object members instead`,
		"resource.test-bucket.spec.deletionPolicy":                  `unsupported whole-object parameter wire from "config"; wire individual object members instead`,
		"resource.test-bucket.spec.items[0]":                        `unsupported whole-object parameter wire from "config"; wire individual object members instead`,
	}

	for expectedPath, expectedReason := range expectedDrops {
		found := false
		for _, d := range report.Drops {
			if d.Path == expectedPath && d.Reason == expectedReason {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected drop on %q with reason %q, got drops: %+v", expectedPath, expectedReason, report.Drops)
		}
	}

	// 7. Verify that bp.Validate() succeeds.
	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate() failed: %v", err)
	}
}

func TestAdoptGoTemplate_ParamWithDefault(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-param-default
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: test-cm
              annotations:
                crossplane.io/composition-resource-name: test-cm
                example.org/tier: '{{ default "standard" $spec.tier }}'
            data:
              storageType: '{{ default "gp3" $spec.volumeType }}'
              region: '{{- default "us-east-1" .spec.region -}}'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	f, ok := r.Fields["data[storageType]"]
	if !ok {
		f, ok = r.Fields["data.storageType"]
	}
	if !ok {
		t.Fatalf("data.storageType missing: %+v", r.Fields)
	}
	if f.From != "params.volumeType" {
		t.Errorf("data.storageType From = %q, want %q (Raw = %q)", f.From, "params.volumeType", f.Raw)
	}

	fReg, ok := r.Fields["data[region]"]
	if !ok {
		fReg, ok = r.Fields["data.region"]
	}
	if !ok {
		t.Fatalf("data.region missing: %+v", r.Fields)
	}
	if fReg.From != "params.region" {
		t.Errorf("data.region From = %q, want %q (Raw = %q)", fReg.From, "params.region", fReg.Raw)
	}

	ann, ok := r.Annotations["example.org/tier"]
	if !ok {
		t.Fatalf("example.org/tier annotation missing: %+v", r.Annotations)
	}
	if ann.From != "params.tier" {
		t.Errorf("annotation example.org/tier From = %q, want %q (Raw = %q)", ann.From, "params.tier", ann.Raw)
	}
}

func TestAdoptGoTemplate_ParamWithDefault_VariationsAndXRDless(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-param-default-variations
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: test-dep
              annotations:
                crossplane.io/composition-resource-name: test-dep
                example.org/env: '{{ default "dev" $spec.environment }}'
                example.org/piped: '{{ $spec.tier | default "standard" | quote }}'
            spec:
              template:
                spec:
                  containers:
                    - name: '{{ default "app" (index $spec "containerName") }}'
                      image: '{{ default "nginx:latest" $spec.appImage }}'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	// Check annotations
	if r.Annotations["example.org/env"].From != "params.environment" {
		t.Errorf("annotation example.org/env From = %q, want %q", r.Annotations["example.org/env"].From, "params.environment")
	}

	// Check piped default annotation
	if r.Annotations["example.org/piped"].From != "params.tier" {
		t.Errorf("annotation example.org/piped From = %q, want %q", r.Annotations["example.org/piped"].From, "params.tier")
	}

	// Check index access default field in slice object
	fName, ok := r.Fields["spec.template.spec.containers[0].name"]
	if !ok {
		t.Fatalf("spec.template.spec.containers[0].name missing: %+v", r.Fields)
	}
	if fName.From != "params.containerName" {
		t.Errorf("spec.template.spec.containers[0].name From = %q, want %q", fName.From, "params.containerName")
	}

	// Check field in slice object
	fImg, ok := r.Fields["spec.template.spec.containers[0].image"]
	if !ok {
		t.Fatalf("spec.template.spec.containers[0].image missing: %+v", r.Fields)
	}
	if fImg.From != "params.appImage" {
		t.Errorf("spec.template.spec.containers[0].image From = %q, want %q", fImg.From, "params.appImage")
	}

	// Check XRDless parameters are guarded (not required)
	for _, pName := range []string{"environment", "tier", "containerName", "appImage"} {
		p, ok := bp.Spec.XRD.Parameters[pName]
		if !ok {
			t.Errorf("parameter %q missing from XRD parameters", pName)
			continue
		}
		if p.Required {
			t.Errorf("parameter %q should be optional when guarded by default fallback, got Required: true", pName)
		}
	}

	if err := bp.Validate(); err != nil {
		t.Errorf("bp.Validate() failed: %v", err)
	}

	nativeCRDs, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	outputs, err := emit.Generate(bp, nativeCRDs, "")
	if err != nil {
		t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
	}
	if len(outputs) == 0 {
		t.Fatalf("emit.Generate returned 0 outputs")
	}
}

func TestAdoptGoTemplate_SliceElementParamWithDefault(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-slice-param-default
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XResource
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
            apiVersion: example.org/v1alpha1
            kind: CustomWidget
            metadata:
              name: test-widget
              annotations:
                crossplane.io/composition-resource-name: test-widget
            spec:
              forProvider:
                items:
                  - '{{ default "first" $spec.primaryItem }}'
                  - '{{ default "second" (index $spec "secondaryItem") }}'
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]

	f0, ok := r.Fields["items[0]"]
	if !ok {
		t.Fatalf("items[0] missing: %+v", r.Fields)
	}
	if f0.From != "params.primaryItem" {
		t.Errorf("items[0] From = %q, want %q", f0.From, "params.primaryItem")
	}

	f1, ok := r.Fields["items[1]"]
	if !ok {
		t.Fatalf("items[1] missing: %+v", r.Fields)
	}
	if f1.From != "params.secondaryItem" {
		t.Errorf("items[1] From = %q, want %q", f1.From, "params.secondaryItem")
	}

	for _, pName := range []string{"primaryItem", "secondaryItem"} {
		p, ok := bp.Spec.XRD.Parameters[pName]
		if !ok {
			t.Errorf("parameter %q missing from XRD parameters", pName)
			continue
		}
		if p.Required {
			t.Errorf("parameter %q should be optional when guarded by default fallback", pName)
		}
	}
}

func TestAdoptGoTemplate_ForEachParamWithDefault(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-foreach-default
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- range $i := until (int (default 1 $spec.count)) }}
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: replicated
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.ForEach != "params.count" {
		t.Errorf("res.ForEach = %q, want %q", res.ForEach, "params.count")
	}
}

func TestAdoptGoTemplate_ForEachParamWithDefault_Variants(t *testing.T) {
	tests := []struct {
		name        string
		loopExpr    string
		wantParam   string
		wantDefault string
	}{
		{
			name:        "dot spec prefix",
			loopExpr:    `until (int (default 5 .spec.replicas))`,
			wantParam:   "replicas",
			wantDefault: "5",
		},
		{
			name:        "dollar dot spec prefix",
			loopExpr:    `until (int (default 2 $.spec.instances))`,
			wantParam:   "instances",
			wantDefault: "2",
		},
		{
			name:        "dot observed full path",
			loopExpr:    `until (int (default 3 .observed.composite.resource.spec.shards))`,
			wantParam:   "shards",
			wantDefault: "3",
		},
		{
			name:        "dollar observed full path",
			loopExpr:    `until (int (default 4 $.observed.composite.resource.spec.nodes))`,
			wantParam:   "nodes",
			wantDefault: "4",
		},
		{
			name:        "double quoted default value",
			loopExpr:    `until (int (default "10" $spec.workers))`,
			wantParam:   "workers",
			wantDefault: "10",
		},
		{
			name:        "single quoted default value",
			loopExpr:    `until (int (default '15' $spec.threads))`,
			wantParam:   "threads",
			wantDefault: "15",
		},
		{
			name:        "index spec double quoted",
			loopExpr:    `until (int (default 1 (index $spec "pools")))`,
			wantParam:   "pools",
			wantDefault: "1",
		},
		{
			name:        "index spec single quoted",
			loopExpr:    `until (int (default 2 (index $spec 'queues')))`,
			wantParam:   "queues",
			wantDefault: "2",
		},
		{
			name:        "piped default",
			loopExpr:    `until (int ($spec.tasks | default 3))`,
			wantParam:   "tasks",
			wantDefault: "3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-foreach-variant
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- range $i := %s }}
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: replicated
            annotations:
              crossplane.io/composition-resource-name: replicated
          data:
            key: value
          {{- end }}
`, tc.loopExpr)

			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			res := bp.Spec.Resources[0]
			wantForEach := fmt.Sprintf("params.%s", tc.wantParam)
			if res.ForEach != wantForEach {
				t.Errorf("res.ForEach = %q, want %q", res.ForEach, wantForEach)
			}

			p, ok := bp.Spec.XRD.Parameters[tc.wantParam]
			if !ok {
				t.Fatalf("parameter %q not declared in XRD parameters: %+v", tc.wantParam, bp.Spec.XRD.Parameters)
			}
			if p.Type != "integer" {
				t.Errorf("parameter %q Type = %q, want %q", tc.wantParam, p.Type, "integer")
			}
			if p.Required {
				t.Errorf("parameter %q should be optional (Required: false), got Required: true", tc.wantParam)
			}
			if p.Default != tc.wantDefault {
				t.Errorf("parameter %q Default = %q, want %q", tc.wantParam, p.Default, tc.wantDefault)
			}

			// Verify round-trip emission
			nativeCRDs, err := k8s.Kinds()
			if err != nil {
				t.Fatalf("k8s.Kinds: %v", err)
			}
			outputs, err := emit.Generate(bp, nativeCRDs, "")
			if err != nil {
				t.Fatalf("emit.Generate failed on adopted blueprint: %v", err)
			}
			if len(outputs) == 0 {
				t.Fatalf("emit.Generate returned 0 outputs")
			}
			var compContent string
			for _, o := range outputs {
				if strings.Contains(o.Path, "composition") {
					compContent = string(o.Body)
					break
				}
			}
			if compContent == "" {
				t.Fatalf("composition output not found in generated outputs")
			}
			expectedLoop := fmt.Sprintf(`{{- range $i := until (int $spec.%s) }}`, tc.wantParam)
			if !strings.Contains(compContent, expectedLoop) {
				t.Errorf("emitted composition missing expected loop %q:\n%s", expectedLoop, compContent)
			}
		})
	}
}

func TestCollectSourcesWithClusterAndCRDManifestResource(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "App",
				Plural:  "apps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
				},
			},
			Sources: []blueprint.Source{
				{CRDs: "crds/custom.yaml"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "custom-res",
					Kind:     "CustomResource",
					Provider: "crds/custom.yaml",
				},
				{
					Name:     "cluster-res",
					Kind:     "ClusterResource",
					Provider: "cluster",
				},
			},
		},
	}

	collectSources(bp, "")

	for _, s := range bp.Spec.Sources {
		if s.Provider == "cluster" {
			t.Errorf("collectSources incorrectly appended pseudo-provider \"cluster\" to spec.sources: %+v", bp.Spec.Sources)
		}
		if s.Provider == "crds/custom.yaml" {
			t.Errorf("collectSources incorrectly appended CRD manifest path %q as a provider source: %+v", s.Provider, bp.Spec.Sources)
		}
	}

	meta, err := emit.ConfigurationMeta(bp, nil)
	if err != nil {
		t.Fatalf("emit.ConfigurationMeta failed: %v", err)
	}
	if strings.Contains(string(meta), "crds/custom.yaml") {
		t.Errorf("emit.ConfigurationMeta emitted CRD manifest path as package dependency in crossplane.yaml:\n%s", string(meta))
	}
	if strings.Contains(string(meta), "cluster") {
		t.Errorf("emit.ConfigurationMeta emitted pseudo-provider cluster as package dependency in crossplane.yaml:\n%s", string(meta))
	}
}

func TestCollectSourcesDeduplicatesCRDs(t *testing.T) {
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{CRDs: "crds/custom.yaml"},
				{CRDs: "crds/custom.yaml"},
				{Provider: "xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0"},
				{Provider: "xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0"},
			},
		},
	}
	collectSources(bp, "")
	if len(bp.Spec.Sources) != 2 {
		t.Fatalf("expected 2 sources after deduplication, got %d: %+v", len(bp.Spec.Sources), bp.Spec.Sources)
	}
	expected := []blueprint.Source{
		{CRDs: "crds/custom.yaml"},
		{Provider: "xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0"},
	}
	for i, want := range expected {
		if bp.Spec.Sources[i] != want {
			t.Errorf("sources[%d] = %+v, want %+v", i, bp.Spec.Sources[i], want)
		}
	}
}

func TestAdoptGoTemplate_ForEachParamIndexSpec_Variants(t *testing.T) {
	tests := []struct {
		name      string
		rangeExpr string
		wantParam string
	}{
		{
			name:      "index dot-spec",
			rangeExpr: `range $i := until (int (index .spec "replicas"))`,
			wantParam: "replicas",
		},
		{
			name:      "index dollar-dot-spec",
			rangeExpr: `range $i := until (int (index $.spec "replicas"))`,
			wantParam: "replicas",
		},
		{
			name:      "index full observed path",
			rangeExpr: `range $i := until (int (index .observed.composite.resource.spec "replicas"))`,
			wantParam: "replicas",
		},
		{
			name:      "index dollar observed path",
			rangeExpr: `range $i := until (int (index $.observed.composite.resource.spec "replicas"))`,
			wantParam: "replicas",
		},
		{
			name:      "index single quotes",
			rangeExpr: `range $i := until (int (index $spec 'replicas'))`,
			wantParam: "replicas",
		},
		{
			name:      "index unparenthesized",
			rangeExpr: `range $i := until (int index $spec "replicas")`,
			wantParam: "replicas",
		},
		{
			name:      "dot syntax unchanged",
			rangeExpr: `range $i := until (int $spec.replicas)`,
			wantParam: "replicas",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp-foreach-variant
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
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
          {{- %s }}
          apiVersion: s3.aws.upbound.io/v1beta1
          kind: Bucket
          metadata:
            annotations:
              crossplane.io/composition-resource-name: bucket
          spec:
            forProvider:
              region: us-east-1
          {{- end }}
`, tc.rangeExpr)

			bp, report, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			wantForEach := "params." + tc.wantParam
			if r.ForEach != wantForEach {
				t.Errorf("r.ForEach = %q, want %q", r.ForEach, wantForEach)
			}
			param, ok := bp.Spec.XRD.Parameters[tc.wantParam]
			if !ok {
				t.Fatalf("expected parameter %q in XRD parameters: %+v (drops: %+v)", tc.wantParam, bp.Spec.XRD.Parameters, report.Drops)
			}
			if param.Type != "integer" {
				t.Errorf("parameter %q Type = %q, want 'integer'", tc.wantParam, param.Type)
			}
		})
	}
}

func TestAdoptPreservesNonNameMetadataReferencesAsRaw(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
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
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      inline:
        template: |
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: test-cm
            annotations:
              crossplane.io/composition-resource-name: cm
              my-anno: {{ .observed.resources.sa.resource.metadata.uid }}
          data:
            ns: {{ .observed.resources.sa.resource.metadata.namespace }}
          items:
            - {{ .observed.resources.sa.resource.metadata.labels.app }}
`
	bp, _, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	res := bp.ResourceNamed("cm")
	if res == nil {
		t.Fatal("resource cm not found")
	}
	field, ok := res.Fields["data[ns]"]
	if !ok {
		t.Fatal("field data[ns] not found")
	}
	if field.From != "" {
		t.Errorf("field.From = %q, want empty From because non-name metadata cannot be a status wire", field.From)
	}
	if field.Raw == "" {
		t.Errorf("field.Raw is empty, want raw template preserved")
	}

	itemField, ok := res.Fields["items[0]"]
	if !ok {
		t.Fatal("field items[0] not found")
	}
	if itemField.From != "" {
		t.Errorf("itemField.From = %q, want empty From", itemField.From)
	}
	if itemField.Raw == "" {
		t.Errorf("itemField.Raw is empty, want raw template preserved")
	}

	annoField, ok := res.Annotations["my-anno"]
	if !ok {
		t.Fatal("annotation my-anno not found")
	}
	if annoField.From != "" {
		t.Errorf("annoField.From = %q, want empty From", annoField.From)
	}
	if annoField.Raw == "" {
		t.Errorf("annoField.Raw is empty, want raw template preserved")
	}
}

func TestAdoptGoTemplate_SingleQuotedSetResourceNameAnnotation(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
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
        source: Inline
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                {{ setResourceNameAnnotation 'custom-queue' }}
            spec:
              forProvider:
                region: us-east-1
`
	t.Run("bare single quotes", func(t *testing.T) {
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) == 0 {
			t.Fatalf("expected resources, got none")
		}
		if bp.Spec.Resources[0].Name != "custom-queue" {
			t.Fatalf("expected resource name custom-queue, got %q", bp.Spec.Resources[0].Name)
		}
	})

	t.Run("printf with single quotes", func(t *testing.T) {
		printfManifest := strings.Replace(manifest, "{{ setResourceNameAnnotation 'custom-queue' }}", "{{ setResourceNameAnnotation (printf '%s-custom-queue' $xr) }}", 1)
		bp, _, err := Adopt([]byte(printfManifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) == 0 {
			t.Fatalf("expected resources, got none")
		}
		if bp.Spec.Resources[0].Name != "custom-queue" {
			t.Fatalf("expected resource name custom-queue, got %q", bp.Spec.Resources[0].Name)
		}
	})
}

func TestAdoptGoTemplate_IndentedDocSeparatorInBlockScalar(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.example.org
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
        source: Inline
        inline:
          template: |
            apiVersion: v1
            kind: ConfigMap
            metadata:
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: app-config
            data:
              config.yaml: |
                ---
                server:
                  port: 8080
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	res := bp.ResourceNamed("app-config")
	if res == nil {
		t.Fatal("resource app-config not found")
	}
}

func TestAdoptConfigurationDocument(t *testing.T) {
	manifest := `apiVersion: meta.pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: my-cool-platform
spec:
  dependsOn:
    - provider: xpkg.upbound.io/upbound/provider-aws-s3
      version: "v1.14.0"
    - package: xpkg.upbound.io/crossplane-contrib/function-cel
      version: "v0.4.0"
---
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xbuckets.custom.example.org
spec:
  group: custom.example.org
  names:
    kind: XBucket
    plural: xbuckets
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
                region:
                  type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.custom.example.org
spec:
  compositeTypeRef:
    apiVersion: custom.example.org/v1alpha1
    kind: XBucket
  mode: Pipeline
  pipeline:
    - step: cel
      functionRef:
        name: function-cel
      input:
        apiVersion: cel.fn.crossplane.io/v1beta1
        kind: Composition
        source: Inline
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if bp.Metadata.Name != "my-cool-platform" {
		t.Errorf("expected bp.Metadata.Name to be %q, got %q", "my-cool-platform", bp.Metadata.Name)
	}

	hasProvider := false
	for _, s := range bp.Spec.Sources {
		if strings.Contains(s.Provider, "provider-aws-s3") {
			hasProvider = true
			break
		}
	}
	if !hasProvider {
		t.Errorf("expected provider-aws-s3 in bp.Spec.Sources, got %v", bp.Spec.Sources)
	}

	for _, step := range bp.Spec.Pipeline {
		if step.Name == "cel" {
			if step.Package != "xpkg.upbound.io/crossplane-contrib/function-cel:v0.4.0" {
				t.Errorf("expected pipeline step cel package to be %q, got %q",
					"xpkg.upbound.io/crossplane-contrib/function-cel:v0.4.0", step.Package)
			}
		}
	}

	for _, d := range report.Drops {
		if strings.Contains(d.Reason, "Function package for \"function-cel\" was not found") {
			t.Errorf("unexpected spurious loss report item: %v", d)
		}
	}
}

func TestCleanDependencyVersion(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"   ", ""},
		{"*", ""},
		{"=v1.14.0", "v1.14.0"},
		{"v1.14.0", "v1.14.0"},
		{">=v1.14.0 <v2.0.0", "v1.14.0"},
		{">=v1.14.0, <v2.0.0", "v1.14.0"},
		{">= 1.14.0, < 2.0.0", "1.14.0"},
		{"^0.4.0", "0.4.0"},
		{"~1.2.3", "1.2.3"},
		{"latest", "latest"},
		{">1.0.0; <2.0.0", "1.0.0"},
	}

	for _, tc := range cases {
		got := cleanDependencyVersion(tc.input)
		if got != tc.want {
			t.Errorf("cleanDependencyVersion(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNativeOnlyRoundTripDoesNotInjectProviderName(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "native-app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "NativeApp",
				Plural:  "nativeapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"isImmutable": {
						Type:     "boolean",
						Required: true,
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "config",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"immutable": {
							From: "params.isImmutable",
						},
					},
				},
			},
		},
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("bp.Validate failed: %v", err)
	}

	crds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds failed: %v", err)
	}

	xrdYAML, err := emit.XRD(bp)
	if err != nil {
		t.Fatalf("emit.XRD failed: %v", err)
	}

	compYAML, err := emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}

	manifest := string(xrdYAML) + "\n---\n" + string(compYAML)

	adoptedBP, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("adopt.Adopt failed: %v", err)
	}

	if _, hasProviderName := adoptedBP.Spec.XRD.Parameters["providerName"]; hasProviderName {
		t.Errorf("adopt.Adopt injected providerName into native-only blueprint: %+v", adoptedBP.Spec.XRD.Parameters)
	}
}

func TestInferProviderMatchesDigestPinnedSource(t *testing.T) {
	digestRef := "xpkg.upbound.io/upbound/provider-aws-sqs@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: digestRef},
			},
		},
	}

	got := inferProvider("sqs.aws.upbound.io/v1beta1", "Queue", "", nil, bp)
	if got != digestRef {
		t.Fatalf("inferProvider() = %q, want existing blueprint source %q", got, digestRef)
	}
}

func TestAdoptPreservesDigestPinnedBlueprintSource(t *testing.T) {
	digestRef := "xpkg.upbound.io/upbound/provider-aws-sqs@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	compositionYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: App
  resources:
    - name: test-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            delaySeconds: 10
`
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "app"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "App",
				Plural:  "apps",
				Scope:   "Namespaced",
			},
			Sources: []blueprint.Source{
				{Provider: digestRef},
			},
		},
	}

	adoptedBP, _, err := Adopt([]byte(compositionYAML), Options{
		BaseBlueprint: bp,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(adoptedBP.Spec.Resources) == 0 {
		t.Fatal("Adopt produced no resources")
	}
	r := adoptedBP.Spec.Resources[0]
	if r.Provider != digestRef {
		t.Errorf("resource Provider = %q, want existing digest-pinned source %q", r.Provider, digestRef)
	}

	foundDigest := false
	for _, s := range adoptedBP.Spec.Sources {
		if s.Provider == digestRef {
			foundDigest = true
		} else if strings.Contains(s.Provider, "provider-aws-sqs") {
			t.Errorf("found duplicate/inferred provider source %q alongside digest ref %q in bp.Spec.Sources", s.Provider, digestRef)
		}
	}
	if !foundDigest {
		t.Errorf("digestRef %q missing from adopted bp.Spec.Sources: %+v", digestRef, adoptedBP.Spec.Sources)
	}
}

func TestCF403_AdoptFromEnvironmentFieldPath(t *testing.T) {
	t.Run("classic composition", func(t *testing.T) {
		compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-patch
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XExample
  environment:
    environmentConfigs:
      - type: Reference
        ref:
          name: default
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
      patches:
        - type: FromEnvironmentFieldPath
          fromFieldPath: clusterRegion
          toFieldPath: spec.forProvider.region
        - type: FromEnvironmentFieldPath
          fromFieldPath: teamTag
          toFieldPath: metadata.annotations[custom.io/team]
        - type: FromEnvironmentFieldPath
          fromFieldPath: deletionPolicy
          toFieldPath: spec.deletionPolicy
`

		bp, report, err := Adopt([]byte(compYAML), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}

		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		res := bp.Spec.Resources[0]
		if f, ok := res.Fields["region"]; !ok || f.From != "env.clusterRegion" {
			t.Errorf("expected res.Fields[\"region\"].From = %q, got %+v", "env.clusterRegion", res.Fields["region"])
		}
		if f, ok := res.Annotations["custom.io/team"]; !ok || f.From != "env.teamTag" {
			t.Errorf("expected res.Annotations[\"custom.io/team\"].From = %q, got %+v", "env.teamTag", res.Annotations["custom.io/team"])
		}
		if f, ok := res.Envelope["deletionPolicy"]; !ok || f.From != "env.deletionPolicy" {
			t.Errorf("expected res.Envelope[\"deletionPolicy\"].From = %q, got %+v", "env.deletionPolicy", res.Envelope["deletionPolicy"])
		}

		if envKey, ok := bp.Spec.Environment["clusterRegion"]; !ok || envKey.Type != "string" {
			t.Errorf("expected bp.Spec.Environment[\"clusterRegion\"] to exist with type 'string', got %+v", bp.Spec.Environment)
		}
		if envKey, ok := bp.Spec.Environment["teamTag"]; !ok || envKey.Type != "string" {
			t.Errorf("expected bp.Spec.Environment[\"teamTag\"] to exist with type 'string', got %+v", bp.Spec.Environment)
		}

		if len(bp.Spec.EnvironmentConfigs) != 1 || bp.Spec.EnvironmentConfigs[0].Name != "default" {
			t.Errorf("expected bp.Spec.EnvironmentConfigs to contain 'default' reference, got %+v", bp.Spec.EnvironmentConfigs)
		}

		for _, entry := range report.Drops {
			if strings.Contains(entry.Reason, "FromEnvironmentFieldPath") {
				t.Errorf("loss report contains unexpected error for FromEnvironmentFieldPath: %s", entry.Reason)
			}
		}
	})

	t.Run("patch-and-transform pipeline", func(t *testing.T) {
		compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-env-patch-pnt
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XExample
  mode: Pipeline
  pipeline:
    - step: environment-configs
      functionRef:
        name: function-environment-configs
      input:
        apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
        kind: Input
        spec:
          environmentConfigs:
            - type: Reference
              ref:
                name: default
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
            patches:
              - type: FromEnvironmentFieldPath
                fromFieldPath: clusterRegion
                toFieldPath: spec.forProvider.region
`

		bp, report, err := Adopt([]byte(compYAML), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}

		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		res := bp.Spec.Resources[0]
		if f, ok := res.Fields["region"]; !ok || f.From != "env.clusterRegion" {
			t.Errorf("expected res.Fields[\"region\"].From = %q, got %+v", "env.clusterRegion", res.Fields["region"])
		}

		if envKey, ok := bp.Spec.Environment["clusterRegion"]; !ok || envKey.Type != "string" {
			t.Errorf("expected bp.Spec.Environment[\"clusterRegion\"] to exist with type 'string', got %+v", bp.Spec.Environment)
		}

		for _, entry := range report.Drops {
			if strings.Contains(entry.Reason, "FromEnvironmentFieldPath") {
				t.Errorf("loss report contains unexpected error for FromEnvironmentFieldPath: %s", entry.Reason)
			}
		}
	})
}

func TestCF406_AdoptBracketQuotedAnnotations(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-bracket-quoted-annotations
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XExample
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.team
          toFieldPath: metadata.annotations['custom.io/team']
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.externalName
          toFieldPath: metadata.annotations["crossplane.io/external-name"]
`

	bp, report, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("Adopted blueprint failed validation: %v", err)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if f, ok := res.Annotations["custom.io/team"]; !ok || f.From != "params.team" {
		t.Errorf("expected res.Annotations[\"custom.io/team\"].From = params.team, got %+v", res.Annotations)
	}
	if f, ok := res.Annotations["crossplane.io/external-name"]; !ok || f.From != "params.externalName" {
		t.Errorf("expected res.Annotations[\"crossplane.io/external-name\"].From = params.externalName, got %+v", res.Annotations)
	}
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "annotation") || strings.Contains(d.Reason, "annotation") {
			t.Errorf("unexpected annotation drop in report: %+v", d)
		}
	}
}

func TestAdoptGoTemplate_WhenEnvIndex(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantEnv   string
	}{
		{
			name:      "env eq index $env",
			condition: `eq (index $env "stage") "prod"`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne index $env",
			condition: `ne (index $env "stage") "dev"`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq reversed index $env",
			condition: `eq "prod" (index $env "stage")`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne reversed index $env",
			condition: `ne "dev" (index $env "stage")`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env boolean index $env",
			condition: `index $env "enabled"`,
			wantWhen:  `env.enabled`,
			wantEnv:   "enabled",
		},
		{
			name:      "env boolean parens index $env",
			condition: `(index $env "enabled")`,
			wantWhen:  `env.enabled`,
			wantEnv:   "enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-when-env-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- if %s }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			if _, ok := bp.Spec.Environment[tc.wantEnv]; !ok {
				t.Errorf("environment key %q not declared: %+v", tc.wantEnv, bp.Spec.Environment)
			}
		})
	}
}

func TestAdoptGoTemplate_WhenEnvIndex_Variants(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		wantWhen  string
		wantEnv   string
	}{
		{
			name:      "env eq single quoted index",
			condition: `eq (index $env 'stage') 'prod'`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env ne single quoted index",
			condition: `ne (index $env 'stage') 'dev'`,
			wantWhen:  `env.stage != "dev"`,
			wantEnv:   "stage",
		},
		{
			name:      "env eq reversed single quoted index",
			condition: `eq 'prod' (index $env 'stage')`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env hasKey with index eq",
			condition: `and (hasKey $env "stage") (eq (index $env "stage") "prod")`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
		{
			name:      "env hasKey with index boolean",
			condition: `and (hasKey $env "enabled") (index $env "enabled")`,
			wantWhen:  `env.enabled`,
			wantEnv:   "enabled",
		},
		{
			name:      "env outer parens eq index",
			condition: `(eq (index $env "stage") "prod")`,
			wantWhen:  `env.stage == "prod"`,
			wantEnv:   "stage",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-when-env-index-variants
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- if %s }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: prod-bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.condition)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			if r.When != tc.wantWhen {
				t.Errorf("r.When = %q, want %q", r.When, tc.wantWhen)
			}
			if _, ok := bp.Spec.Environment[tc.wantEnv]; !ok {
				t.Errorf("environment key %q not declared: %+v", tc.wantEnv, bp.Spec.Environment)
			}
		})
	}
}

func TestCF413_AdoptB64encWires(t *testing.T) {
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xsec.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XSec
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: db
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: db
            ---
            apiVersion: v1
            kind: Secret
            metadata:
              name: app-secret
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: app-secret
            data:
              token: {{ $spec.password | b64enc | quote }}
              tokenIndex: {{ (index $spec "password") | b64enc | quote }}
              apiKey: {{ $env.apiKey | b64enc | quote }}
              apiKeyIndex: {{ (index $env "apiKey") | b64enc | quote }}
              statusSecret: {{ (index $.observed.resources "db").resource.status.atProvider.secret | b64enc | quote }}
              statusSecretDotted: {{ $observed.resources.db.resource.status.atProvider.secret | b64enc | quote }}
`

	bp, report, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if _, ok := bp.Spec.XRD.Parameters["password"]; !ok {
		t.Errorf("expected parameter 'password' in bp.Spec.XRD.Parameters, got %v", bp.Spec.XRD.Parameters)
	}
	if _, ok := bp.Spec.Environment["apiKey"]; !ok {
		t.Errorf("expected environment 'apiKey' in bp.Spec.Environment, got %v", bp.Spec.Environment)
	}

	res := bp.ResourceNamed("app-secret")
	if res == nil {
		t.Fatalf("expected resource 'app-secret', got nil")
	}

	if f := res.Fields["data[token]"]; f.From != "params.password" {
		t.Errorf("data[token].From = %q (Raw: %q), want %q", f.From, f.Raw, "params.password")
	}
	if f := res.Fields["data[tokenIndex]"]; f.From != "params.password" {
		t.Errorf("data[tokenIndex].From = %q (Raw: %q), want %q", f.From, f.Raw, "params.password")
	}
	if f := res.Fields["data[apiKey]"]; f.From != "env.apiKey" {
		t.Errorf("data[apiKey].From = %q (Raw: %q), want %q", f.From, f.Raw, "env.apiKey")
	}
	if f := res.Fields["data[apiKeyIndex]"]; f.From != "env.apiKey" {
		t.Errorf("data[apiKeyIndex].From = %q (Raw: %q), want %q", f.From, f.Raw, "env.apiKey")
	}
	if f := res.Fields["data[statusSecret]"]; f.From != "resources.db.status.atProvider.secret" {
		t.Errorf("data[statusSecret].From = %q (Raw: %q), want %q", f.From, f.Raw, "resources.db.status.atProvider.secret")
	}
	if f := res.Fields["data[statusSecretDotted]"]; f.From != "resources.db.status.atProvider.secret" {
		t.Errorf("data[statusSecretDotted].From = %q (Raw: %q), want %q", f.From, f.Raw, "resources.db.status.atProvider.secret")
	}

	// Also verify variations with bare | b64enc (without | quote)
	compYAMLBareB64 := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xsec-bare.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XSec
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
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: db
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: db
            ---
            apiVersion: v1
            kind: Secret
            metadata:
              name: app-secret
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: app-secret
            data:
              token: {{ $spec.password | b64enc }}
              tokenIndex: {{ (index $spec "password") | b64enc }}
              apiKey: {{ $env.apiKey | b64enc }}
              apiKeyIndex: {{ (index $env "apiKey") | b64enc }}
              statusSecret: {{ (index $.observed.resources "db").resource.status.atProvider.secret | b64enc }}
              statusSecretDotted: {{ $observed.resources.db.resource.status.atProvider.secret | b64enc }}
`

	bpBare, _, err := Adopt([]byte(compYAMLBareB64), Options{})
	if err != nil {
		t.Fatalf("Adopt with bare b64enc failed: %v", err)
	}
	if _, ok := bpBare.Spec.XRD.Parameters["password"]; !ok {
		t.Errorf("expected parameter 'password' in bpBare.Spec.XRD.Parameters, got %v", bpBare.Spec.XRD.Parameters)
	}
	if _, ok := bpBare.Spec.Environment["apiKey"]; !ok {
		t.Errorf("expected environment 'apiKey' in bpBare.Spec.Environment, got %v", bpBare.Spec.Environment)
	}
	resBare := bpBare.ResourceNamed("app-secret")
	if resBare == nil {
		t.Fatalf("expected resource 'app-secret', got nil")
	}
	if f := resBare.Fields["data[token]"]; f.From != "params.password" {
		t.Errorf("bare b64enc data[token].From = %q (Raw: %q), want %q", f.From, f.Raw, "params.password")
	}
	if f := resBare.Fields["data[tokenIndex]"]; f.From != "params.password" {
		t.Errorf("bare b64enc data[tokenIndex].From = %q (Raw: %q), want %q", f.From, f.Raw, "params.password")
	}
	if f := resBare.Fields["data[apiKey]"]; f.From != "env.apiKey" {
		t.Errorf("bare b64enc data[apiKey].From = %q (Raw: %q), want %q", f.From, f.Raw, "env.apiKey")
	}
	if f := resBare.Fields["data[apiKeyIndex]"]; f.From != "env.apiKey" {
		t.Errorf("bare b64enc data[apiKeyIndex].From = %q (Raw: %q), want %q", f.From, f.Raw, "env.apiKey")
	}
	if f := resBare.Fields["data[statusSecret]"]; f.From != "resources.db.status.atProvider.secret" {
		t.Errorf("bare b64enc data[statusSecret].From = %q (Raw: %q), want %q", f.From, f.Raw, "resources.db.status.atProvider.secret")
	}
	if f := resBare.Fields["data[statusSecretDotted]"]; f.From != "resources.db.status.atProvider.secret" {
		t.Errorf("bare b64enc data[statusSecretDotted].From = %q (Raw: %q), want %q", f.From, f.Raw, "resources.db.status.atProvider.secret")
	}
	_ = report
}

func TestCF413_EmitAdoptRoundTrip(t *testing.T) {
	crds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds failed: %v", err)
	}

	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsec-roundtrip.example.org"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "example.org", Version: "v1alpha1", Kind: "XSec", Plural: "xsecs",
				Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"password": {Type: "string"},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"apiKey": {Type: "string"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "app-secret",
					Kind:     "Secret",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[token]":  {From: "params.password"},
						"data[apiKey]": {From: "env.apiKey"},
					},
				},
			},
		},
	}
	if err := bp.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	compBytes, err := emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}

	adoptedBP, _, err := Adopt(compBytes, Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if _, ok := adoptedBP.Spec.XRD.Parameters["password"]; !ok {
		t.Errorf("expected parameter 'password' in adoptedBP.Spec.XRD.Parameters")
	}
	if _, ok := adoptedBP.Spec.Environment["apiKey"]; !ok {
		t.Errorf("expected environment 'apiKey' in adoptedBP.Spec.Environment")
	}

	res := adoptedBP.ResourceNamed("app-secret")
	if res == nil {
		t.Fatalf("expected resource 'app-secret' in adoptedBP")
	}
	if f := res.Fields["data[token]"]; f.From != "params.password" {
		t.Errorf("data[token].From = %q (Raw: %q), want %q", f.From, f.Raw, "params.password")
	}
	if f := res.Fields["data[apiKey]"]; f.From != "env.apiKey" {
		t.Errorf("data[apiKey].From = %q (Raw: %q), want %q", f.From, f.Raw, "env.apiKey")
	}
}

func TestAdoptGoTemplate_ForEachEnvIndex(t *testing.T) {
	tests := []struct {
		name     string
		loopExpr string
		wantEnv  string
	}{
		{
			name:     "env forEach index parens double quotes",
			loopExpr: `until (int (index $env "replicas"))`,
			wantEnv:  "replicas",
		},
		{
			name:     "env forEach index parens single quotes",
			loopExpr: `until (int (index $env 'replicas'))`,
			wantEnv:  "replicas",
		},
		{
			name:     "env forEach index no-parens double quotes",
			loopExpr: `until (int index $env "replicas")`,
			wantEnv:  "replicas",
		},
		{
			name:     "env forEach index no-parens single quotes",
			loopExpr: `until (int index $env 'replicas')`,
			wantEnv:  "replicas",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-foreach-env-index
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            {{- range $i := %s }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              annotations:
                crossplane.io/composition-resource-name: bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`, tc.loopExpr)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 1 {
				t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[0]
			wantForEach := "env." + tc.wantEnv
			if r.ForEach != wantForEach {
				t.Errorf("r.ForEach = %q, want %q", r.ForEach, wantForEach)
			}
			if _, ok := bp.Spec.Environment[tc.wantEnv]; !ok {
				t.Errorf("environment key %q not declared: %+v", tc.wantEnv, bp.Spec.Environment)
			}
		})
	}
}

func TestCF434_AdoptGoTemplatingNames(t *testing.T) {
	t.Run("functionRef name go-templating", func(t *testing.T) {
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
        name: go-templating
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
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if bp.Spec.Resources[0].Kind != "Queue" {
			t.Errorf("Resource kind = %q, want Queue", bp.Spec.Resources[0].Kind)
		}
		if len(bp.Spec.Pipeline) != 0 {
			t.Errorf("expected 0 pipeline steps (engine step adopted as resources), got %d", len(bp.Spec.Pipeline))
		}
	})

	t.Run("functionRef name fn-go-templating", func(t *testing.T) {
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
        name: fn-go-templating
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
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if bp.Spec.Resources[0].Kind != "Queue" {
			t.Errorf("Resource kind = %q, want Queue", bp.Spec.Resources[0].Kind)
		}
	})

	t.Run("resolved via opts.FunctionPackages", func(t *testing.T) {
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
        name: custom-template-fn
      input:
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
		opts := Options{
			FunctionPackages: map[string]string{
				"custom-template-fn": "xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.4.0",
			},
		}
		bp, _, err := Adopt([]byte(manifest), opts)
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if bp.Spec.Resources[0].Kind != "Queue" {
			t.Errorf("Resource kind = %q, want Queue", bp.Spec.Resources[0].Kind)
		}
	})

	t.Run("resolved via companion Function manifest", func(t *testing.T) {
		manifest := `
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: aliased-template
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.4.0
---
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
        name: aliased-template
      input:
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
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if bp.Spec.Resources[0].Kind != "Queue" {
			t.Errorf("Resource kind = %q, want Queue", bp.Spec.Resources[0].Kind)
		}
	})

	t.Run("input kind GoTemplate fallback with arbitrary functionRef name", func(t *testing.T) {
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
        name: unknown-render-function
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
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if bp.Spec.Resources[0].Kind != "Queue" {
			t.Errorf("Resource kind = %q, want Queue", bp.Spec.Resources[0].Kind)
		}
	})

	t.Run("pipeline with hyphenated go-templating and other step", func(t *testing.T) {
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
        name: go-templating
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
    - step: custom-filter
      functionRef:
        name: function-cel-filter
      input:
        apiVersion: cel.fn.crossplane.io/v1alpha1
        kind: Filter
`
		bp, _, err := Adopt([]byte(manifest), Options{})
		if err != nil {
			t.Fatalf("Adopt failed: %v", err)
		}
		if len(bp.Spec.Resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
		}
		if len(bp.Spec.Pipeline) != 1 {
			t.Fatalf("expected 1 pipeline step, got %d", len(bp.Spec.Pipeline))
		}
		if bp.Spec.Pipeline[0].FunctionRef != "function-cel-filter" {
			t.Errorf("pipeline[0].FunctionRef = %q, want function-cel-filter", bp.Spec.Pipeline[0].FunctionRef)
		}
		if bp.Spec.Pipeline[0].Position != "after" {
			t.Errorf("pipeline[0].Position = %q, want after", bp.Spec.Pipeline[0].Position)
		}
	})
}

func TestAdoptGoTemplate_GetComposedResourceStatusWire(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-getcomposedresource-wire
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table
          spec:
            forProvider:
              subnetId: {{ (getComposedResource . "subnet").status.atProvider.id }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	rt := bp.Spec.Resources[1]
	f := rt.Fields["subnetId"]
	wantFrom := "resources.subnet.status.atProvider.id"
	if f.From != wantFrom {
		t.Errorf("subnetId.From = %q, want %q (got Raw: %q)", f.From, wantFrom, f.Raw)
	}
}

func TestAdoptGoTemplate_GetComposedResourceVariations(t *testing.T) {
	tests := []struct {
		name     string
		resName  string
		expr     string
		wantFrom string
		wantRaw  string
	}{
		{
			name:     "dot dollar double quotes",
			expr:     `{{ (getComposedResource . "subnet").status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "dollar double quotes",
			expr:     `{{ (getComposedResource $ "subnet").status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "dollar dot double quotes",
			expr:     `{{ (getComposedResource $. "subnet").status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "observed double quotes",
			expr:     `{{ (getComposedResource $observed "subnet").status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "item variable double quotes",
			expr:     `{{ (getComposedResource $item "subnet").status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "inverted args dot",
			expr:     `{{ (getComposedResource "subnet" .).status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "inverted args dollar",
			expr:     `{{ (getComposedResource "subnet" $).status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "single quotes",
			expr:     `{{ (getComposedResource . 'subnet').status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "inverted single quotes",
			expr:     `{{ (getComposedResource 'subnet' .).status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "backtick quotes",
			expr:     "{{ (getComposedResource . `subnet`).status.atProvider.id }}",
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "inverted backtick quotes",
			expr:     "{{ (getComposedResource `subnet` .).status.atProvider.id }}",
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "with optional resource segment",
			expr:     `{{ (getComposedResource . "subnet").resource.status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "direct status without atProvider",
			expr:     `{{ (getComposedResource . "subnet").status.id }}`,
			wantFrom: "resources.subnet.status.id",
		},
		{
			name:     "condition status path",
			expr:     `{{ (getComposedResource . "subnet").status.conditions.Ready.status }}`,
			wantFrom: "resources.subnet.status.conditions.Ready.status",
		},
		{
			name:     "metadata name",
			expr:     `{{ (getComposedResource . "subnet").metadata.name }}`,
			wantFrom: "resources.subnet.metadata.name",
		},
		{
			name:     "piped quote",
			expr:     `{{ (getComposedResource . "subnet").status.atProvider.id | quote }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "piped b64enc",
			expr:     `{{ (getComposedResource . "subnet").status.atProvider.id | b64enc }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "piped b64enc and quote",
			expr:     `{{ (getComposedResource . "subnet").status.atProvider.id | b64enc | quote }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "extra outer parentheses",
			expr:     `{{ ((getComposedResource . "subnet").status.atProvider.id) }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "double parens around helper",
			expr:     `{{ ((getComposedResource . "subnet")).status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "spaces inside parens",
			expr:     `{{ ( getComposedResource . "subnet" ).status.atProvider.id }}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "trim markers",
			expr:     `{{- (getComposedResource . "subnet").status.atProvider.id -}}`,
			wantFrom: "resources.subnet.status.atProvider.id",
		},
		{
			name:     "normalize DNS label underscores",
			resName:  "sub_net",
			expr:     `{{ (getComposedResource . "sub_net").status.atProvider.id }}`,
			wantFrom: "resources.sub-net.status.atProvider.id",
		},
		{
			name:    "spec path not status or metadata stays raw",
			expr:    `{{ (getComposedResource . "subnet").spec.forProvider.cidrBlock }}`,
			wantRaw: `{{ (getComposedResource . "subnet").spec.forProvider.cidrBlock }}`,
		},
		{
			name:    "metadata other than name stays raw",
			expr:    `{{ (getComposedResource . "subnet").metadata.namespace }}`,
			wantRaw: `{{ (getComposedResource . "subnet").metadata.namespace }}`,
		},
		{
			name:    "interpolated prefix stays raw",
			expr:    `prefix-{{ (getComposedResource . "subnet").status.atProvider.id }}`,
			wantRaw: `prefix-{{ (getComposedResource . "subnet").status.atProvider.id }}`,
		},
		{
			name:    "interpolated suffix stays raw",
			expr:    `{{ (getComposedResource . "subnet").status.atProvider.id }}-suffix`,
			wantRaw: `{{ (getComposedResource . "subnet").status.atProvider.id }}-suffix`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resName := tc.resName
			if resName == "" {
				resName = "subnet"
			}
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-variations
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: %s
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table
          spec:
            forProvider:
              subnetId: %s
`, resName, tc.expr)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 2 {
				t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
			}
			rt := bp.Spec.Resources[1]
			f := rt.Fields["subnetId"]
			if tc.wantFrom != "" && f.From != tc.wantFrom {
				t.Errorf("subnetId.From = %q, want %q (got Raw: %q)", f.From, tc.wantFrom, f.Raw)
			}
			if tc.wantRaw != "" && f.Raw != tc.wantRaw {
				t.Errorf("subnetId.Raw = %q, want %q (got From: %q)", f.Raw, tc.wantRaw, f.From)
			}
		})
	}
}

func TestAdoptGoTemplate_GetComposedResource_AnnotationsAndSlices(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-annotations-slices
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table
              example.com/subnet-id: '{{ (getComposedResource . "subnet").status.atProvider.id }}'
              example.com/subnet-name: '{{ (getComposedResource . "subnet").metadata.name }}'
            name: route-table
          spec:
            forProvider:
              subnets:
              - '{{ (getComposedResource . "subnet").status.atProvider.id }}'
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	rt := bp.Spec.Resources[1]

	annId := rt.Annotations["example.com/subnet-id"]
	wantAnnId := "resources.subnet.status.atProvider.id"
	if annId.From != wantAnnId {
		t.Errorf("annotation subnet-id From = %q, want %q", annId.From, wantAnnId)
	}

	annName := rt.Annotations["example.com/subnet-name"]
	wantAnnName := "resources.subnet.metadata.name"
	if annName.From != wantAnnName {
		t.Errorf("annotation subnet-name From = %q, want %q", annName.From, wantAnnName)
	}

	sliceElem := rt.Fields["subnets[0]"]
	wantSliceElem := "resources.subnet.status.atProvider.id"
	if sliceElem.From != wantSliceElem {
		t.Errorf("slice element subnets[0] From = %q, want %q", sliceElem.From, wantSliceElem)
	}
}

func TestAdoptGoTemplate_ForEachStatusVariants(t *testing.T) {
	tests := []struct {
		name        string
		loopExpr    string
		wantForEach string
	}{
		{
			name:        "status default index double quotes",
			loopExpr:    `until (int (default 1 (index $.observed.resources "subnet").resource.status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status index single quotes",
			loopExpr:    `until (int (index $.observed.resources 'subnet').resource.status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status dollar observed resources without dot",
			loopExpr:    `until (int (index $observed.resources "subnet").resource.status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-foreach-status
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            ---
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: Subnet
            metadata:
              annotations:
                crossplane.io/composition-resource-name: subnet
            spec:
              forProvider:
                cidrBlock: 10.0.0.0/24
            ---
            {{- range $i := %s }}
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: RouteTable
            metadata:
              annotations:
                crossplane.io/composition-resource-name: route-table
            spec:
              forProvider:
                vpcId: vpc-12345
            {{- end }}
`, tc.loopExpr)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 2 {
				t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[1]
			if r.ForEach != tc.wantForEach {
				t.Errorf("r.ForEach = %q, want %q", r.ForEach, tc.wantForEach)
			}
		})
	}
}

func TestAdoptGoTemplate_ForEachStatusAdditionalVariants(t *testing.T) {
	tests := []struct {
		name        string
		loopExpr    string
		wantForEach string
	}{
		{
			name:        "status dot notation default",
			loopExpr:    `until (int (default 1 $.observed.resources.subnet.resource.status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status dot notation dollar without dot default",
			loopExpr:    `until (int (default 1 $observed.resources.subnet.resource.status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status dot notation dollar without dot no default",
			loopExpr:    `until (int $observed.resources.subnet.resource.status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status single quotes with dollar without dot and default",
			loopExpr:    `until (int (default 1 (index $observed.resources 'subnet').resource.status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "status pipe default",
			loopExpr:    `until (int ((index $.observed.resources "subnet").resource.status.count | default 1))`,
			wantForEach: "resources.subnet.status.count",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-foreach-status-additional
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTest
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
            ---
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: Subnet
            metadata:
              annotations:
                crossplane.io/composition-resource-name: subnet
            spec:
              forProvider:
                cidrBlock: 10.0.0.0/24
            ---
            {{- range $i := %s }}
            apiVersion: ec2.aws.upbound.io/v1beta1
            kind: RouteTable
            metadata:
              annotations:
                crossplane.io/composition-resource-name: route-table
            spec:
              forProvider:
                vpcId: vpc-12345
            {{- end }}
`, tc.loopExpr)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 2 {
				t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
			}
			r := bp.Spec.Resources[1]
			if r.ForEach != tc.wantForEach {
				t.Errorf("r.ForEach = %q, want %q", r.ForEach, tc.wantForEach)
			}
		})
	}
}

func TestAdoptGoTemplate_ForEachStatusGetComposedResource(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-for-each-status-composed
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          {{- range $i := until (int (getComposedResource . "subnet").status.count) }}
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table-{{ $i }}
          spec:
            forProvider:
              subnetId: {{ (getComposedResource . "subnet").status.atProvider.id }}
          {{- end }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	rt := bp.Spec.Resources[1]
	wantForEach := "resources.subnet.status.count"
	if rt.ForEach != wantForEach {
		t.Errorf("rt.ForEach = %q, want %q", rt.ForEach, wantForEach)
	}
}

func TestAdoptGoTemplate_ForEachStatusGetComposedResource_Variants(t *testing.T) {
	tests := []struct {
		name        string
		loopExpr    string
		wantForEach string
	}{
		{
			name:        "with dollar scope",
			loopExpr:    `until (int (getComposedResource $ "subnet").status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "single quotes",
			loopExpr:    `until (int (getComposedResource . 'subnet').status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "inverted args dot",
			loopExpr:    `until (int (getComposedResource "subnet" .).status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "inverted args dollar",
			loopExpr:    `until (int (getComposedResource "subnet" $).status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "inverted single quotes",
			loopExpr:    `until (int (getComposedResource 'subnet' .).status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "backtick quotes",
			loopExpr:    "until (int (getComposedResource . `subnet`).status.count)",
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "inverted backtick quotes",
			loopExpr:    "until (int (getComposedResource `subnet` .).status.count)",
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with default prefix",
			loopExpr:    `until (int (default 1 (getComposedResource . "subnet").status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with default prefix without outer paren",
			loopExpr:    `until (int default 1 (getComposedResource . "subnet").status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with default prefix dollar single quotes",
			loopExpr:    `until (int (default 1 (getComposedResource $ 'subnet').status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with pipe default",
			loopExpr:    `until (int ((getComposedResource . "subnet").status.count | default 1))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with pipe default without outer parens",
			loopExpr:    `until (int (getComposedResource . "subnet").status.count | default 1)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with pipe default and dollar single quotes",
			loopExpr:    `until (int ((getComposedResource $ 'subnet').status.count | default 1))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with optional resource segment",
			loopExpr:    `until (int (getComposedResource . "subnet").resource.status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "with status atProvider path",
			loopExpr:    `until (int (getComposedResource . "subnet").status.atProvider.count)`,
			wantForEach: "resources.subnet.status.atProvider.count",
		},
		{
			name:        "with extra outer parens",
			loopExpr:    `until (int ((getComposedResource . "subnet").status.count))`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "double parens around helper",
			loopExpr:    `until (int ((getComposedResource . "subnet")).status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "spaces inside helper parens",
			loopExpr:    `until (int ( getComposedResource . "subnet" ).status.count)`,
			wantForEach: "resources.subnet.status.count",
		},
		{
			name:        "condition status path",
			loopExpr:    `until (int (getComposedResource . "subnet").status.conditions.Ready.status)`,
			wantForEach: "resources.subnet.status.conditions.Ready.status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-variants
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: subnet
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          {{- range $i := %s }}
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table-{{ $i }}
          spec:
            forProvider:
              subnetId: test
          {{- end }}
`, tc.loopExpr)
			bp, _, err := Adopt([]byte(manifest), Options{})
			if err != nil {
				t.Fatalf("Adopt failed: %v", err)
			}
			if len(bp.Spec.Resources) != 2 {
				t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
			}
			rt := bp.Spec.Resources[1]
			if rt.ForEach != tc.wantForEach {
				t.Errorf("rt.ForEach = %q, want %q", rt.ForEach, tc.wantForEach)
			}
		})
	}
}

func TestAdoptGoTemplate_ForEachStatusGetComposedResource_NameMapping(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-name-mapping
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
      source: Inline
      inline:
        template: |
          ---
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: Subnet
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_subnet
            name: my-subnet
          spec:
            forProvider:
              cidrBlock: 10.0.0.0/24
          ---
          {{- range $i := until (int (getComposedResource . "my_subnet").status.count) }}
          apiVersion: ec2.aws.upbound.io/v1beta1
          kind: RouteTable
          metadata:
            annotations:
              crossplane.io/composition-resource-name: route-table-{{ $i }}
          spec:
            forProvider:
              subnetId: test
          {{- end }}
`
	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	rt := bp.Spec.Resources[1]
	wantForEach := "resources.my-subnet.status.count"
	if rt.ForEach != wantForEach {
		t.Errorf("rt.ForEach = %q, want %q", rt.ForEach, wantForEach)
	}
}

func TestAdopt_NormalizeResourceNames_Repro(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xpostgres-app
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XPostgres
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
          apiVersion: database.example.org/v1alpha1
          kind: DatabaseInstance
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_db
          spec:
            forProvider:
              allocatedStorage: 20
          ---
          apiVersion: database.example.org/v1alpha1
          kind: DatabaseUser
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-user
          spec:
            forProvider:
              endpoint: "https://{{ (index $.observed.resources \"my_db\").resource.status.atProvider.endpoint }}/api"
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	userRes := bp.Spec.Resources[1]
	wantEndpoint := `https://{{ (index $.observed.resources \"my-db\").resource.status.atProvider.endpoint }}/api`
	if got := userRes.Fields["endpoint"].Raw; got != wantEndpoint {
		t.Errorf("endpoint.Raw = %q, want %q", got, wantEndpoint)
	}
}

func TestAdopt_NormalizeResourceNames_RewritesRawAndTemplates(t *testing.T) {
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xpostgres-app
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XPostgres
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
          {{- define "db-helper" }}
          helper: {{ (index $.observed.resources "my_db").resource.status.atProvider.endpoint }}
          {{- end }}
          ---
          apiVersion: database.example.org/v1alpha1
          kind: DatabaseInstance
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my_db
          spec:
            forProvider:
              allocatedStorage: 20
          ---
          apiVersion: database.example.org/v1alpha1
          kind: DatabaseUser
          metadata:
            annotations:
              crossplane.io/composition-resource-name: my-user
              custom-anno: "https://{{ (index $.observed.resources \"my_db\").resource.status.atProvider.endpoint }}/anno"
          spec:
            customConfig: "https://{{ (index $.observed.resources \"my_db\").resource.status.atProvider.endpoint }}/env"
            forProvider:
              endpoint: "https://{{ (index $.observed.resources \"my_db\").resource.status.atProvider.endpoint }}/api"
              singleQuoteField: 'prefix-{{ (index $.observed.resources "my_db").resource.status.atProvider.endpoint }}'
              dottedField: 'https://{{ $.observed.resources.my_db.resource.status.atProvider.endpoint }}'
`

	bp, _, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if len(bp.Spec.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(bp.Spec.Resources))
	}
	dbRes := bp.Spec.Resources[0]
	if dbRes.Name != "my-db" {
		t.Fatalf("expected db resource name %q, got %q", "my-db", dbRes.Name)
	}
	userRes := bp.Spec.Resources[1]
	if userRes.Name != "my-user" {
		t.Fatalf("expected user resource name %q, got %q", "my-user", userRes.Name)
	}

	// Field Raw rewrite check (escaped quotes inside double-quoted YAML scalar)
	endpointField, ok := userRes.Fields["endpoint"]
	if !ok {
		t.Fatalf("expected endpoint field in userRes.Fields")
	}
	wantEndpointRaw := `https://{{ (index $.observed.resources \"my-db\").resource.status.atProvider.endpoint }}/api`
	if endpointField.Raw != wantEndpointRaw {
		t.Errorf("endpoint.Raw = %q, want %q", endpointField.Raw, wantEndpointRaw)
	}

	// Field Raw rewrite check (unescaped quotes inside single-quoted YAML scalar)
	sqField, ok := userRes.Fields["singleQuoteField"]
	if !ok {
		t.Fatalf("expected singleQuoteField in userRes.Fields")
	}
	wantSQRaw := `prefix-{{ (index $.observed.resources "my-db").resource.status.atProvider.endpoint }}`
	if sqField.Raw != wantSQRaw {
		t.Errorf("singleQuoteField.Raw = %q, want %q", sqField.Raw, wantSQRaw)
	}

	// Field Raw rewrite check (dotted notation)
	dotField, ok := userRes.Fields["dottedField"]
	if !ok {
		t.Fatalf("expected dottedField in userRes.Fields")
	}
	wantDotRaw := `https://{{ $.observed.resources.my-db.resource.status.atProvider.endpoint }}`
	if dotField.Raw != wantDotRaw {
		t.Errorf("dottedField.Raw = %q, want %q", dotField.Raw, wantDotRaw)
	}

	// Annotation Raw rewrite check
	annoField, ok := userRes.Annotations["custom-anno"]
	if !ok {
		t.Fatalf("expected custom-anno annotation in userRes.Annotations")
	}
	wantAnnoRaw := `https://{{ (index $.observed.resources \"my-db\").resource.status.atProvider.endpoint }}/anno`
	if annoField.Raw != wantAnnoRaw {
		t.Errorf("annotations[custom-anno].Raw = %q, want %q", annoField.Raw, wantAnnoRaw)
	}

	// Envelope Raw rewrite check
	envField, ok := userRes.Envelope["customConfig"]
	if !ok {
		t.Fatalf("expected customConfig envelope field in userRes.Envelope")
	}
	wantEnvRaw := `https://{{ (index $.observed.resources \"my-db\").resource.status.atProvider.endpoint }}/env`
	if envField.Raw != wantEnvRaw {
		t.Errorf("envelope[customConfig].Raw = %q, want %q", envField.Raw, wantEnvRaw)
	}

	// Template body rewrite check
	tmplBody, ok := bp.Spec.Templates["db-helper"]
	if !ok {
		t.Fatalf("expected db-helper template in bp.Spec.Templates")
	}
	if strings.Contains(tmplBody, `"my_db"`) {
		t.Errorf("template db-helper still contains old resource name %q: %s", "my_db", tmplBody)
	}
	if !strings.Contains(tmplBody, `"my-db"`) {
		t.Errorf("template db-helper does not contain normalized name %q: %s", "my-db", tmplBody)
	}
}

func TestAdopt_RewriteRawResourceBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		from     string
		to       string
		expected string
	}{
		{
			name:     "quoted exact",
			raw:      `{{ index $.observed.resources "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources "primary" }}`,
		},
		{
			name:     "escaped quoted exact",
			raw:      `{{ index $.observed.resources \"main\" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources \"primary\" }}`,
		},
		{
			name:     "quoted prefix-sharing left alone",
			raw:      `{{ index $.observed.resources "main-queue" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources "main-queue" }}`,
		},
		{
			name:     "escaped quoted prefix-sharing left alone",
			raw:      `{{ index $.observed.resources \"main-queue\" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources \"main-queue\" }}`,
		},
		{
			name:     "single quoted index exact",
			raw:      `{{ index $.observed.resources 'main' }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources 'primary' }}`,
		},
		{
			name:     "escaped single quoted index exact",
			raw:      `{{ index $.observed.resources \'main\' }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index $.observed.resources \'primary\' }}`,
		},
		{
			name:     "backtick index exact",
			raw:      "{{ index $.observed.resources `main` }}",
			from:     "main",
			to:       "primary",
			expected: "{{ index $.observed.resources `primary` }}",
		},
		{
			name:     "index resources exact",
			raw:      `{{ index resources "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ index resources "primary" }}`,
		},
		{
			name:     "unanchored quoted string left alone",
			raw:      `{{ if eq $spec.tier "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ if eq $spec.tier "main" }}`,
		},
		{
			name:     "dot observed rewrites exact",
			raw:      `{{ .observed.resources.main.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.primary.resource.status.url }}`,
		},
		{
			name:     "dot observed leaves prefix-sharing alone",
			raw:      `{{ .observed.resources.main-queue.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.main-queue.resource.status.url }}`,
		},
		{
			name:     "dollar dot observed rewrites exact",
			raw:      `{{ $.observed.resources.main.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ $.observed.resources.primary.resource.status.url }}`,
		},
		{
			name:     "dollar dot observed leaves prefix-sharing alone",
			raw:      `{{ $.observed.resources.main-queue.resource.status.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ $.observed.resources.main-queue.resource.status.url }}`,
		},
		{
			name:     "dollar observed rewrites exact",
			raw:      `$observed.resources.main.resource.status.url`,
			from:     "main",
			to:       "primary",
			expected: `$observed.resources.primary.resource.status.url`,
		},
		{
			name:     "dollar observed leaves prefix-sharing alone",
			raw:      `$observed.resources.main-queue.resource.status.url`,
			from:     "main",
			to:       "primary",
			expected: `$observed.resources.main-queue.resource.status.url`,
		},
		{
			name:     "resources dot rewrites exact",
			raw:      `resources.main.status.url`,
			from:     "main",
			to:       "primary",
			expected: `resources.primary.status.url`,
		},
		{
			name:     "resources dot leaves prefix-sharing alone",
			raw:      `resources.main-queue.status.url`,
			from:     "main",
			to:       "primary",
			expected: `resources.main-queue.status.url`,
		},
		{
			name:     "resources with space and brace",
			raw:      `{{ resources.main }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ resources.primary }}`,
		},
		{
			name:     "multiple occurrences in same string",
			raw:      `{{ .observed.resources.main.url }} and {{ .observed.resources.main-queue.url }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ .observed.resources.primary.url }} and {{ .observed.resources.main-queue.url }}`,
		},
		{
			name:     "hasKey exact",
			raw:      `{{ hasKey $.observed.resources "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ hasKey $.observed.resources "primary" }}`,
		},
		{
			name:     "getComposedResource exact",
			raw:      `{{ getComposedResource . "main" }}`,
			from:     "main",
			to:       "primary",
			expected: `{{ getComposedResource . "primary" }}`,
		},
		{
			name:     "dig resources double quoted exact",
			raw:      `hasKey (dig "resources" "main" "resource" "status" dict $.observed) "url"`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig "resources" "primary" "resource" "status" dict $.observed) "url"`,
		},
		{
			name:     "dig resources double quoted prefix-sharing left alone",
			raw:      `hasKey (dig "resources" "main-queue" "resource" "status" dict $.observed) "url"`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig "resources" "main-queue" "resource" "status" dict $.observed) "url"`,
		},
		{
			name:     "dig resources single quoted exact",
			raw:      `hasKey (dig 'resources' 'main' 'resource' 'status' dict $.observed) 'url'`,
			from:     "main",
			to:       "primary",
			expected: `hasKey (dig 'resources' 'primary' 'resource' 'status' dict $.observed) 'url'`,
		},
		{
			name:     "dig resources backtick exact",
			raw:      "hasKey (dig `resources` `main` `resource` `status` dict $.observed) `url`",
			from:     "main",
			to:       "primary",
			expected: "hasKey (dig `resources` `primary` `resource` `status` dict $.observed) `url`",
		},
		{
			name:     "dig non-resources left alone",
			raw:      `dig "params" "main" "status"`,
			from:     "main",
			to:       "primary",
			expected: `dig "params" "main" "status"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteRawResource(tt.raw, tt.from, tt.to)
			if got != tt.expected {
				t.Errorf("rewriteRawResource(%q, %q, %q) = %q, want %q", tt.raw, tt.from, tt.to, got, tt.expected)
			}
		})
	}
}

func TestCF456_AdoptNestedParamInWhenAndForEach(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-nested-when
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XExample
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
            {{- if $spec.cluster.enabled }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed with fatal error: %v", err)
	}

	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatalf("bucket resource not found in adopted blueprint")
	}

	if res.When != "" {
		t.Errorf("expected res.When to be empty (unsupported nested guard dropped), got %q", res.When)
	}

	if len(report.Drops) == 0 {
		t.Errorf("expected LossReport to record drop for unsupported nested when guard")
	}
}

func TestCF456_AdoptNestedParamInForEach(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-nested-foreach
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XExample
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
            {{- range $i := until (int $spec.cluster.count) }}
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: Bucket
            metadata:
              name: bucket
            spec:
              forProvider:
                region: us-east-1
            {{- end }}
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed with fatal error: %v", err)
	}

	res := bp.ResourceNamed("bucket")
	if res == nil {
		t.Fatalf("bucket resource not found in adopted blueprint")
	}

	if res.ForEach != "" {
		t.Errorf("expected res.ForEach to be empty (unsupported nested forEach dropped), got %q", res.ForEach)
	}

	if len(report.Drops) == 0 {
		t.Errorf("expected LossReport to record drop for unsupported nested forEach loop")
	}
}
