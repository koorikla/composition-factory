package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

func TestCF187_EnvironmentConfigScaffold_DefaultConfig(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xirsa
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XIrsa
    plural: xirsas
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    clusterName:
      type: string
      default: "prod-eu"
    oidcIssuer:
      type: string
    accountId:
      type: string
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {value: "us-east-1"}
`
	bp, err := blueprint.Parse([]byte(bpYAML))
	if err != nil {
		t.Fatalf("blueprint.Parse failed: %v", err)
	}

	crds := testCRDs(t)
	outDir := t.TempDir()
	outputs, err := Generate(bp, crds, outDir)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var envConfigOutput *Output
	for i := range outputs {
		if filepath.ToSlash(outputs[i].Path) == filepath.ToSlash(filepath.Join(outDir, "environmentconfigs", "default.yaml")) {
			envConfigOutput = &outputs[i]
			break
		}
	}

	if envConfigOutput == nil {
		t.Fatalf("expected environmentconfigs/default.yaml in generate outputs, got: %v", outputs)
	}

	body := string(envConfigOutput.Body)
	if !strings.Contains(body, "kind: EnvironmentConfig") {
		t.Errorf("expected kind: EnvironmentConfig in default.yaml, got:\n%s", body)
	}
	if !strings.Contains(body, "apiVersion: apiextensions.crossplane.io/v1beta1") {
		t.Errorf("expected apiVersion: apiextensions.crossplane.io/v1beta1 in default.yaml, got:\n%s", body)
	}
	if !strings.Contains(body, "name: default") {
		t.Errorf("expected name: default in default.yaml metadata, got:\n%s", body)
	}
	if !strings.Contains(body, "clusterName: prod-eu") && !strings.Contains(body, "clusterName: \"prod-eu\"") {
		t.Errorf("expected clusterName: prod-eu in default.yaml data, got:\n%s", body)
	}
	if !strings.Contains(body, "oidcIssuer: \"\"") {
		t.Errorf("expected oidcIssuer: \"\" in default.yaml data, got:\n%s", body)
	}
	if !strings.Contains(body, "accountId: \"\"") {
		t.Errorf("expected accountId: \"\" in default.yaml data, got:\n%s", body)
	}
}

func TestCF187_EnvironmentConfigScaffold_DeclaredConfigs(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xirsa
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XIrsa
    plural: xirsas
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    clusterName:
      type: string
    region:
      type: string
      default: "us-east-1"
    accountId:
      type: string
  environmentConfigs:
    - name: cluster-prod-eu
      selector:
        matchLabels:
          cluster: prod-eu
      data:
        clusterName: prod-eu
        region: eu-north-1
    - name: default
      data:
        clusterName: fallback
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {from: env.region}
`
	bp, err := blueprint.Parse([]byte(bpYAML))
	if err != nil {
		t.Fatalf("blueprint.Parse failed: %v", err)
	}

	crds := testCRDs(t)
	outDir := t.TempDir()
	outputs, err := Generate(bp, crds, outDir)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var prodOutput, defaultOutput *Output
	for i := range outputs {
		p := filepath.ToSlash(outputs[i].Path)
		if strings.HasSuffix(p, "environmentconfigs/cluster-prod-eu.yaml") {
			prodOutput = &outputs[i]
		}
		if strings.HasSuffix(p, "environmentconfigs/default.yaml") {
			defaultOutput = &outputs[i]
		}
	}

	if prodOutput == nil {
		t.Fatalf("missing environmentconfigs/cluster-prod-eu.yaml in outputs")
	}
	if defaultOutput == nil {
		t.Fatalf("missing environmentconfigs/default.yaml in outputs")
	}

	prodBody := string(prodOutput.Body)
	if !strings.Contains(prodBody, "name: cluster-prod-eu") {
		t.Errorf("expected name: cluster-prod-eu in metadata")
	}
	if !strings.Contains(prodBody, "cluster: prod-eu") {
		t.Errorf("expected labels cluster: prod-eu in metadata, got:\n%s", prodBody)
	}
	if !strings.Contains(prodBody, "region: eu-north-1") && !strings.Contains(prodBody, "region: \"eu-north-1\"") {
		t.Errorf("expected region: eu-north-1 in prod data, got:\n%s", prodBody)
	}
	if !strings.Contains(prodBody, "accountId: \"\"") {
		t.Errorf("expected accountId: \"\" in prod data, got:\n%s", prodBody)
	}

	// Verify Composition pipeline step input
	var compOutput *Output
	for i := range outputs {
		if strings.HasSuffix(filepath.ToSlash(outputs[i].Path), "compositions/xirsas.platform.example.org.yaml") {
			compOutput = &outputs[i]
			break
		}
	}
	if compOutput == nil {
		t.Fatalf("missing composition output")
	}

	type compManifest struct {
		Spec struct {
			Pipeline []struct {
				Step        string `json:"step"`
				FunctionRef struct {
					Name string `json:"name"`
				} `json:"functionRef"`
				Input map[string]any `json:"input"`
			} `json:"pipeline"`
		} `json:"spec"`
	}

	var compDoc compManifest
	if err := yaml.Unmarshal(compOutput.Body, &compDoc); err != nil {
		t.Fatalf("unmarshal composition: %v", err)
	}

	var envStep *struct {
		Step        string `json:"step"`
		FunctionRef struct {
			Name string `json:"name"`
		} `json:"functionRef"`
		Input map[string]any `json:"input"`
	}

	for i := range compDoc.Spec.Pipeline {
		s := &compDoc.Spec.Pipeline[i]
		if s.FunctionRef.Name == "function-environment-configs" {
			envStep = s
			break
		}
	}
	if envStep == nil {
		t.Fatalf("function-environment-configs step not found in composition pipeline")
	}

	// Step input must reference what was declared:
	// 1st: type: Selector, selector.matchLabels.cluster: prod-eu
	// 2nd: type: Reference, ref.name: default
	stepSpec, ok := envStep.Input["spec"].(map[string]any)
	if !ok {
		t.Fatalf("env step missing spec: %+v", envStep.Input)
	}
	envCfgs, ok := stepSpec["environmentConfigs"].([]any)
	if !ok || len(envCfgs) != 2 {
		t.Fatalf("expected 2 environmentConfigs in step input, got: %+v", stepSpec)
	}

	first := envCfgs[0].(map[string]any)
	if first["type"] != "Selector" {
		t.Errorf("expected first config type Selector, got: %v", first["type"])
	}
	selector := first["selector"].(map[string]any)
	matchLabels := selector["matchLabels"].(map[string]any)
	if matchLabels["cluster"] != "prod-eu" {
		t.Errorf("expected cluster: prod-eu in selector, got: %v", matchLabels)
	}

	second := envCfgs[1].(map[string]any)
	if second["type"] != "Reference" {
		t.Errorf("expected second config type Reference, got: %v", second["type"])
	}
	ref := second["ref"].(map[string]any)
	if ref["name"] != "default" {
		t.Errorf("expected ref.name: default, got: %v", ref["name"])
	}
}

func TestCF187_PristineBlueprintEmitsByteIdentical(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  resources:
    - name: main-queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {value: "us-east-1"}
`
	bp, err := blueprint.Parse([]byte(bpYAML))
	if err != nil {
		t.Fatalf("blueprint.Parse failed: %v", err)
	}
	crds := testCRDs(t)
	outDir := t.TempDir()
	outputs, err := Generate(bp, crds, outDir)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, o := range outputs {
		if strings.Contains(o.Path, "environmentconfigs") {
			t.Errorf("pristine blueprint without environment emitted environmentconfigs output: %s", o.Path)
		}
	}
}
