package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/emit"
)

// TestCF196_AdoptMultiDocEnvironmentConfig tests that adopt.Adopt parses EnvironmentConfig
// documents in multi-document streams, populating bp.Spec.Environment and bp.Spec.EnvironmentConfigs.
func TestCF196_AdoptMultiDocEnvironmentConfig(t *testing.T) {
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

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	// EnvironmentConfig must be parsed into bp.Spec.Environment
	if bp.Spec.Environment == nil {
		t.Fatalf("expected bp.Spec.Environment to be populated, got nil")
	}
	envKey, ok := bp.Spec.Environment["clusterRegion"]
	if !ok {
		t.Fatalf("expected environment key 'clusterRegion' in bp.Spec.Environment, got: %+v", bp.Spec.Environment)
	}
	if envKey.Type != "string" {
		t.Errorf("expected clusterRegion.Type == 'string', got %q", envKey.Type)
	}

	// EnvironmentConfig must be parsed into bp.Spec.EnvironmentConfigs
	if len(bp.Spec.EnvironmentConfigs) != 1 {
		t.Fatalf("expected 1 EnvironmentConfig in bp.Spec.EnvironmentConfigs, got %d", len(bp.Spec.EnvironmentConfigs))
	}
	cfg := bp.Spec.EnvironmentConfigs[0]
	if cfg.Name != "default" {
		t.Errorf("expected cfg.Name == 'default', got %q", cfg.Name)
	}
	if cfg.Data == nil || cfg.Data["clusterRegion"] != "us-east-1" {
		t.Errorf("expected cfg.Data['clusterRegion'] == 'us-east-1', got: %+v", cfg.Data)
	}

	// No true loss should be reported for this clean EnvironmentConfig
	if report.HasTrueLoss() {
		t.Errorf("expected no true loss, got: %+v", report.Drops)
	}
}

// TestCF196_AdoptMultiDocEnvironmentConfig_WithLabels tests that metadata.labels
// are converted to Selector.MatchLabels on EnvironmentConfig.
func TestCF196_AdoptMultiDocEnvironmentConfig_WithLabels(t *testing.T) {
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
  name: cluster-prod-eu
  labels:
    stage: production
    region: eu-west-1
data:
  clusterName: prod-eu-01
  nodeCount: 10
  isProtected: true
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if len(bp.Spec.EnvironmentConfigs) != 1 {
		t.Fatalf("expected 1 EnvironmentConfig, got %d", len(bp.Spec.EnvironmentConfigs))
	}
	cfg := bp.Spec.EnvironmentConfigs[0]
	if cfg.Name != "cluster-prod-eu" {
		t.Errorf("expected cfg.Name == 'cluster-prod-eu', got %q", cfg.Name)
	}
	if cfg.Selector == nil || cfg.Selector.MatchLabels == nil {
		t.Fatalf("expected selector.matchLabels, got nil")
	}
	if cfg.Selector.MatchLabels["stage"] != "production" {
		t.Errorf("expected stage == production, got %q", cfg.Selector.MatchLabels["stage"])
	}
	if cfg.Selector.MatchLabels["region"] != "eu-west-1" {
		t.Errorf("expected region == eu-west-1, got %q", cfg.Selector.MatchLabels["region"])
	}

	// Verify data and scalar types in bp.Spec.Environment
	if bp.Spec.Environment["clusterName"].Type != "string" {
		t.Errorf("clusterName type = %q, want 'string'", bp.Spec.Environment["clusterName"].Type)
	}
	if bp.Spec.Environment["nodeCount"].Type != "integer" {
		t.Errorf("nodeCount type = %q, want 'integer'", bp.Spec.Environment["nodeCount"].Type)
	}
	if bp.Spec.Environment["isProtected"].Type != "boolean" {
		t.Errorf("isProtected type = %q, want 'boolean'", bp.Spec.Environment["isProtected"].Type)
	}

	if cfg.Data["clusterName"] != "prod-eu-01" || cfg.Data["nodeCount"] != "10" || cfg.Data["isProtected"] != "true" {
		t.Errorf("unexpected cfg.Data: %+v", cfg.Data)
	}

	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}
}

