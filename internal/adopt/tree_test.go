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

func TestAdoptTree(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Write crossplane.yaml
	crossplaneYaml := `apiVersion: meta.pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: configuration-aws-app
spec:
  crossplane:
    version: ">=v1.14.0"
  dependsOn:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs
      version: "=v1.14.0"
    - apiVersion: pkg.crossplane.io/v1
      kind: Provider
      package: xpkg.upbound.io/upbound/provider-aws-s3
      version: "v1.14.0"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "crossplane.yaml"), []byte(crossplaneYaml), 0644); err != nil {
		t.Fatalf("write crossplane.yaml: %v", err)
	}

	// 2. Write apis/xapp/definition.yaml
	apisDir := filepath.Join(tmpDir, "apis", "xapp")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}

	xrdYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xapps.aws.example.org
spec:
  group: aws.example.org
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
                - queueName
              properties:
                queueName:
                  type: string
                  description: Name of the queue
                region:
                  type: string
                  default: us-east-1
                config:
                  type: object
                  properties:
                    retentionPeriod:
                      type: integer
                      default: 86400
`
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(xrdYaml), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

	// 3. Write composition.yaml
	compYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: aws.example.org/v1alpha1
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
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: app-queue
            spec:
              forProvider:
                name: {{ $spec.queueName }}
                region: {{ $spec.region }}
                messageRetentionSeconds: {{ $spec.config.retentionPeriod }}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}

	if report.IsLossy() {
		t.Errorf("unexpected loss report: %s", report.String())
	}

	// Verify Metadata
	if bp.Metadata.Name != "configuration-aws-app" {
		t.Errorf("expected metadata.name 'configuration-aws-app', got %q", bp.Metadata.Name)
	}

	// Verify Sources from crossplane.yaml
	if len(bp.Spec.Sources) < 2 {
		t.Fatalf("expected at least 2 sources from crossplane.yaml, got %d", len(bp.Spec.Sources))
	}
	foundSQS := false
	foundS3 := false
	for _, s := range bp.Spec.Sources {
		if strings.Contains(s.Provider, "provider-aws-sqs:v1.14.0") {
			foundSQS = true
		}
		if strings.Contains(s.Provider, "provider-aws-s3:v1.14.0") {
			foundS3 = true
		}
	}
	if !foundSQS || !foundS3 {
		t.Errorf("sources missing expected provider refs: %+v", bp.Spec.Sources)
	}

	// Verify XRD
	if bp.Spec.XRD.Kind != "XApp" {
		t.Errorf("expected XRD kind 'XApp', got %q", bp.Spec.XRD.Kind)
	}
	if bp.Spec.XRD.Group != "aws.example.org" {
		t.Errorf("expected XRD group 'aws.example.org', got %q", bp.Spec.XRD.Group)
	}
	if bp.Spec.XRD.Version != "v1alpha1" {
		t.Errorf("expected XRD version 'v1alpha1', got %q", bp.Spec.XRD.Version)
	}

	// Verify Parameters
	qParam, ok := bp.Spec.XRD.Parameters["queueName"]
	if !ok || !qParam.Required || qParam.Type != "string" {
		t.Errorf("queueName parameter not parsed correctly: %+v", qParam)
	}
	rParam, ok := bp.Spec.XRD.Parameters["region"]
	if !ok || rParam.Default != "us-east-1" {
		t.Errorf("region parameter not parsed correctly: %+v", rParam)
	}
	cParam, ok := bp.Spec.XRD.Parameters["config"]
	if !ok || cParam.Type != "object" || cParam.Properties["retentionPeriod"].Type != "integer" {
		t.Errorf("config object parameter not parsed correctly: %+v", cParam)
	}

	// Verify Resources
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.Name != "app-queue" || res.Kind != "Queue" {
		t.Errorf("resource mismatch: %+v", res)
	}
	if res.Fields["name"].From != "params.queueName" {
		t.Errorf("field name wire mismatch: %q", res.Fields["name"].From)
	}
	if res.Fields["region"].From != "params.region" {
		t.Errorf("field region wire mismatch: %q", res.Fields["region"].From)
	}
	if res.Fields["messageRetentionSeconds"].From != "params.config.retentionPeriod" {
		t.Errorf("field messageRetentionSeconds wire mismatch: %q", res.Fields["messageRetentionSeconds"].From)
	}
}

