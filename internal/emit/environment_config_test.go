package emit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

func TestEnvironmentConfigOmittedKeysTyping(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-app"},
		Spec: blueprint.Spec{
			Environment: map[string]blueprint.EnvironmentKey{
				"replicaCount": {Type: "integer"},
				"scaleFactor":  {Type: "number"},
				"enabled":      {Type: "boolean"},
				"endpoint":     {Type: "string"},
			},
		},
	}

	t.Run("omitted keys without default", func(t *testing.T) {
		cfg := blueprint.EnvironmentConfig{
			Name: "dev",
			Data: map[string]string{
				"endpoint": "https://dev.example.org",
			},
		}

		out, err := EnvironmentConfig(bp, cfg)
		if err != nil {
			t.Fatalf("EnvironmentConfig failed: %v", err)
		}

		outStr := string(out)

		if !strings.Contains(outStr, "replicaCount: 0") {
			t.Errorf("expected 'replicaCount: 0', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `replicaCount: ""`) {
			t.Errorf("expected replicaCount not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, "scaleFactor: 0.0") {
			t.Errorf("expected 'scaleFactor: 0.0', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `scaleFactor: ""`) {
			t.Errorf("expected scaleFactor not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, "enabled: false") {
			t.Errorf("expected 'enabled: false', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `enabled: ""`) {
			t.Errorf("expected enabled not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, `endpoint: "https://dev.example.org"`) {
			t.Errorf("expected endpoint: \"https://dev.example.org\", got:\n%s", outStr)
		}

		var doc struct {
			Data map[string]any `json:"data"`
		}
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("yaml.Unmarshal failed: %v", err)
		}
		if _, ok := doc.Data["replicaCount"].(string); ok {
			t.Errorf("expected replicaCount to not unmarshal as string, got %T: %v", doc.Data["replicaCount"], doc.Data["replicaCount"])
		}
		if _, ok := doc.Data["scaleFactor"].(string); ok {
			t.Errorf("expected scaleFactor to not unmarshal as string, got %T: %v", doc.Data["scaleFactor"], doc.Data["scaleFactor"])
		}
		if _, ok := doc.Data["enabled"].(string); ok {
			t.Errorf("expected enabled to not unmarshal as string, got %T: %v", doc.Data["enabled"], doc.Data["enabled"])
		}
	})

	t.Run("empty string values in data", func(t *testing.T) {
		cfg := blueprint.EnvironmentConfig{
			Name: "dev-empty",
			Data: map[string]string{
				"replicaCount": "",
				"scaleFactor":  "",
				"enabled":      "",
				"endpoint":     "",
			},
		}

		out, err := EnvironmentConfig(bp, cfg)
		if err != nil {
			t.Fatalf("EnvironmentConfig failed: %v", err)
		}

		outStr := string(out)

		if !strings.Contains(outStr, "replicaCount: 0") {
			t.Errorf("expected 'replicaCount: 0', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `replicaCount: ""`) {
			t.Errorf("expected replicaCount not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, "scaleFactor: 0.0") {
			t.Errorf("expected 'scaleFactor: 0.0', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `scaleFactor: ""`) {
			t.Errorf("expected scaleFactor not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, "enabled: false") {
			t.Errorf("expected 'enabled: false', got:\n%s", outStr)
		}
		if strings.Contains(outStr, `enabled: ""`) {
			t.Errorf("expected enabled not to be empty string, got:\n%s", outStr)
		}

		if !strings.Contains(outStr, `endpoint: ""`) {
			t.Errorf("expected endpoint: \"\", got:\n%s", outStr)
		}

		var doc struct {
			Data map[string]any `json:"data"`
		}
		if err := yaml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("yaml.Unmarshal failed: %v", err)
		}
		if _, ok := doc.Data["replicaCount"].(string); ok {
			t.Errorf("expected replicaCount to not unmarshal as string, got %T: %v", doc.Data["replicaCount"], doc.Data["replicaCount"])
		}
		if _, ok := doc.Data["scaleFactor"].(string); ok {
			t.Errorf("expected scaleFactor to not unmarshal as string, got %T: %v", doc.Data["scaleFactor"], doc.Data["scaleFactor"])
		}
		if _, ok := doc.Data["enabled"].(string); ok {
			t.Errorf("expected enabled to not unmarshal as string, got %T: %v", doc.Data["enabled"], doc.Data["enabled"])
		}
	})
}

// CF-344: EnvironmentConfig data keys named with YAML 1.1 keywords (on, off, yes, no, etc.)
// must be emitted quoted so Kubernetes YAML decoders (sigs.k8s.io/yaml) decode them as strings,
// rather than collapsing them into booleans.
func TestEnvironmentConfigKeywordDataKeys(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "test-app"},
		Spec: blueprint.Spec{
			Environment: map[string]blueprint.EnvironmentKey{
				"on":           {Type: "string"},
				"off":          {Type: "string"},
				"yes":          {Type: "string"},
				"no":           {Type: "string"},
				"providerName": {Type: "string"},
			},
		},
	}

	cfg := blueprint.EnvironmentConfig{
		Name: "dev",
		Data: map[string]string{
			"on":           "alpha",
			"off":          "beta",
			"yes":          "gamma",
			"no":           "delta",
			"providerName": "prod",
		},
	}

	out, err := EnvironmentConfig(bp, cfg)
	if err != nil {
		t.Fatalf("EnvironmentConfig failed: %v", err)
	}

	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\n---\n%s", err, out)
	}

	for _, wantKey := range []string{"on", "off", "yes", "no", "providerName"} {
		if _, ok := doc.Data[wantKey]; !ok {
			t.Errorf("data missing key %q under sigs.k8s.io/yaml decoding: %v", wantKey, doc.Data)
		}
	}
	if _, ok := doc.Data["true"]; ok {
		t.Errorf("data contains key 'true', indicating boolean collapse: %v", doc.Data)
	}
	if _, ok := doc.Data["false"]; ok {
		t.Errorf("data contains key 'false', indicating boolean collapse: %v", doc.Data)
	}
}

// CF-398: When spec.environmentConfigs[].name is omitted and selector.matchLabels contains
// domain prefixes (e.g. environment.crossplane.io/stage: prod), emit.Generate must write
// a flat YAML file at out/environmentconfigs/<sanitized-name>.yaml and metadata.name must
// be a valid RFC 1123 DNS subdomain without slashes.
func TestEnvironmentConfigSynthesizedName_EmitsFlatPathAndValidName(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-env-dns
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    stage:
      type: string
  environmentConfigs:
    - selector:
        matchLabels:
          "environment.crossplane.io/stage": "prod"
      data:
        stage: "prod"
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

	wantPath := filepath.ToSlash(filepath.Join(outDir, "environmentconfigs", "environment-crossplane-io-stage-prod.yaml"))
	var envConfigOutput *Output
	for i := range outputs {
		p := filepath.ToSlash(outputs[i].Path)
		if strings.Contains(p, "environment.crossplane.io") {
			t.Errorf("output path %q leaked unflattened domain slash/subdirectory", p)
		}
		if p == wantPath {
			envConfigOutput = &outputs[i]
		}
	}

	if envConfigOutput == nil {
		t.Fatalf("expected %s in generate outputs, got: %v", wantPath, outputs)
	}

	body := string(envConfigOutput.Body)
	if !strings.Contains(body, "name: environment-crossplane-io-stage-prod") {
		t.Errorf("expected metadata.name: environment-crossplane-io-stage-prod, got:\n%s", body)
	}
	if strings.Contains(body, "name: environment.crossplane.io") {
		t.Errorf("expected metadata.name not to contain domain prefix, got:\n%s", body)
	}
	if !strings.Contains(body, "'environment.crossplane.io/stage': prod") && !strings.Contains(body, "environment.crossplane.io/stage: prod") {
		t.Errorf("expected original label key in labels, got:\n%s", body)
	}
}