// TestCF196_AdoptMultiDocEnvironmentConfig_LossReport tests that invalid or unconvertible
// fields in EnvironmentConfig documents are recorded in LossReport.
func TestCF196_AdoptMultiDocEnvironmentConfig_LossReport(t *testing.T) {
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
  annotations:
    custom.io/note: unconvertible-annotation
spec:
  unsupportedSpecField: true
data:
  validKey: hello
  invalid-key-name: bad
  nestedObject:
    subKey: val
  arrayValue:
    - one
    - two
`

	bp, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}

	if !report.HasTrueLoss() {
		t.Fatalf("expected true loss in report, got none: %+v", report.Drops)
	}

	// Verify specific drops are recorded
	hasDrop := func(pathPrefix string) bool {
		for _, d := range report.Drops {
			if len(d.Path) >= len(pathPrefix) && d.Path[:len(pathPrefix)] == pathPrefix {
				return true
			}
		}
		return false
	}

	if !hasDrop("environmentConfig.default.data.invalid-key-name") {
		t.Errorf("expected drop for invalid-key-name, got drops: %+v", report.Drops)
	}
	if !hasDrop("environmentConfig.default.data.nestedObject") {
		t.Errorf("expected drop for nestedObject, got drops: %+v", report.Drops)
	}
	if !hasDrop("environmentConfig.default.data.arrayValue") {
		t.Errorf("expected drop for arrayValue, got drops: %+v", report.Drops)
	}
	if !hasDrop("environmentConfig.default.spec") {
		t.Errorf("expected drop for unsupported spec, got drops: %+v", report.Drops)
	}
	if !hasDrop("environmentConfig.default.metadata.annotations") {
		t.Errorf("expected drop for annotations, got drops: %+v", report.Drops)
	}

	// validKey should still be retained
	if bp.Spec.Environment["validKey"].Type != "string" {
		t.Errorf("validKey type = %q, want 'string'", bp.Spec.Environment["validKey"].Type)
	}

	// Blueprint must still be valid
	if err := bp.Validate(); err != nil {
		t.Fatalf("blueprint validate failed: %v", err)
	}
}

// TestCF196_AdoptMultiDocEnvironmentConfig_RoundTrip verifies that adopting an EnvironmentConfig
// and re-emitting it preserves the data and selectors byte-for-byte.
func TestCF196_AdoptMultiDocEnvironmentConfig_RoundTrip(t *testing.T) {
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
  name: cluster-prod-eu
  labels:
    cluster: prod-eu
data:
  clusterName: prod-eu
  region: eu-west-1
`

	bpAdopted, report, err := Adopt([]byte(manifest), Options{})
	if err != nil {
		t.Fatalf("Adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}

	if len(bpAdopted.Spec.EnvironmentConfigs) != 1 {
		t.Fatalf("expected 1 EnvironmentConfig, got %d", len(bpAdopted.Spec.EnvironmentConfigs))
	}
	cfg := bpAdopted.Spec.EnvironmentConfigs[0]
	emittedBytes, err := emit.EnvironmentConfig(bpAdopted, cfg)
	if err != nil {
		t.Fatalf("emit.EnvironmentConfig failed: %v", err)
	}

	emittedStr := string(emittedBytes)
	if !strings.Contains(emittedStr, "kind: EnvironmentConfig") {
		t.Errorf("expected kind: EnvironmentConfig in emitted YAML, got:\n%s", emittedStr)
	}
	if !strings.Contains(emittedStr, "name: cluster-prod-eu") {
		t.Errorf("expected name: cluster-prod-eu, got:\n%s", emittedStr)
	}
	if !strings.Contains(emittedStr, "cluster: prod-eu") {
		t.Errorf("expected cluster: prod-eu label, got:\n%s", emittedStr)
	}
	if !strings.Contains(emittedStr, "clusterName: \"prod-eu\"") && !strings.Contains(emittedStr, "clusterName: prod-eu") {
		t.Errorf("expected clusterName data, got:\n%s", emittedStr)
	}
	if !strings.Contains(emittedStr, "region: \"eu-west-1\"") && !strings.Contains(emittedStr, "region: eu-west-1") {
		t.Errorf("expected region data, got:\n%s", emittedStr)
	}
}

// TestCF196_AdoptTree_EnvironmentConfig tests that AdoptTree also correctly processes
// EnvironmentConfig documents and populates bp.Spec.Environment and bp.Spec.EnvironmentConfigs.
func TestCF196_AdoptTree_EnvironmentConfig(t *testing.T) {
	tmpDir := t.TempDir()

	compYaml := `apiVersion: apiextensions.crossplane.io/v1
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
`
	if err := os.WriteFile(filepath.Join(tmpDir, "composition.yaml"), []byte(compYaml), 0644); err != nil {
		t.Fatalf("write composition.yaml: %v", err)
	}

	envConfigYaml := `apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: default
data:
  clusterRegion: us-west-1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "env-config.yaml"), []byte(envConfigYaml), 0644); err != nil {
		t.Fatalf("write env-config.yaml: %v", err)
	}

	bp, report, err := AdoptTree(tmpDir, Options{})
	if err != nil {
		t.Fatalf("AdoptTree failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("unexpected true loss: %+v", report.Drops)
	}

	if bp.Spec.Environment == nil || bp.Spec.Environment["clusterRegion"].Type != "string" {
		t.Errorf("expected clusterRegion in bp.Spec.Environment, got: %+v", bp.Spec.Environment)
	}
	if len(bp.Spec.EnvironmentConfigs) != 1 || bp.Spec.EnvironmentConfigs[0].Data["clusterRegion"] != "us-west-1" {
		t.Errorf("expected default EnvironmentConfig with clusterRegion: us-west-1, got: %+v", bp.Spec.EnvironmentConfigs)
	}
}
