package emit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

const (
	testDockerDownOutput = `crossplane: error: cannot create Docker network for rendering: cannot create Docker network "crossplane-render-2t87lbdb": Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?`
	testRemovalOutput    = `crossplane: error: cannot render composition: Error response from daemon: container 1234abcd is marked for removal and cannot be connected`
	testRenderError      = `crossplane: error: cannot render composition: pipeline step "render-templates": run function: template: manifests:12:14: map has no entry for key "maxMessageSize"`

	testOkRenderStream = `---
apiVersion: platform.sparky.ee/v1alpha1
kind: XQueue
metadata:
  name: render-check
  namespace: default
spec:
  providerName: sample
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: dead-letter-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
`

	testInvalidRenderStream = `---
apiVersion: platform.sparky.ee/v1alpha1
kind: XQueue
metadata:
  name: render-check
  namespace: default
spec:
  providerName: sample
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
    visibiltyTimeoutSeconds: 45
`
)

func testBlueprintAndCRDs(t *testing.T) (*blueprint.Blueprint, []schema.CRD) {
	t.Helper()
	b := &blueprint.Blueprint{}
	b.APIVersion = blueprint.APIVersion
	b.Kind = blueprint.Kind
	b.Metadata.Name = "xqueue"
	b.Spec.Sources = []blueprint.Source{{Provider: "example.org/provider-test:v2"}}
	b.Spec.XRD = blueprint.XRD{
		Group:   "platform.sparky.ee",
		Version: "v1alpha1",
		Kind:    "XQueue",
		Plural:  "xqueues",
		Scope:   "Namespaced",
		Parameters: map[string]blueprint.Parameter{
			"providerName":   {Type: "string", Required: true},
			"maxMessageSize": {Type: "integer"},
		},
	}
	b.Spec.Resources = []blueprint.Resource{
		{
			Name:     "main-queue",
			Kind:     "Queue",
			Provider: "example.org/provider-test:v2",
			Fields: map[string]blueprint.Field{
				"region":         {Value: "eu-north-1"},
				"maxMessageSize": {From: "params.maxMessageSize"},
			},
		},
	}

	const crdJSON = `[{"Group":"sqs.aws.m.upbound.io","Kind":"Queue","Plural":"queues","Scope":"Namespaced","Categories":["managed"],` +
		`"Versions":[{"Name":"v1beta1","Served":true,"Storage":true,"Properties":{"spec":{"properties":{"providerConfigRef":{"type":"object","properties":{"kind":{"type":"string"},"name":{"type":"string"}}},"forProvider":{` +
		`"required":["region"],"properties":{"region":{"type":"string"},"maxMessageSize":{"type":"integer"}}}}}}}]}]`

	var crds []schema.CRD
	if err := json.Unmarshal([]byte(crdJSON), &crds); err != nil {
		t.Fatal(err)
	}
	return b, crds
}

func TestRenderCheckMissingCrossplaneCLI(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) {
			return "", &exec.Error{Name: "crossplane", Err: exec.ErrNotFound}
		},
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			t.Fatal("runner must not be called when crossplane CLI is missing")
			return nil, nil
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK || res.Error != "" {
		t.Errorf("res = %+v, want OK: false and Error: empty", res)
	}
	if !strings.Contains(res.Unavailable, "crossplane CLI not found") {
		t.Errorf("res.Unavailable = %q, want it to mention crossplane CLI not found", res.Unavailable)
	}
}

func TestRenderCheckDockerUnavailable(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			return []byte(testDockerDownOutput + "\n"), errors.New("exit status 1")
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK || res.Error != "" {
		t.Errorf("res = %+v, want OK: false and Error: empty", res)
	}
	if res.Unavailable != testDockerDownOutput {
		t.Errorf("res.Unavailable = %q, want %q", res.Unavailable, testDockerDownOutput)
	}
}

func TestRenderCheckContainerMarkedForRemoval(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			return []byte(testRemovalOutput + "\n"), errors.New("exit status 1")
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK || res.Error != "" {
		t.Errorf("res = %+v, want OK: false and Error: empty", res)
	}
	if res.Unavailable != testRemovalOutput {
		t.Errorf("res.Unavailable = %q, want %q", res.Unavailable, testRemovalOutput)
	}
}

func TestRenderCheckCompositionError(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			return []byte(testRenderError + "\n"), errors.New("exit status 1")
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK || res.Unavailable != "" {
		t.Errorf("res = %+v, want OK: false and Unavailable: empty", res)
	}
	if res.Error != testRenderError {
		t.Errorf("res.Error = %q, want %q", res.Error, testRenderError)
	}
}

func TestRenderCheckSuccess(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			return []byte(testOkRenderStream), nil
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.OK || res.Error != "" || res.Unavailable != "" {
		t.Errorf("res = %+v, want OK: true, Error: empty, Unavailable: empty", res)
	}
	if res.Resources != 2 {
		t.Errorf("res.Resources = %d, want 2", res.Resources)
	}
}