func TestAdoptTreeClassicComposition(t *testing.T) {
	tmpDir := t.TempDir()

	apisDir := filepath.Join(tmpDir, "apis", "v1alpha1")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}

	xrdYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xbuckets.s3.example.org
spec:
  group: s3.example.org
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
                acl:
                  type: string
                  default: private
`
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(xrdYaml), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xbuckets.s3.example.org
spec:
  compositeTypeRef:
    apiVersion: s3.example.org/v1alpha1
    kind: XBucket
  resources:
    - name: bucket
      base:
        apiVersion: s3.aws.upbound.io/v1beta1
        kind: Bucket
        spec:
          forProvider:
            region: us-west-2
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.acl
          toFieldPath: spec.forProvider.acl
`
	if err := os.WriteFile(filepath.Join(apisDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-s3:v1.14.0",
	})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}

	if report.IsLossy() {
		t.Errorf("unexpected loss report: %s", report.String())
	}

	if bp.Spec.XRD.Kind != "XBucket" {
		t.Errorf("expected Kind 'XBucket', got %q", bp.Spec.XRD.Kind)
	}

	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	res := bp.Spec.Resources[0]
	if res.Fields["acl"].From != "params.acl" {
		t.Errorf("expected acl wire from params.acl, got %q", res.Fields["acl"].From)
	}
	if res.Fields["region"].Value != "us-west-2" {
		t.Errorf("expected region value 'us-west-2', got %q", res.Fields["region"].Value)
	}
}

func TestAdoptTreeErrors(t *testing.T) {
	// 1. Non-existent dir
	_, _, err := AdoptTree("/non/existent/dir", Options{})
	if err == nil {
		t.Error("expected error for non-existent directory")
	}

	// 2. File instead of dir
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	_ = os.WriteFile(tmpFile, []byte("hello"), 0644)
	_, _, err = AdoptTree(tmpFile, Options{})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' error, got %v", err)
	}

	// 3. Empty directory without Composition
	emptyDir := t.TempDir()
	_, _, err = AdoptTree(emptyDir, Options{})
	if err == nil || !strings.Contains(err.Error(), "no Composition document found") {
		t.Errorf("expected 'no Composition document found' error, got %v", err)
	}
}

func TestRoundTripEmittedCompositionAndXRD(t *testing.T) {
	// 1. Construct a canonical Blueprint with a native Kubernetes resource
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata: blueprint.Metadata{
			Name: "xapps.workloads.example.org",
		},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "workloads.example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:        "string",
						Description: "ProviderConfig name",
						Required:    true,
					},
					"port": {
						Type:        "string",
						Description: "Service port",
						Default:     "8080",
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "app-config",
					Kind:     "ConfigMap",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[PORT]": {
							From: "params.port",
						},
					},
				},
			},
		},
	}

	// 2. Emit initial Crossplane artifacts
	crds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds failed: %v", err)
	}
	origOutputs, err := emit.Generate(bp, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}

	var origComp, origXRD []byte
	for _, o := range origOutputs {
		if strings.Contains(o.Path, "compositions") {
			origComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			origXRD = o.Body
		}
	}
	if len(origComp) == 0 || len(origXRD) == 0 {
		t.Fatalf("failed to find emitted composition or XRD in outputs: %+v", origOutputs)
	}

	// 3. Simulate live Kubernetes API server responses with server-injected metadata and status
	liveXRD := string(origXRD) + "\n" + `  status:
    conditions:
      - lastTransitionTime: "2026-09-03T12:00:00Z"
        reason: Established
        status: "True"
        type: Established
    controllers:
      compositeResourceType:
        apiVersion: workloads.example.org/v1alpha1
        kind: XApp
`
	liveComp := strings.Replace(
		string(origComp),
		"metadata:\n  name: xapps.workloads.example.org",
		`metadata:
  name: xapps.workloads.example.org
  uid: a1b2c3d4-e5f6-7890-abcd-ef1234567890
  resourceVersion: "123456"
  generation: 1
  creationTimestamp: "2026-09-03T12:00:00Z"
  managedFields:
    - manager: crossplane
      operation: Update
      time: "2026-09-03T12:00:00Z"
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"apiextensions.crossplane.io/v1"}'`,
		1,
	)
	liveComp += "\n" + `status:
  conditions:
    - lastTransitionTime: "2026-09-03T12:00:00Z"
      reason: Available
      status: "True"
      type: Ready
`

	// 4. Save to simulated Configuration tree directory
	tmpDir := t.TempDir()
	apisDir := filepath.Join(tmpDir, "apis", "xapp")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(liveXRD), 0644); err != nil {
		t.Fatalf("write definition: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(liveComp), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	// 5. Adopt tree
	adoptedBP, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("expected no true functional loss, got drops: %+v", report.Drops)
	}
	if report.ScrubCount() == 0 {
		t.Errorf("expected scrubbed server-side fields, got 0")
	}

	// 6. Regenerate from adopted blueprint
	rtOutputs, err := emit.Generate(adoptedBP, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate from adopted blueprint failed: %v", err)
	}

	var rtComp, rtXRD []byte
	for _, o := range rtOutputs {
		if strings.Contains(o.Path, "compositions") {
			rtComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			rtXRD = o.Body
		}
	}

	// 7. Verify byte-for-byte fidelity
	if !bytes.Equal(origComp, rtComp) {
		t.Errorf("Round-trip composition mismatch:\n--- ORIGINAL ---\n%s\n--- REGENERATED ---\n%s", string(origComp), string(rtComp))
	}
	if !bytes.Equal(origXRD, rtXRD) {
		t.Errorf("Round-trip XRD mismatch:\n--- ORIGINAL ---\n%s\n--- REGENERATED ---\n%s", string(origXRD), string(rtXRD))
	}
}

