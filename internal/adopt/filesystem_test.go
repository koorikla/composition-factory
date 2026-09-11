package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// TestAdopt_FileSystemTemplates_Resolved tests that when source: FileSystem is
// used and templates can be loaded from the referenced directory, Adopt parses
// the templates into blueprint resources and sets TemplateSource to FileSystem.
func TestAdopt_FileSystemTemplates_Resolved(t *testing.T) {
	tmpDir := t.TempDir()

	// Write templates to tmpDir/templates/xqueues.platform.sparky.ee/
	tmplDir := filepath.Join(tmpDir, "templates", "xqueues.platform.sparky.ee")
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		t.Fatalf("mkdir tmplDir: %v", err)
	}

	ctxTmpl := `{{- $spec := .observed.composite.resource.spec -}}`
	if err := os.WriteFile(filepath.Join(tmplDir, "000-context.yaml"), []byte(ctxTmpl), 0644); err != nil {
		t.Fatalf("write 000-context.yaml: %v", err)
	}

	queueTmpl := `apiVersion: sqs.aws.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
spec:
  forProvider:
    name: {{ $spec.queueName }}
`
	if err := os.WriteFile(filepath.Join(tmplDir, "001-main-queue.yaml"), []byte(queueTmpl), 0644); err != nil {
		t.Fatalf("write 001-main-queue.yaml: %v", err)
	}

	xrdYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.platform.sparky.ee
spec:
  group: platform.sparky.ee
  names:
    kind: XQueue
    plural: xqueues
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
`

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
      source: FileSystem
      fileSystem:
        dirPath: /templates/xqueues.platform.sparky.ee
`

	combinedManifest := xrdYAML + "\n---\n" + compYAML
	bp, report, err := Adopt([]byte(combinedManifest), Options{
		SourceDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	if len(dropsBeyondXRDless(report)) > 0 {
		t.Fatalf("unexpected drops in report: %+v", dropsBeyondXRDless(report))
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "main-queue" {
		t.Errorf("expected resource name 'main-queue', got %q", bp.Spec.Resources[0].Name)
	}
	if bp.Spec.Resources[0].Kind != "Queue" {
		t.Errorf("expected resource kind 'Queue', got %q", bp.Spec.Resources[0].Kind)
	}
	if bp.TemplateSource() != blueprint.TemplateSourceFileSystem {
		t.Errorf("expected TemplateSource 'FileSystem', got %q", bp.TemplateSource())
	}
}

// TestAdopt_FileSystemTemplates_Unresolved_LossReport tests that when
// source: FileSystem cannot be resolved, an explicit drop entry is recorded in
// LossReport stating that resources defined in fileSystem.dir could not be adopted.
func TestAdopt_FileSystemTemplates_Unresolved_LossReport(t *testing.T) {
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
      source: FileSystem
      fileSystem:
        dirPath: /nonexistent/templates/dir
`

	bp, report, err := Adopt([]byte(compYAML), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	if len(bp.Spec.Resources) != 0 {
		t.Fatalf("expected 0 resources when unresolved, got %d", len(bp.Spec.Resources))
	}
	if report == nil || !report.HasTrueLoss() {
		t.Fatalf("expected true loss in LossReport when FileSystem templates cannot be resolved, got: %+v", report)
	}

	found := false
	for _, d := range report.Drops {
		if d.Path == "fileSystem.dir" && strings.Contains(d.Reason, "resources defined in fileSystem.dir could not be adopted") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected drop with path 'fileSystem.dir' stating 'resources defined in fileSystem.dir could not be adopted', got drops: %+v", report.Drops)
	}
}

// TestAdopt_FileSystemTemplates_DirField tests that source: FileSystem works with
// the fileSystem.dir field as described in CF-288 step 2.
func TestAdopt_FileSystemTemplates_DirField(t *testing.T) {
	tmpDir := t.TempDir()

	tmplDir := filepath.Join(tmpDir, "templates", "my-comp")
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		t.Fatalf("mkdir tmplDir: %v", err)
	}

	ctxTmpl := `{{- $spec := .observed.composite.resource.spec -}}`
	if err := os.WriteFile(filepath.Join(tmplDir, "000-context.yaml"), []byte(ctxTmpl), 0644); err != nil {
		t.Fatalf("write 000-context.yaml: %v", err)
	}

	resTmpl := `apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  annotations:
    crossplane.io/composition-resource-name: my-bucket
spec:
  forProvider:
    region: {{ $spec.region }}
`
	if err := os.WriteFile(filepath.Join(tmplDir, "001-bucket.yaml"), []byte(resTmpl), 0644); err != nil {
		t.Fatalf("write 001-bucket.yaml: %v", err)
	}

	compYAML := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: my-comp
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
      source: FileSystem
      fileSystem:
        dir: templates/my-comp
`

	bp, report, err := Adopt([]byte(compYAML), Options{
		SourceDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if bp == nil {
		t.Fatal("expected non-nil blueprint")
	}
	if len(dropsBeyondXRDless(report)) > 0 {
		t.Fatalf("unexpected drops: %+v", dropsBeyondXRDless(report))
	}
	if len(bp.Spec.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(bp.Spec.Resources))
	}
	if bp.Spec.Resources[0].Name != "my-bucket" {
		t.Errorf("expected resource name 'my-bucket', got %q", bp.Spec.Resources[0].Name)
	}
	if bp.Spec.Resources[0].Kind != "Bucket" {
		t.Errorf("expected resource kind 'Bucket', got %q", bp.Spec.Resources[0].Kind)
	}
}
