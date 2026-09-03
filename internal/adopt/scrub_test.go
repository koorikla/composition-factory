package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubKubectlLiveCompositionExport(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
  generation: 7
  resourceVersion: "12345678"
  uid: "87654321-4321-4321-4321-210987654321"
  creationTimestamp: "2026-09-02T14:32:00Z"
  selfLink: "/apis/apiextensions.crossplane.io/v1/compositions/xqueues.aws.example.org"
  managedFields:
    - manager: crossplane
      operation: Update
      apiVersion: apiextensions.crossplane.io/v1
      time: "2026-09-02T14:32:05Z"
      fieldsType: FieldsV1
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"apiextensions.crossplane.io/v1","kind":"Composition"}'
    argocd.argoproj.io/tracking-id: "app:xqueues"
    user.example.com/team: "platform"
  labels:
    example.com/provider: aws
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
              annotations:
                iam.amazonaws.com/role: {{ $spec.roleArn }}
            spec:
              forProvider:
                region: {{ $spec.region }}
                maxMessageSize: 262144
    - step: auto-ready
      functionRef:
        name: function-auto-ready
status:
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: "2026-09-02T14:32:10Z"
      reason: Available
`

	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.IsLossy() {
		t.Fatalf("expected loss report to record dropped server-side fields")
	}

	scrubCount := report.ScrubCount()
	if scrubCount < 8 {
		t.Errorf("expected at least 8 scrubbed server-side fields, got %d. Drops: %+v", scrubCount, report.Drops)
	}

	// Verify all server noise is stripped and not present in blueprint
	if bp.Metadata.Name != "xqueues.aws.example.org" {
		t.Errorf("Metadata.Name = %q, want xqueues.aws.example.org", bp.Metadata.Name)
	}
	if bp.Spec.XRD.Kind != "XQueue" {
		t.Errorf("XRD.Kind = %q, want XQueue", bp.Spec.XRD.Kind)
	}

	// Verify valid spec fields and resource fields are preserved
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Name != "main-queue" || r.Kind != "Queue" {
		t.Errorf("resource = %s (%s), want main-queue (Queue)", r.Name, r.Kind)
	}
	if r.Fields["region"].From != "params.region" {
		t.Errorf("field region = %+v, want From: params.region", r.Fields["region"])
	}
	if r.Fields["maxMessageSize"].Value != "262144" {
		t.Errorf("field maxMessageSize = %+v, want Value: 262144", r.Fields["maxMessageSize"])
	}
	if r.Annotations["iam.amazonaws.com/role"].From != "params.roleArn" {
		t.Errorf("annotation role = %+v, want From: params.roleArn", r.Annotations["iam.amazonaws.com/role"])
	}

	// Verify blueprint validates cleanly
	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validation failed: %v", err)
	}

	// Verify report summary format
	reportStr := report.String()
	if !strings.Contains(reportStr, "metadata.uid") {
		t.Errorf("report missing metadata.uid: %s", reportStr)
	}
	if !strings.Contains(reportStr, "metadata.resourceVersion") {
		t.Errorf("report missing metadata.resourceVersion: %s", reportStr)
	}
	if !strings.Contains(reportStr, "metadata.creationTimestamp") {
		t.Errorf("report missing metadata.creationTimestamp: %s", reportStr)
	}
	if !strings.Contains(reportStr, "metadata.managedFields") {
		t.Errorf("report missing metadata.managedFields: %s", reportStr)
	}
	if !strings.Contains(reportStr, "status") {
		t.Errorf("report missing status: %s", reportStr)
	}
	if !strings.Contains(reportStr, "kubectl.kubernetes.io/last-applied-configuration") {
		t.Errorf("report missing last-applied-configuration: %s", reportStr)
	}

	// Verify formatting to YAML contains comments for drops
	outYAML, err := FormatAdoptedYAML(bp, report)
	if err != nil {
		t.Fatalf("FormatAdoptedYAML failed: %v", err)
	}
	if !strings.Contains(string(outYAML), "# adopt: dropped") {
		t.Errorf("expected '# adopt: dropped' comments in YAML output:\n%s", string(outYAML))
	}
}

func TestScrubKubectlLiveClassicExportWithNestedResourceBase(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: classic-export
  uid: "11111111-2222-3333-4444-555555555555"
  resourceVersion: "54321"
  generation: 2
  creationTimestamp: "2026-09-01T10:00:00Z"
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"apiextensions.crossplane.io/v1"}'
spec:
  compositeTypeRef:
    apiVersion: example.org/v1
    kind: XRQueue
  resources:
    - name: sqs-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        metadata:
          creationTimestamp: "2026-09-01T10:00:01Z"
          uid: "99999999-8888-7777-6666-555555555555"
          resourceVersion: "99999"
          annotations:
            kubectl.kubernetes.io/last-applied-configuration: "{}"
            custom.io/keep: "true"
        spec:
          forProvider:
            region: us-east-1
        status:
          conditions:
            - type: Ready
              status: "True"
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.region
          toFieldPath: spec.forProvider.region
status:
  conditions:
    - type: Ready
      status: "True"
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to server metadata scrubbing")
	}

	// Should have scrubbed root metadata + status, and nested base metadata + status
	scrubCount := report.ScrubCount()
	if scrubCount < 10 {
		t.Errorf("expected at least 10 scrubbed fields across root and nested resource, got %d. Drops: %+v", scrubCount, report.Drops)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(bp.Spec.Resources))
	}
	r := bp.Spec.Resources[0]
	if r.Name != "sqs-queue" || r.Kind != "Queue" {
		t.Errorf("resource = %s (%s), want sqs-queue (Queue)", r.Name, r.Kind)
	}
	if r.Fields["region"].From != "params.region" {
		t.Errorf("region field = %+v, want From: params.region", r.Fields["region"])
	}
	if r.Annotations["custom.io/keep"].Value != "true" {
		t.Errorf("annotation custom.io/keep = %+v, want Value: true", r.Annotations["custom.io/keep"])
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validation failed: %v", err)
	}
}

func TestScrubKubectlListExport(t *testing.T) {
	manifest := `
