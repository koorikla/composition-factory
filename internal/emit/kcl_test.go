package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestEmitKCLComposition(t *testing.T) {
	bpYAML := `
apiVersion: compositionfactory.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  emit:
    engine: kcl
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XQueue
    plural: xqueues
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      region:
        type: string
        description: Target AWS region
      retention:
        type: integer
        default: "345600"
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          from: params.region
        messageRetentionSeconds:
          from: params.retention
`

	dir := t.TempDir()
	path := filepath.Join(dir, "xqueue.cf.yaml")
	if err := os.WriteFile(path, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}

	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint.Load: %v", err)
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  messageRetentionSeconds: {type: integer}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)

	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	compBytes, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}

	compStr := string(compBytes)

	// Verify function-kcl step
	if !strings.Contains(compStr, "name: function-kcl") {
		t.Errorf("expected function-kcl in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "apiVersion: krm.kcl.dev/v1alpha1") {
		t.Errorf("expected krm.kcl.dev/v1alpha1 apiVersion in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "kind: KCLInput") {
		t.Errorf("expected KCLInput kind in composition, got:\n%s", compStr)
	}

	// Verify KCL source content
	if !strings.Contains(compStr, `oxr = option("params")?.oxr or {}`) {
		t.Errorf("expected oxr preamble in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `"krm.kcl.dev/composition-resource-name" = "work-queue"`) {
		t.Errorf("expected composition-resource-name in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "region = _spec?.region") {
		t.Errorf("expected region field wire in KCL, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "messageRetentionSeconds = _spec?.retention") {
		t.Errorf("expected messageRetentionSeconds field wire in KCL, got:\n%s", compStr)
	}

	// Verify functions.yaml
	fnBytes, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	fnStr := string(fnBytes)
	if !strings.Contains(fnStr, "name: function-kcl") {
		t.Errorf("expected function-kcl in functions.yaml, got:\n%s", fnStr)
	}
	if !strings.Contains(fnStr, "package: xpkg.upbound.io/crossplane-contrib/function-kcl:v0.11.2") {
		t.Errorf("expected function-kcl package in functions.yaml, got:\n%s", fnStr)
	}
}

func TestTranslateWhenToKCL_BooleanSubstrings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "params.is_true_enabled == true",
			want: "_spec?.is_true_enabled == True",
		},
		{
			in:   "params.use_false_fallback == false",
			want: "_spec?.use_false_fallback == False",
		},
		{
			in:   "params.truename != false",
			want: "_spec?.truename != False",
		},
	}

	for _, tc := range cases {
		got := translateWhenToKCL(tc.in)
		if got != tc.want {
			t.Errorf("translateWhenToKCL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
