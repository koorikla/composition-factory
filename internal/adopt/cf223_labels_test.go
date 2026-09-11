package adopt

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
)

func TestCF223_AdoptManagedResourceMetadataLabels(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: repro-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: test-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        metadata:
          labels:
            stage: prod
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.env
          toFieldPath: metadata.labels.env
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// 1. Verify adopt reports loss for the dropped labels
	if !report.HasTrueLoss() {
		t.Errorf("expected report to have true loss for dropped managed resource labels, got none")
	}
	var foundBaseLoss, foundPatchLoss bool
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "metadata.labels") {
			foundBaseLoss = true
		}
		if strings.Contains(d.Path, "patches") && strings.Contains(d.Reason, "labels") {
			foundPatchLoss = true
		}
	}
	if !foundBaseLoss {
		t.Errorf("expected drop report for base metadata.labels, got drops: %+v", report.Drops)
	}
	if !foundPatchLoss {
		t.Errorf("expected drop report for patch toFieldPath metadata.labels, got drops: %+v", report.Drops)
	}

	// 2. Verify res.Fields does NOT contain metadata.labels[...]
	res := bp.ResourceNamed("test-queue")
	if res == nil {
		t.Fatalf("resource test-queue not found")
	}
	for fldName := range res.Fields {
		if strings.HasPrefix(fldName, "metadata.labels") {
			t.Errorf("res.Fields contains %q, expected no metadata.labels on managed resource", fldName)
		}
	}

	// 3. Verify that emit succeeds without "is not in ... spec.forProvider" error
	crds := testCRDs(t)
	_, err = emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
}

func TestCF223_AdoptManagedResourceMetadataLabels_BracketSyntax(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: repro-comp-bracket
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: test-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        metadata:
          labels:
            stage: staging
        spec:
          forProvider:
            region: us-west-2
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.env
          toFieldPath: metadata.labels[env]
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.HasTrueLoss() {
		t.Errorf("expected report to have true loss, got none")
	}

	res := bp.ResourceNamed("test-queue")
	if res == nil {
		t.Fatalf("resource test-queue not found")
	}
	for fldName := range res.Fields {
		if strings.HasPrefix(fldName, "metadata.labels") {
			t.Errorf("res.Fields contains %q, want no metadata.labels on managed resource", fldName)
		}
	}

	crds := testCRDs(t)
	_, err = emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
}

func TestCF223_AdoptPipelineComposition_ManagedLabels(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: pt-managed-labels
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: sqs-queue
            base:
              apiVersion: sqs.aws.upbound.io/v1beta1
              kind: Queue
              metadata:
                labels:
                  managedBy: crossplane
              spec:
                forProvider:
                  region: us-east-1
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.parameters.team
                toFieldPath: metadata.labels.team
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.HasTrueLoss() {
		t.Errorf("expected report to have true loss, got none")
	}

	res := bp.ResourceNamed("sqs-queue")
	if res == nil {
		t.Fatalf("resource sqs-queue not found")
	}
	for fldName := range res.Fields {
		if strings.HasPrefix(fldName, "metadata.labels") {
			t.Errorf("res.Fields contains %q, want no metadata.labels on managed resource", fldName)
		}
	}

	crds := testCRDs(t)
	_, err = emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
}

func TestCF223_AdoptGoTemplateInline_ManagedLabels(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: gt-managed-labels
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
              name: inline-queue
              labels:
                env: dev
                app: test
            spec:
              forProvider:
                region: us-east-1
`
	bp, report, err := Adopt([]byte(manifest), Options{
		DefaultProviderRef: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.HasTrueLoss() {
		t.Errorf("expected report to have true loss, got none")
	}

	res := bp.ResourceNamed("inline-queue")
	if res == nil {
		t.Fatalf("resource inline-queue not found")
	}
	for fldName := range res.Fields {
		if strings.HasPrefix(fldName, "metadata.labels") {
			t.Errorf("res.Fields contains %q, want no metadata.labels on managed resource", fldName)
		}
	}

	crds := testCRDs(t)
	_, err = emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("emit.Composition failed: %v", err)
	}
}

func TestCF223_AdoptNativeResource_PreservesLabels(t *testing.T) {
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: native-labels-comp
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XApp
  resources:
    - name: sa
      base:
        apiVersion: v1
        kind: ServiceAccount
        metadata:
          labels:
            tier: backend
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.parameters.appName
          toFieldPath: metadata.labels.app
`
	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// ServiceAccount is native, so its labels should NOT be dropped as loss
	for _, d := range report.Drops {
		if strings.Contains(d.Path, "labels") {
			t.Errorf("unexpected drop for native resource labels: %+v", d)
		}
	}

	res := bp.ResourceNamed("sa")
	if res == nil {
		t.Fatalf("resource sa not found")
	}
	if res.Provider != blueprint.NativeProvider {
		t.Errorf("expected provider %q, got %q", blueprint.NativeProvider, res.Provider)
	}
	if fld, ok := res.Fields["metadata.labels[tier]"]; !ok || fld.Value != "backend" {
		t.Errorf("expected metadata.labels[tier] = backend, got: %+v", fld)
	}
	if fld, ok := res.Fields["metadata.labels[app]"]; !ok || fld.From != "params.appName" {
		t.Errorf("expected metadata.labels[app] from params.appName, got: %+v", fld)
	}
}