apiVersion: v1
kind: List
metadata:
  resourceVersion: ""
items:
- apiVersion: apiextensions.crossplane.io/v1
  kind: CompositeResourceDefinition
  metadata:
    name: xbuckets.example.org
    uid: "xrd-uid-1234"
    resourceVersion: "11111"
    generation: 1
    creationTimestamp: "2026-09-02T10:00:00Z"
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: "{}"
  spec:
    group: example.org
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
                  bucketName:
                    type: string
  status:
    conditions:
      - type: Established
        status: "True"
- apiVersion: apiextensions.crossplane.io/v1
  kind: Composition
  metadata:
    name: xbuckets.example.org
    uid: "comp-uid-5678"
    resourceVersion: "22222"
    generation: 1
    creationTimestamp: "2026-09-02T10:00:05Z"
    managedFields:
      - manager: kubectl
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: "{}"
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
          inline:
            template: |
              apiVersion: s3.aws.upbound.io/v1beta1
              kind: Bucket
              metadata:
                name: my-bucket
              spec:
                forProvider:
                  region: us-west-2
  status:
    conditions:
      - type: Ready
        status: "True"
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.IsLossy() {
		t.Fatalf("expected lossy report due to server metadata scrubbing in List")
	}

	if bp.Metadata.Name != "xbuckets.example.org" {
		t.Errorf("Metadata.Name = %q, want xbuckets.example.org", bp.Metadata.Name)
	}
	if bp.Spec.XRD.Kind != "XBucket" {
		t.Errorf("XRD.Kind = %q, want XBucket", bp.Spec.XRD.Kind)
	}
	if _, ok := bp.Spec.XRD.Parameters["bucketName"]; !ok {
		t.Errorf("expected parameter bucketName from XRD")
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "my-bucket" {
		t.Errorf("resource name = %q, want my-bucket", bp.Spec.Resources[0].Name)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validation failed: %v", err)
	}
}

func TestScrubAdoptTreeWithLiveClusterFiles(t *testing.T) {
	tempDir := t.TempDir()

	compYAML := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: tree-comp
  uid: "tree-comp-uid"
  resourceVersion: "9988"
  creationTimestamp: "2026-09-02T12:00:00Z"
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: "{}"
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTree
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
              name: tree-queue
            spec:
              forProvider:
                region: eu-west-1
status:
  conditions:
    - type: Ready
      status: "True"
`
	if err := os.WriteFile(filepath.Join(tempDir, "composition.yaml"), []byte(compYAML), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tempDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}

	if !report.IsLossy() {
		t.Fatalf("expected lossy report from AdoptTree with live cluster metadata")
	}

	if report.ScrubCount() < 5 {
		t.Errorf("expected at least 5 scrubbed fields, got %d", report.ScrubCount())
	}

	if bp.Metadata.Name != "tree-comp" {
		t.Errorf("Metadata.Name = %q, want tree-comp", bp.Metadata.Name)
	}
	if len(bp.Spec.Resources) != 1 || bp.Spec.Resources[0].Name != "tree-queue" {
		t.Errorf("resources = %+v, want tree-queue", bp.Spec.Resources)
	}

	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validation failed: %v", err)
	}
}