func TestRoundTripK8sWorkloadExample(t *testing.T) {
	// 1. Load k8s-workload example
	rawBP, err := os.ReadFile("../../internal/examples/k8s-workload.cf.yaml")
	if err != nil {
		t.Fatalf("read k8s-workload.cf.yaml: %v", err)
	}
	bp, err := blueprint.Parse(rawBP)
	if err != nil {
		t.Fatalf("parse blueprint: %v", err)
	}

	bp.Metadata.Name = "xworkloads.workloads.sparky.ee"
	crds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("load k8s crds: %v", err)
	}

	// 2. Generate original artifacts
	origOutputs, err := emit.Generate(bp, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate failed: %v", err)
	}

	var origComp, origXRD []byte
	for _, o := range origOutputs {
		if strings.Contains(o.Path, "compositions") {
			origComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			origXRD = o.Body
		}
	}
	if len(origComp) == 0 || len(origXRD) == 0 {
		t.Fatalf("expected non-empty origComp and origXRD")
	}

	// 3. Simulate live server-side manifests
	liveXRD := strings.Replace(
		string(origXRD),
		"metadata:\n  name: xworkloads.workloads.sparky.ee",
		`metadata:
  name: xworkloads.workloads.sparky.ee
  uid: 11111111-2222-3333-4444-555555555555
  resourceVersion: "987654"
  generation: 1
  creationTimestamp: "2026-09-03T12:00:00Z"
  managedFields:
    - manager: crossplane
      operation: Update
      time: "2026-09-03T12:00:00Z"
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"apiextensions.crossplane.io/v2"}'`,
		1,
	)
	liveXRD += "\n" + `status:
  conditions:
    - lastTransitionTime: "2026-09-03T12:00:00Z"
      reason: Established
      status: "True"
      type: Established
`

	liveComp := strings.Replace(
		string(origComp),
		"metadata:\n  name: xworkloads.workloads.sparky.ee",
		`metadata:
  name: xworkloads.workloads.sparky.ee
  uid: a1b2c3d4-e5f6-7890-abcd-ef1234567890
  resourceVersion: "123456"
  generation: 1
  creationTimestamp: "2026-09-03T12:00:00Z"
  managedFields:
    - manager: crossplane
      operation: Update
      time: "2026-09-03T12:00:00Z"
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: '{"apiVersion":"apiextensions.crossplane.io/v1"}'`,
		1,
	)
	liveComp += "\n" + `status:
  conditions:
    - lastTransitionTime: "2026-09-03T12:00:00Z"
      reason: Available
      status: "True"
      type: Ready
`

	// 4. Save to simulated Configuration tree directory
	tmpDir := t.TempDir()
	apisDir := filepath.Join(tmpDir, "apis", "xworkload")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(liveXRD), 0644); err != nil {
		t.Fatalf("write definition: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(liveComp), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	// 5. Adopt tree
	adoptedBP, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("expected no true functional loss, got drops: %+v", report.Drops)
	}
	if report.ScrubCount() == 0 {
		t.Errorf("expected scrubbed server-side fields, got 0")
	}

	// 6. Regenerate from adopted blueprint
	rtOutputs, err := emit.Generate(adoptedBP, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate from adopted blueprint failed: %v", err)
	}

	var rtComp, rtXRD []byte
	for _, o := range rtOutputs {
		if strings.Contains(o.Path, "compositions") {
			rtComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			rtXRD = o.Body
		}
	}

	// 7. Verify byte-for-byte fidelity
	if !bytes.Equal(origComp, rtComp) {
		t.Errorf("Round-trip composition mismatch:\n--- ORIGINAL ---\n%s\n--- REGENERATED ---\n%s", string(origComp), string(rtComp))
	}
	if !bytes.Equal(origXRD, rtXRD) {
		t.Errorf("Round-trip XRD mismatch:\n--- ORIGINAL ---\n%s\n--- REGENERATED ---\n%s", string(origXRD), string(rtXRD))
	}
}

