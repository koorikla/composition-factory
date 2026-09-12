package adopt

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
)

func TestCF466_AdoptEnvironment_PreservesAnnotationDeclaredTypesAndRoundtrips(t *testing.T) {
	cacheDir, err := filepath.Abs("../../tests/fixtures/cache")
	if err != nil {
		t.Fatalf("resolve cache dir: %v", err)
	}

	rawBlueprint := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: env-roundtrip
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XEnvQueue
    plural: xenvqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    enabled:      {type: boolean, default: "true"}
    retries:      {type: integer, default: "3"}
    delaySeconds: {type: integer, default: "30"}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
      fields:
        region:        {value: eu-north-1}
        delaySeconds:  {from: env.delaySeconds}
    - name: gated-queue
      kind: Queue
      provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
      when: env.enabled
      forEach: env.retries
      fields:
        region: {value: eu-north-1}
`

	bp, err := blueprint.Parse([]byte(rawBlueprint))
	if err != nil {
		t.Fatalf("parse blueprint: %v", err)
	}

	store := cache.New(cacheDir)
	crds, err := store.Load("ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0")
	if err != nil {
		t.Fatalf("load CRDs: %v", err)
	}

	compYAML, err := emit.Composition(bp, crds)
	if err != nil {
		t.Fatalf("initial emit failed: %v", err)
	}

	if !strings.Contains(string(compYAML), blueprint.EnvironmentKeysAnnotation) {
		t.Fatalf("generated composition missing annotation %s", blueprint.EnvironmentKeysAnnotation)
	}

	fnsYAML, err := emit.Functions(bp)
	if err != nil {
		t.Fatalf("emit.Functions failed: %v", err)
	}
	manifest := string(compYAML) + "\n---\n" + string(fnsYAML)

	adoptedBP, report, err := Adopt([]byte(manifest), Options{
		CacheDir: cacheDir,
		Store:    store,
	})
	if err != nil {
		t.Fatalf("adopt failed: %v", err)
	}
	if report.HasTrueLoss() {
		t.Errorf("expected no true loss, got: %+v", report.Drops)
	}

	envKey, ok := adoptedBP.Spec.Environment["delaySeconds"]
	if !ok {
		t.Fatalf("missing delaySeconds in adopted environment")
	}
	if envKey.Type != "integer" {
		t.Errorf("delaySeconds type = %q, want %q", envKey.Type, "integer")
	}
	if envKey.Default != "30" {
		t.Errorf("delaySeconds default = %q, want %q", envKey.Default, "30")
	}

	retriesKey, ok := adoptedBP.Spec.Environment["retries"]
	if !ok {
		t.Fatalf("missing retries in adopted environment")
	}
	if retriesKey.Type != "integer" {
		t.Errorf("retries type = %q, want %q", retriesKey.Type, "integer")
	}

	enabledKey, ok := adoptedBP.Spec.Environment["enabled"]
	if !ok {
		t.Fatalf("missing enabled in adopted environment")
	}
	if enabledKey.Type != "boolean" {
		t.Errorf("enabled type = %q, want %q", enabledKey.Type, "boolean")
	}

	// Must be able to regenerate without error
	_, err = emit.Composition(adoptedBP, crds)
	if err != nil {
		t.Fatalf("re-emit failed on adopted blueprint: %v", err)
	}
}

func TestCF466_AdoptEnvironment_WithoutAnnotationReportsContradictionInLossReport(t *testing.T) {
	cacheDir, err := filepath.Abs("../../tests/fixtures/cache")
	if err != nil {
		t.Fatalf("resolve cache dir: %v", err)
	}

	// Composition without factory.crossplane.io/environment-keys annotation
	manifest := `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xenvqueues.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XEnvQueue
  mode: Pipeline
  pipeline:
    - step: environment-configs
      functionRef:
        name: function-environment-configs
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            {{- $env := index .context "apiextensions.crossplane.io/environment" | default dict -}}
            ---
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: {{ $xr }}-main-queue
            spec:
              forProvider:
                region: eu-north-1
                delaySeconds: {{ ternary (index $env "delaySeconds") 30 (hasKey $env "delaySeconds") }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

	store := cache.New(cacheDir)
	adoptedBP, report, err := Adopt([]byte(manifest), Options{
		CacheDir:           cacheDir,
		Store:              store,
		DefaultProviderRef: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
	})
	if err != nil {
		t.Fatalf("adopt failed: %v", err)
	}

	envKey, ok := adoptedBP.Spec.Environment["delaySeconds"]
	if !ok {
		t.Fatalf("missing delaySeconds in adopted environment")
	}
	if envKey.Type != "string" {
		t.Errorf("without annotation, delaySeconds inferred type = %q, want %q", envKey.Type, "string")
	}

	if !report.IsLossy() {
		t.Fatalf("expected loss report to record contradiction, but report.IsLossy() is false")
	}

	found := false
	for _, drop := range report.Drops {
		if drop.Path == "environment.delaySeconds" && strings.Contains(drop.Reason, "contradicts") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected drop for environment.delaySeconds mentioning contradiction, got drops: %+v", report.Drops)
	}
}
