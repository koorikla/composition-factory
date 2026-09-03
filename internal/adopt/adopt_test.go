package adopt

import (
	"github.com/koorikla/compositionfactory/internal/blueprint"
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

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got drops: %+v", report.Drops)
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

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got: %+v", report.Drops)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got: %+v", report.Drops)
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

	// Verify function-patch-and-transform is not kept in pipeline, but auto-ready is
	if len(bp.Spec.Pipeline) != 1 || bp.Spec.Pipeline[0].Name != "auto-ready" {
		t.Errorf("pipeline = %+v, want only auto-ready step", bp.Spec.Pipeline)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got: %+v", report.Drops)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got drops: %+v", report.Drops)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got drops: %+v", report.Drops)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got drops: %+v", report.Drops)
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
	if report.IsLossy() {
		t.Errorf("expected non-lossy adopt, got drops: %+v", report.Drops)
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