func TestAdoptTreeFullFeatures(t *testing.T) {
	tmpDir := t.TempDir()

	crossplaneYaml := `apiVersion: meta.pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: configuration-full-app
spec:
  crossplane:
    version: ">=v1.14.0"
  dependsOn:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs
      version: "=v1.14.0"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "crossplane.yaml"), []byte(crossplaneYaml), 0644); err != nil {
		t.Fatalf("write crossplane.yaml: %v", err)
	}

	apisDir := filepath.Join(tmpDir, "apis", "xfull")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}

	xrdYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xfulls.example.org
spec:
  group: example.org
  names:
    kind: XFull
    plural: xfulls
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
                - queueName
                - replicaCount
              properties:
                queueName:
                  type: string
                  description: Primary queue name
                replicaCount:
                  type: integer
                  default: 2
                enableAudit:
                  type: boolean
                  default: false
`
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(xrdYaml), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xfulls.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XFull
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
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: primary-queue
            spec:
              forProvider:
                region: us-east-1
              writeConnectionSecretToRef:
                name: {{ $spec.queueName }}-secret
                namespace: default
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: QueuePolicy
            metadata:
              name: primary-policy
            spec:
              forProvider:
                queueUrl: {{ (index $.observed.resources "primary-queue").resource.status.atProvider.url }}
            {{- if $env.enableMetrics }}
            {{- range $i := until (int $env.metricReplicas) }}
            ---
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              annotations:
                {{ setResourceNameAnnotation (printf "metrics-%d" $i) }}
            spec:
              forProvider:
                region: eu-west-1
            {{- end }}
            {{- end }}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss in report: %+v", report.Drops)
	}

	// 1. Verify status wire atProvider preservation
	policy := bp.ResourceNamed("primary-policy")
	if policy == nil {
		t.Fatal("primary-policy resource not found")
	}
	if fld := policy.Fields["queueUrl"]; fld.From != "resources.primary-queue.status.atProvider.url" {
		t.Errorf("queueUrl = %+v, want From: resources.primary-queue.status.atProvider.url", fld)
	}

	// 2. Verify provider inference
	primaryQ := bp.ResourceNamed("primary-queue")
	if primaryQ == nil {
		t.Fatal("primary-queue resource not found")
	}
	if primaryQ.Provider != "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0" {
		t.Errorf("primaryQ.Provider = %q, want 'xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0'", primaryQ.Provider)
	}

	// 3. Verify envelope writeConnectionSecretToRef
	if fld, ok := primaryQ.Envelope["writeConnectionSecretToRef.name"]; !ok || fld.Raw == "" && fld.From == "" && fld.Value == "" {
		t.Errorf("writeConnectionSecretToRef.name missing or empty in envelope: %+v", primaryQ.Envelope)
	}
	if fld, ok := primaryQ.Envelope["writeConnectionSecretToRef.namespace"]; !ok || fld.Value != "default" {
		t.Errorf("writeConnectionSecretToRef.namespace = %+v, want Value: default", fld)
	}

	// 4. Verify XRD parameter required and defaults
	repCount, ok := bp.Spec.XRD.Parameters["replicaCount"]
	if !ok {
		t.Fatal("replicaCount parameter missing")
	}
	if !repCount.Required || repCount.Default != "2" || repCount.Type != "integer" {
		t.Errorf("replicaCount = %+v, want Required: true, Default: '2', Type: 'integer'", repCount)
	}

	// 5. Verify forEach loop resource clean naming
	metricsQ := bp.ResourceNamed("metrics")
	if metricsQ == nil {
		t.Fatalf("expected resource named 'metrics', got resources: %+v", bp.Spec.Resources)
	}
	if metricsQ.ForEach != "env.metricReplicas" {
		t.Errorf("metricsQ.ForEach = %q, want 'env.metricReplicas'", metricsQ.ForEach)
	}
	if metricsQ.When != "env.enableMetrics" {
		t.Errorf("metricsQ.When = %q, want 'env.enableMetrics'", metricsQ.When)
	}

	// 6. Verify foreign environment types
	if bp.Spec.Environment["enableMetrics"].Type != "boolean" {
		t.Errorf("env.enableMetrics.Type = %q, want 'boolean'", bp.Spec.Environment["enableMetrics"].Type)
	}
	if bp.Spec.Environment["metricReplicas"].Type != "integer" {
		t.Errorf("env.metricReplicas.Type = %q, want 'integer'", bp.Spec.Environment["metricReplicas"].Type)
	}
}

