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
apiVersion: factory.crossplane.io/v1alpha1
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

func TestKCLStatusWireReadsStatusPathOnce(t *testing.T) {
	got := kclStructuredRHS(StructuredRHS{Kind: RHSStatus, Resource: "role", StatusPath: "atProvider.arn"}, "")
	want := `ocds?["role"]?.Resource?.status?.atProvider?.arn`
	if got != want {
		t.Errorf("kclStructuredRHS = %q, want %q", got, want)
	}

	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	out, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatal(err)
	}
	line := lineContaining(t, string(out), "queueUrl =")
	if n := strings.Count(line, "atProvider"); n != 1 {
		t.Errorf("queueUrl wire mentions atProvider %d times, want 1:\n%s", n, line)
	}
	if !strings.Contains(line, `ocds?["main-queue"]?.Resource?.status?.atProvider?.url`) {
		t.Errorf("queueUrl wire = %s", line)
	}
}

func TestTranslateForEachToKCL_StatusBoundReadsPathOnce(t *testing.T) {
	got := translateForEachToKCL("resources.main-queue.status.atProvider.nodeCount")
	want := `range(0, int(ocds?["main-queue"]?.Resource?.status?.atProvider?.nodeCount or 0))`
	if got != want {
		t.Errorf("translateForEachToKCL = %q, want %q", got, want)
	}
}

func TestKCLEnvelopeNesting(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-env
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
      secretName:
        type: string
        required: true
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
      envelope:
        writeConnectionSecretToRef.name:
          from: params.secretName
        writeConnectionSecretToRef.namespace:
          value: crossplane-system
        managementPolicies:
          value: "*"
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
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
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
              writeConnectionSecretToRef:
                type: object
                required: [name, namespace]
                properties: {name: {type: string}, namespace: {type: string}}
              managementPolicies:
                type: array
                items: {type: string}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "writeConnectionSecretToRef.name") {
		t.Errorf("flattened dot path found in KCL output:\n%s", s)
	}
	if !strings.Contains(s, "writeConnectionSecretToRef = {\n") {
		t.Errorf("expected nested writeConnectionSecretToRef object in KCL:\n%s", s)
	}
	if !strings.Contains(s, "name = _spec?.secretName") {
		t.Errorf("expected name child in KCL:\n%s", s)
	}
	if !strings.Contains(s, `namespace = "crossplane-system"`) {
		t.Errorf("expected namespace child in KCL:\n%s", s)
	}
}

func TestKCLRefusesTemplateConventions(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-conv
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
  templates:
    cf.name: "{{ .xr }}-{{ .resource }}"
  conventions:
    - match: name
      template: cf.name
  resources:
    - name: work-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: us-east-1
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	_ = os.WriteFile(p, []byte(bpYAML), 0600)
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
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
                properties: {region: {type: string}}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, _ := schema.ParseCRDs([][]byte{crdDoc})
	_, err = Composition(b, crds)
	if err == nil {
		t.Fatal("expected error on KCL with conventions, got nil")
	}
	if !strings.Contains(err.Error(), "conventions") {
		t.Errorf("expected error to mention conventions, got: %v", err)
	}
}

// lineContaining returns the single line of s that contains needle.
func lineContaining(t *testing.T, s, needle string) string {
	t.Helper()
	var hits []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, needle) {
			hits = append(hits, strings.TrimSpace(l))
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly one line containing %q, got %d:\n%s", needle, len(hits), s)
	}
	return hits[0]
}
