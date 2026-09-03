package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestAdoptCLI(t *testing.T) {
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "composition.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")

	compContent := `
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
                region: {{ $spec.region }}
`

	if err := os.WriteFile(compPath, []byte(compContent), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: compPath,
		Out:         outBlueprintPath,
		Provider:    "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
	}

	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out.String(), "Adopted blueprint written to") {
		t.Errorf("stdout = %q, want mention of written blueprint", out.String())
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read generated blueprint: %v", err)
	}

	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "kind: Blueprint") {
		t.Errorf("blueprint missing 'kind: Blueprint'")
	}
	if !strings.Contains(bpStr, "region:") {
		t.Errorf("blueprint missing parameter 'region'")
	}

	// Verify output cleanliness: no empty strings or null slices
	if strings.Contains(bpStr, `from: ""`) {
		t.Errorf("blueprint contains empty string from: \"\"")
	}
	if strings.Contains(bpStr, `raw: ""`) {
		t.Errorf("blueprint contains empty string raw: \"\"")
	}
	if strings.Contains(bpStr, `conventions: null`) {
		t.Errorf("blueprint contains conventions: null")
	}
	if strings.Contains(bpStr, `enum: null`) {
		t.Errorf("blueprint contains enum: null")
	}
}

func TestAdoptCLILossyExitCode2(t *testing.T) {
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "lossy.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")

	compContent := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.aws.example.org
spec:
  group: aws.example.org
  claimNames:
    kind: Queue
    plural: queues
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
              properties:
                subnets:
                  type: array
                region:
                  type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.example.org
spec:
  compositeTypeRef:
    apiVersion: aws.example.org/v1alpha1
    kind: XQueue
  resources:
    - name: sqs-queue
      base:
        apiVersion: sqs.aws.upbound.io/v1beta1
        kind: Queue
        spec:
          forProvider:
            region: us-east-1
      patches:
        - type: ToCompositeFieldPath
          fromFieldPath: status.atProvider.arn
          toFieldPath: status.arn
`

	if err := os.WriteFile(compPath, []byte(compContent), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: compPath,
		Out:         outBlueprintPath,
		CacheDir:    tmpDir,
	}

	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for lossy adopt", code)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Adopt loss report") {
		t.Errorf("expected loss report in stdout, got: %s", outStr)
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read generated blueprint: %v", err)
	}

	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "# adopt: dropped") {
		t.Errorf("expected dropped comments in output YAML:\n%s", bpStr)
	}
}

func TestAdoptCLIDirectoryTree(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "my-config")
	apisDir := filepath.Join(configDir, "apis", "v1alpha1")
	if err := os.MkdirAll(apisDir, 0755); err != nil {
		t.Fatalf("mkdir apis: %v", err)
	}

	crossplaneYaml := `apiVersion: meta.pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: test-config
spec:
  dependsOn:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs
      version: "=v1.14.0"
`
	if err := os.WriteFile(filepath.Join(configDir, "crossplane.yaml"), []byte(crossplaneYaml), 0644); err != nil {
		t.Fatalf("write crossplane.yaml: %v", err)
	}

	xrdYaml := `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.aws.example.org
spec:
  group: aws.example.org
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
              properties:
                region:
                  type: string
                  default: us-west-2
`
	if err := os.WriteFile(filepath.Join(apisDir, "definition.yaml"), []byte(xrdYaml), 0644); err != nil {
		t.Fatalf("write definition.yaml: %v", err)
	}

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
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
              name: queue-res
            spec:
              forProvider:
                region: {{ $spec.region }}
`
	if err := os.WriteFile(filepath.Join(configDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")
	cmd := &AdoptCmd{
		Composition: configDir,
		Out:         outBlueprintPath,
	}

	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read output blueprint: %v", err)
	}
	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "name: test-config") {
		t.Errorf("blueprint missing package metadata name: %s", bpStr)
	}
	if !strings.Contains(bpStr, "provider-aws-sqs:v1.14.0") {
		t.Errorf("blueprint missing provider dependency: %s", bpStr)
	}
	if !strings.Contains(bpStr, "kind: XQueue") {
		t.Errorf("blueprint missing XRD kind: %s", bpStr)
	}
}

func TestImportCLICommand(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "pkg-config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
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
              name: queue-res
            spec:
              forProvider:
                region: {{ $spec.region }}
`
	if err := os.WriteFile(filepath.Join(configDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")

	var cli CLI
	opts := append(kongOptions(), kong.Exit(func(int) {}))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}

	// Test "import" alias
	ctx, err := parser.Parse([]string{"import", configDir, "-o", outBlueprintPath, "--provider", "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0"})
	if err != nil {
		t.Fatalf("parse import cmd: %v", err)
	}

	var out bytes.Buffer
	ctx.BindTo(&out, (*io.Writer)(nil))
	if err := ctx.Run(); err != nil {
		t.Fatalf("run import cmd: %v", err)
	}

	if _, err := os.Stat(outBlueprintPath); err != nil {
		t.Fatalf("output blueprint not created: %v", err)
	}
}