func TestFunctionPackagePinsSurviveAdopt(t *testing.T) {
	tmpDir := t.TempDir()

	crossplaneYaml := `apiVersion: meta.pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: configuration-custom-fn
spec:
  crossplane:
    version: ">=v1.14.0"
  dependsOn:
    - function: xpkg.upbound.io/crossplane-contrib/function-auto-ready
      version: "=v0.4.1"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "crossplane.yaml"), []byte(crossplaneYaml), 0644); err != nil {
		t.Fatalf("write crossplane.yaml: %v", err)
	}

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xcustoms.example.org
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XCustom
  mode: Pipeline
  pipeline:
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}

	found := false
	for _, step := range bp.Spec.Pipeline {
		if step.FunctionRef == "function-auto-ready" {
			found = true
			if step.Package != "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.4.1" {
				t.Errorf("step.Package = %q, want 'xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.4.1'", step.Package)
			}
		}
	}
	if !found {
		t.Errorf("pipeline step function-auto-ready not found in adopted blueprint: %+v", bp.Spec.Pipeline)
	}
}

func TestRoundTripFullFeaturesFixture(t *testing.T) {
	fixtureData, err := os.ReadFile("../examples/testdata/roundtrip-full.cf.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	bOrig, err := blueprint.Parse(fixtureData)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	bOrig.Metadata.Name = "xworkloadfulls.platform.sparky.ee"

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
        type: object
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                type: object
                properties:
                  region: {type: string}
                  sqsManagedSseEnabled: {type: boolean}
              providerConfigRef:
                type: object
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                properties: {name: {type: string}, namespace: {type: string}}
          status:
            type: object
            properties:
              atProvider:
                type: object
                properties:
                  url: {type: string}
                  arn: {type: string}
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queuepolicies.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: QueuePolicy, plural: queuepolicies, categories: [managed]}
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
                properties:
                  region: {type: string}
                  queueUrl: {type: string}
                  policy: {type: string}
              providerConfigRef:
                type: object
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs(blueprint.SplitDocs(crdDoc))
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	crds = append(crds, native...)

	origOutputs, err := emit.Generate(bOrig, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate orig: %v", err)
	}

	var origComp, origXRD, origFns []byte
	for _, o := range origOutputs {
		if strings.Contains(o.Path, "compositions") {
			origComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			origXRD = o.Body
		} else if strings.Contains(o.Path, "functions.yaml") {
			origFns = o.Body
		}
	}

	// Build simulated live cluster tree
	tmpDir := t.TempDir()
	apisDir := filepath.Join(tmpDir, "apis", "xworkloadfull")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}
	t.Logf("origComp:\n%s", string(origComp))

	liveXRD := string(origXRD) + "\n" + `  status:
    conditions:
      - lastTransitionTime: "2026-09-03T12:00:00Z"
        reason: Established
        status: "True"
        type: Established
`
	liveComp := strings.Replace(
		string(origComp),
		"metadata:\n  name: xworkloadfulls.platform.sparky.ee",
		`metadata:
  name: xworkloadfulls.platform.sparky.ee
  uid: a1b2c3d4-e5f6-7890-abcd-ef1234567890
  resourceVersion: "123456"
  generation: 1
  creationTimestamp: "2026-09-03T12:00:00Z"
  managedFields:
    - manager: crossplane
      operation: Update
      time: "2026-09-03T12:00:00Z"`,
		1,
	)

	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(liveXRD), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(liveComp), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}
	if len(origFns) > 0 {
		if err := os.WriteFile(filepath.Join(tmpDir, "functions.yaml"), origFns, 0644); err != nil {
			t.Fatalf("write functions.yaml: %v", err)
		}
	}

	adoptedBP, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree: %v", err)
	}
	t.Logf("Adopted resources: %+v", adoptedBP.Spec.Resources)
	if p := adoptedBP.ResourceNamed("primary-policy"); p != nil {
		t.Logf("primary-policy fields: %+v", p.Fields)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss in report: %+v", report.Drops)
	}

	rtOutputs, err := emit.Generate(adoptedBP, crds, "")
	if err != nil {
		t.Fatalf("emit.Generate rt: %v", err)
	}

	var rtComp, rtXRD []byte
	for _, o := range rtOutputs {
		if strings.Contains(o.Path, "compositions") {
			rtComp = o.Body
		} else if strings.Contains(o.Path, "xrds") {
			rtXRD = o.Body
		}
	}

	if !bytes.Equal(origComp, rtComp) {
		t.Errorf("regenerated Composition differs from original emission:\n--- Orig ---\n%s\n--- RT ---\n%s", string(origComp), string(rtComp))
	}
	if !bytes.Equal(origXRD, rtXRD) {
		t.Errorf("regenerated XRD differs from original emission:\n--- Orig ---\n%s\n--- RT ---\n%s", string(origXRD), string(rtXRD))
	}
}

func TestAdoptTreePluralInferenceWithoutXRD(t *testing.T) {
	tests := []struct {
		name       string
		compName   string
		kind       string
		group      string
		ctrPlural  string
		wantPlural string
	}{
		{
			name:       "deduce plural from composition metadata name and group",
			compName:   "xpolicies.iam.aws.m.upbound.io",
			kind:       "XPolicy",
			group:      "iam.aws.m.upbound.io",
			wantPlural: "xpolicies",
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
			name:       "fallback to inferPlural for kinds ending in y with consonant",
			compName:   "unrelated-name",
			kind:       "XPolicy",
			group:      "example.org",
			wantPlural: "xpolicies",
		},
		{
			name:       "fallback to inferPlural for kinds ending in s",
			compName:   "unrelated-name",
			kind:       "XAccess",
			group:      "example.org",
			wantPlural: "xaccesses",
		},
		{
			name:       "fallback to inferPlural for kinds ending in x",
			compName:   "unrelated-name",
			kind:       "XBox",
			group:      "example.org",
			wantPlural: "xboxes",
		},
		{
			name:       "fallback to inferPlural for kinds ending in ch or sh",
			compName:   "unrelated-name",
			kind:       "XBranch",
			group:      "example.org",
			wantPlural: "xbranches",
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
			tmpDir := t.TempDir()
			pluralLine := ""
			if tt.ctrPlural != "" {
				pluralLine = fmt.Sprintf("    plural: %s\n", tt.ctrPlural)
			}
			manifest := fmt.Sprintf(`apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: %s
spec:
  compositeTypeRef:
    apiVersion: %s/v1alpha1
    kind: %s
%s  pipeline:
  - step: patch-and-transform
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources:
      - name: dummy
        base:
          apiVersion: v1
          kind: ConfigMap
`, tt.compName, tt.group, tt.kind, pluralLine)

			if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(manifest), 0644); err != nil {
				t.Fatalf("write composition.yaml: %v", err)
			}

			bp, _, err := AdoptTree(tmpDir, Options{})
			if err != nil {
				t.Fatalf("AdoptTree failed: %v", err)
			}
			if bp.Spec.XRD.Plural != tt.wantPlural {
				t.Errorf("XRD.Plural = %q, want %q", bp.Spec.XRD.Plural, tt.wantPlural)
			}
		})
	}
}

func TestAdoptTreeReusesMemoizedStore(t *testing.T) {
	cacheDir := t.TempDir()
	store := cache.New(cacheDir)
	ref := "xpkg.upbound.io/upbound/provider-tree-test:v1.0.0"
	crds := []schema.CRD{
		{
			Group: "tree.test.io",
			Kind:  "TreeResourceA",
		},
		{
			Group: "tree.test.io",
			Kind:  "TreeResourceB",
		},
	}
	if err := store.SaveCRDs(ref, "sha256:abc123tree", crds); err != nil {
		t.Fatalf("SaveCRDs failed: %v", err)
	}

	treeDir := t.TempDir()
	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-tree-memoized
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XTree
  resources:
    - name: res-a
      base:
        apiVersion: tree.test.io/v1
        kind: TreeResourceA
    - name: res-b
      base:
        apiVersion: tree.test.io/v1
        kind: TreeResourceB
`
	if err := os.WriteFile(filepath.Join(treeDir, "composition.yaml"), []byte(compYAML), 0o644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, _, err := AdoptTree(treeDir, Options{Store: store, CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
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

	// Also verify with CacheDir only (no Store explicitly passed)
	bpCacheOnly, _, err := AdoptTree(treeDir, Options{CacheDir: cacheDir})
	if err != nil {
		t.Fatalf("AdoptTree with CacheDir failed: %v", err)
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

func TestCF207_AdoptTree_CompositionSpecUnsupportedFields_LossReport(t *testing.T) {
	treeDir := t.TempDir()

	xrdYAML := `apiVersion: apiextensions.crossplane.io/v1
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
`
	if err := os.WriteFile(filepath.Join(treeDir, "definition.yaml"), []byte(xrdYAML), 0o644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

	compYAML := `apiVersion: apiextensions.crossplane.io/v1
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
	if err := os.WriteFile(filepath.Join(treeDir, "composition.yaml"), []byte(compYAML), 0o644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	bp, report, err := AdoptTree(treeDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
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
}

func TestAdoptTreeFileSystemTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	xrdYAML := `apiVersion: apiextensions.crossplane.io/v1
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
`
	if err := os.WriteFile(filepath.Join(tmpDir, "definition.yaml"), []byte(xrdYAML), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

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
  - step: render
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: FileSystem
      fileSystem:
        dirPath: /templates/xapps.platform.example.org
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYAML), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	templatesDir := filepath.Join(tmpDir, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	ctxTmpl := `{{- $spec := .observed.composite.resource.spec -}}`
	if err := os.WriteFile(filepath.Join(templatesDir, "000-context.yaml"), []byte(ctxTmpl), 0644); err != nil {
		t.Fatalf("write 000-context.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	if bp.Spec.XRD.Kind != "XApp" {
		t.Errorf("expected XRD kind 'XApp', got %q", bp.Spec.XRD.Kind)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}
}

func TestAdoptTreeGracefullyHandlesInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	xrdYAML := `apiVersion: apiextensions.crossplane.io/v1
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
`
	if err := os.WriteFile(filepath.Join(tmpDir, "definition.yaml"), []byte(xrdYAML), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

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
  - step: render
    functionRef:
      name: function-patch-and-transform
    input:
      apiVersion: pt.fn.crossplane.io/v1beta1
      kind: Resources
      resources: []
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYAML), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	invalidYAML := `: this is not valid yaml {[`
	if err := os.WriteFile(filepath.Join(tmpDir, "invalid.yaml"), []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("write invalid.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	if bp.Spec.XRD.Kind != "XApp" {
		t.Errorf("expected XRD kind 'XApp', got %q", bp.Spec.XRD.Kind)
	}
	if !report.IsLossy() {
		t.Errorf("expected report to record skipped unparseable YAML")
	}
}

func TestCF257_AdoptTreeMultiCompositionAmbiguousError(t *testing.T) {
	tmpDir := t.TempDir()

	compA := `
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
`
	compB := `
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
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-a.yaml"), []byte(compA), 0644); err != nil {
		t.Fatalf("write comp-a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-b.yaml"), []byte(compB), 0644); err != nil {
		t.Fatalf("write comp-b.yaml: %v", err)
	}

	bp, _, err := AdoptTree(tmpDir, Options{})
	if err == nil {
		t.Fatalf("expected error when adopting tree with multiple compositions, got nil (bp resources count: %d)", len(bp.Spec.Resources))
	}
	if !strings.Contains(err.Error(), "comp-a") || !strings.Contains(err.Error(), "comp-b") {
		t.Errorf("expected error to identify 'comp-a' and 'comp-b', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ambiguous") && !strings.Contains(err.Error(), "multi-composition") {
		t.Errorf("expected error to report ambiguous multi-composition input, got: %v", err)
	}
}

func TestCF257_AdoptTreeMultiCompositionSelectedTarget(t *testing.T) {
	tmpDir := t.TempDir()

	compA := `
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
`
	compB := `
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
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-a.yaml"), []byte(compA), 0644); err != nil {
		t.Fatalf("write comp-a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-b.yaml"), []byte(compB), 0644); err != nil {
		t.Fatalf("write comp-b.yaml: %v", err)
	}

	// 1. Adopt with TargetComposition: "comp-a"
	bp, report, err := AdoptTree(tmpDir, Options{TargetComposition: "comp-a"})
	if err != nil {
		t.Fatalf("AdoptTree failed with TargetComposition: %v", err)
	}
	if bp.Metadata.Name != "comp-a" {
		t.Errorf("expected bp name 'comp-a', got %q", bp.Metadata.Name)
	}
	if len(bp.Spec.Resources) != 1 || bp.Spec.Resources[0].Name != "res-a" {
		t.Errorf("expected 1 resource 'res-a', got %+v", bp.Spec.Resources)
	}
	if report == nil || !report.HasTrueLoss() {
		t.Errorf("expected report to record true loss for omitted comp-b")
	}
	foundDropB := false
	for _, d := range report.Drops {
		if d.Path == "manifest.Composition/comp-b" {
			foundDropB = true
			if !strings.Contains(d.Reason, "omitted") {
				t.Errorf("expected drop reason to mention omitted, got %q", d.Reason)
			}
			break
		}
	}
	if !foundDropB {
		t.Errorf("expected drop entry for manifest.Composition/comp-b, got drops: %+v", report.Drops)
	}

	// 2. Adopt with CompositionName: "comp-b" (alias)
	bpB, reportB, err := AdoptTree(tmpDir, Options{CompositionName: "comp-b"})
	if err != nil {
		t.Fatalf("AdoptTree failed with CompositionName: %v", err)
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

	// 3. Adopt with non-existent target composition
	_, _, err = AdoptTree(tmpDir, Options{TargetComposition: "comp-c"})
	if err == nil {
		t.Fatalf("expected error for non-existent composition comp-c, got nil")
	}
	if !strings.Contains(err.Error(), "comp-c") {
		t.Errorf("expected error to mention 'comp-c', got: %v", err)
	}
}

func TestCF257_AdoptTreeMultiCompositionWithMultiXRD(t *testing.T) {
	tmpDir := t.TempDir()

	xrdA := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xresourceas.example.org
spec:
  group: example.org
  names:
    kind: XResourceA
    plural: xresourceas
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
                paramA:
                  type: string
`
	xrdB := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xresourcebs.example.org
spec:
  group: example.org
  names:
    kind: XResourceB
    plural: xresourcebs
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
                paramB:
                  type: string
`
	compA := `
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
`
	compB := `
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
	if err := os.MkdirAll(filepath.Join(tmpDir, "apis", "a"), 0755); err != nil {
		t.Fatalf("mkdir apis/a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "apis", "b"), 0755); err != nil {
		t.Fatalf("mkdir apis/b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "apis", "a", "definition.yaml"), []byte(xrdA), 0644); err != nil {
		t.Fatalf("write xrdA: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "apis", "b", "definition.yaml"), []byte(xrdB), 0644); err != nil {
		t.Fatalf("write xrdB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-a.yaml"), []byte(compA), 0644); err != nil {
		t.Fatalf("write comp-a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "comp-b.yaml"), []byte(compB), 0644); err != nil {
		t.Fatalf("write comp-b.yaml: %v", err)
	}

	bpA, _, err := AdoptTree(tmpDir, Options{TargetComposition: "comp-a"})
	if err != nil {
		t.Fatalf("AdoptTree for comp-a failed: %v", err)
	}
	if bpA.Spec.XRD.Kind != "XResourceA" {
		t.Errorf("expected XRD kind 'XResourceA', got %q", bpA.Spec.XRD.Kind)
	}
	if _, ok := bpA.Spec.XRD.Parameters["paramA"]; !ok {
		t.Errorf("expected XRD parameters to include paramA")
	}
	if _, ok := bpA.Spec.XRD.Parameters["paramB"]; ok {
		t.Errorf("XRD parameters unexpectedly included paramB from unselected XRD")
	}

	bpB, _, err := AdoptTree(tmpDir, Options{TargetComposition: "comp-b"})
	if err != nil {
		t.Fatalf("AdoptTree for comp-b failed: %v", err)
	}
	if bpB.Spec.XRD.Kind != "XResourceB" {
		t.Errorf("expected XRD kind 'XResourceB', got %q", bpB.Spec.XRD.Kind)
	}
	if _, ok := bpB.Spec.XRD.Parameters["paramB"]; !ok {
		t.Errorf("expected XRD parameters to include paramB")
	}
	if _, ok := bpB.Spec.XRD.Parameters["paramA"]; ok {
		t.Errorf("XRD parameters unexpectedly included paramA from unselected XRD")
	}
}
