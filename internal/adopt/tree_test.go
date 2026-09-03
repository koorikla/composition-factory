package adopt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
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
