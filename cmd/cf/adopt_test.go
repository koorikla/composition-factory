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
	// No XRD alongside: the parameter schema is lost, which is a true loss
	// (exit 2) and is named on screen (CF-108).
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	if !strings.Contains(out.String(), "Adopted blueprint written to") {
		t.Errorf("stdout = %q, want mention of written blueprint", out.String())
	}
	if !strings.Contains(out.String(), "xrd.parameters.region") {
		t.Errorf("stdout = %q, want the XRD-less loss named for region", out.String())
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
	// The XRD travels with the tree so the import is lossless; without it the
	// parameter schema is a named loss and the command exits 2 (CF-108).
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
              required: [region]
              properties:
                region:
                  type: string
`
	if err := os.WriteFile(filepath.Join(configDir, "definition.yaml"), []byte(xrdYaml), 0644); err != nil {
		t.Fatalf("write xrd: %v", err)
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

// CF-108 (#6): `cf adopt <composition.yaml>` with no XRD alongside must name,
// on screen, what it could not recover per parameter, and exit 2.
func TestCF108AdoptCLIWithoutXRDPrintsLossPerParameter(t *testing.T) {
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "composition.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "adopted.cf.yaml")

	compContent := `
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
            ---
            apiVersion: rds.aws.upbound.io/v1beta1
            kind: Instance
            metadata:
              name: db
            spec:
              forProvider:
                region: {{ $spec.region | quote }}
                {{- if hasKey $spec "storageGB" }}
                allocatedStorage: {{ $spec.storageGB }}
                {{- end }}
`
	if err := os.WriteFile(compPath, []byte(compContent), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: compPath,
		Out:         outBlueprintPath,
		Provider:    "xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0",
		CacheDir:    filepath.Join(tmpDir, "cache"),
	}
	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2: adopting without the XRD loses parameter facts", code)
	}
	screen := out.String()
	for _, want := range []string{"Adopt loss report", "xrd.parameters.region", "xrd.parameters.storageGB", "XRD"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen output must contain %q, got:\n%s", want, screen)
		}
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read blueprint: %v", err)
	}
	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "# adopt: dropped xrd.parameters.storageGB") {
		t.Errorf("blueprint must carry the per-parameter loss comment:\n%s", bpStr)
	}
	// region is dereferenced unguarded: it must regenerate as required.
	if !strings.Contains(bpStr, "region:\n    required: true") && !strings.Contains(bpStr, "region:\n        required: true") {
		t.Errorf("region must be adopted as required: true:\n%s", bpStr)
	}
}

func TestCF196_AdoptCLI_MultiDocEnvironmentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "adopted-bp.yaml")

	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
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
---
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: default
data:
  clusterRegion: us-east-1
`

	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: manifestPath,
		Out:         outBlueprintPath,
	}

	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	bpBytes, err := os.ReadFile(outBlueprintPath)
	if err != nil {
		t.Fatalf("read adopted blueprint: %v", err)
	}
	bpStr := string(bpBytes)
	if !strings.Contains(bpStr, "clusterRegion") {
		t.Errorf("expected adopted blueprint to contain clusterRegion, got:\n%s", bpStr)
	}
	if !strings.Contains(bpStr, "environment:") {
		t.Errorf("expected adopted blueprint to contain spec.environment, got:\n%s", bpStr)
	}
	if !strings.Contains(bpStr, "environmentConfigs:") {
		t.Errorf("expected adopted blueprint to contain spec.environmentConfigs, got:\n%s", bpStr)
	}
}

func TestCF207_AdoptCLI_DroppedCompositionSpecFields_ExitCode2(t *testing.T) {
	tmpDir := t.TempDir()
	compPath := filepath.Join(tmpDir, "composition.yaml")
	outBlueprintPath := filepath.Join(tmpDir, "blueprint.yaml")

	compContent := `apiVersion: apiextensions.crossplane.io/v1
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
	if err := os.WriteFile(compPath, []byte(compContent), 0644); err != nil {
		t.Fatalf("write composition: %v", err)
	}

	cmd := &AdoptCmd{
		Composition: compPath,
		Out:         outBlueprintPath,
	}

	var out bytes.Buffer
	code, err := cmd.run(&out)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "spec.writeConnectionSecretsToNamespace") {
		t.Errorf("stdout missing spec.writeConnectionSecretsToNamespace: %s", outStr)
	}
	if !strings.Contains(outStr, "spec.publishConnectionDetailsWithStoreConfigRef") {
		t.Errorf("stdout missing spec.publishConnectionDetailsWithStoreConfigRef: %s", outStr)
	}
}