func TestRenderCheckSchemaValidationError(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Runner: func(context.Context, string, string, string, string) ([]byte, error) {
			return []byte(testInvalidRenderStream), nil
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK || res.Unavailable != "" {
		t.Errorf("res = %+v, want OK: false, Unavailable: empty", res)
	}
	if !res.ValidationFailed {
		t.Error("res.ValidationFailed = false, want true")
	}
	if !strings.Contains(res.Error, "visibiltyTimeoutSeconds") {
		t.Errorf("res.Error = %q, want it to mention visibiltyTimeoutSeconds", res.Error)
	}
}

func TestRenderCheckTimeout(t *testing.T) {
	b, crds := testBlueprintAndCRDs(t)
	opts := RenderOptions{
		LookPath: func(string) (string, error) { return "/bin/crossplane", nil },
		Timeout:  20 * time.Millisecond,
		Runner: func(ctx context.Context, _, _, _, _ string) ([]byte, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
				return []byte(testOkRenderStream), nil
			}
		},
	}

	res, err := RenderCheck(context.Background(), b, crds, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.OK {
		t.Fatal("res.OK = true, want false on timeout")
	}
	if !strings.Contains(res.Error, "context deadline exceeded") {
		t.Errorf("res.Error = %q, want context deadline exceeded", res.Error)
	}
}

func TestDefaultRenderRunnerRequiredResources(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create mock crossplane binary that echos its arguments.
	mockScript := filepath.Join(binDir, "crossplane")
	scriptContent := "#!/bin/sh\necho \"$@\"\n"
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+origPath)

	workDir := filepath.Join(tempDir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	xrPath := filepath.Join(workDir, "xr.yaml")
	compPath := filepath.Join(workDir, "compositions", "comp.yaml")
	fnsPath := filepath.Join(workDir, "functions.yaml")
	xrdPath := filepath.Join(workDir, "xrds", "xrd.yaml")

	// Case 1: No environmentconfigs directory.
	out, err := DefaultRenderRunner(context.Background(), xrPath, compPath, fnsPath, xrdPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(out), "--required-resources") {
		t.Errorf("expected no --required-resources without environmentconfigs, got: %s", string(out))
	}

	// Case 2: environmentconfigs directory exists with configs.
	envDir := filepath.Join(workDir, "environmentconfigs")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "default.yaml"), []byte("apiVersion: apiextensions.crossplane.io/v1beta1\nkind: EnvironmentConfig\nmetadata:\n  name: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err = DefaultRenderRunner(context.Background(), xrPath, compPath, fnsPath, xrdPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantFlag := "--required-resources " + envDir
	if !strings.Contains(string(out), wantFlag) {
		t.Errorf("expected %q in runner output, got: %s", wantFlag, string(out))
	}
}

func TestAcceptanceRenderCheckEnvironmentConfigs(t *testing.T) {
	if testing.Short() {
		t.Skip("acceptance test needs Docker and crossplane CLI; skipped under -short")
	}
	if _, err := exec.LookPath("crossplane"); err != nil {
		t.Skipf("crossplane CLI not found on PATH: %v", err)
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}

	b, crds := testBlueprintAndCRDs(t)
	b.Spec.Environment = map[string]blueprint.EnvironmentKey{
		"clusterRegion": {
			Type:    "string",
			Default: "us-west-2",
		},
	}
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{
		From: "env.clusterRegion",
	}

	res, err := RenderCheck(context.Background(), b, crds, RenderOptions{})
	if err != nil {
		t.Fatalf("unexpected RenderCheck err: %v", err)
	}
	if res.Unavailable != "" {
		t.Fatalf("unexpected RenderCheck unavailable: %s", res.Unavailable)
	}
	if !res.OK {
		t.Fatalf("RenderCheck failed: %s", res.Error)
	}
	if res.Resources != 1 {
		t.Errorf("res.Resources = %d, want 1", res.Resources)
	}
}

func TestAcceptanceRenderCheckWithEnvironmentConfigs(t *testing.T) {
	if testing.Short() {
		t.Skip("acceptance test needs Docker and crossplane CLI; skipped under -short")
	}
	if _, err := exec.LookPath("crossplane"); err != nil {
		t.Skipf("crossplane CLI not found on PATH: %v", err)
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}

	b, crds := testBlueprintAndCRDs(t)
	b.Spec.Environment = map[string]blueprint.EnvironmentKey{
		"clusterRegion": {
			Type:    "string",
			Default: "us-west-2",
		},
	}
	b.Spec.EnvironmentConfigs = []blueprint.EnvironmentConfig{
		{
			Name: "custom-env",
			Values: map[string]string{
				"clusterRegion": "eu-central-1",
			},
		},
	}
	b.Spec.Resources[0].Fields["region"] = blueprint.Field{
		From: "env.clusterRegion",
	}

	res, err := RenderCheck(context.Background(), b, crds, RenderOptions{})
	if err != nil {
		t.Fatalf("unexpected RenderCheck err: %v", err)
	}
	if res.Unavailable != "" {
		t.Fatalf("unexpected RenderCheck unavailable: %s", res.Unavailable)
	}
	if !res.OK {
		t.Fatalf("RenderCheck failed: %s", res.Error)
	}
	if res.Resources != 1 {
		t.Errorf("res.Resources = %d, want 1", res.Resources)
	}
}
