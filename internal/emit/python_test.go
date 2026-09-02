package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestEmitPythonComposition(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  emit:
    engine: python
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

	// Verify function-python step
	if !strings.Contains(compStr, "name: function-python") {
		t.Errorf("expected function-python in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "apiVersion: python.fn.crossplane.io/v1beta1") {
		t.Errorf("expected python.fn.crossplane.io/v1beta1 apiVersion in composition, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "kind: Script") {
		t.Errorf("expected Script kind in composition, got:\n%s", compStr)
	}

	// Verify Python script content
	if !strings.Contains(compStr, "def compose(req: fnv1.RunFunctionRequest, rsp: fnv1.RunFunctionResponse):") {
		t.Errorf("expected compose signature in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `rsp.desired.resources["work-queue"].resource.update({`) {
		t.Errorf("expected desired.resources[work-queue] update in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `"region": spec.get("region")`) {
		t.Errorf("expected region field wire in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `"messageRetentionSeconds": spec.get("retention")`) {
		t.Errorf("expected messageRetentionSeconds field wire in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `rsp.desired.resources["work-queue"].ready = fnv1.READY_TRUE`) {
		t.Errorf("expected ready assignment in Python, got:\n%s", compStr)
	}

	// Verify functions.yaml
	fnBytes, err := Functions(b)
	if err != nil {
		t.Fatalf("Functions: %v", err)
	}
	fnStr := string(fnBytes)
	if !strings.Contains(fnStr, "name: function-python") {
		t.Errorf("expected function-python in functions.yaml, got:\n%s", fnStr)
	}
	if !strings.Contains(fnStr, "package: xpkg.upbound.io/crossplane-contrib/function-python:v0.5.0") {
		t.Errorf("expected function-python package in functions.yaml, got:\n%s", fnStr)
	}
}

func TestTranslateWhenToPython_BooleanSubstrings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "params.is_true_enabled == true",
			want: "bool(spec.get(\"is_true_enabled\")) is True",
		},
		{
			in:   "params.use_false_fallback == false",
			want: "bool(spec.get(\"use_false_fallback\")) is False",
		},
		{
			in:   "params.truename != false",
			want: "bool(spec.get(\"truename\")) is not False",
		},
	}

	for _, tc := range cases {
		got := translateWhenToPython(tc.in)
		if got != tc.want {
			t.Errorf("translateWhenToPython(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPythonStatusWireWalksResourceStatus(t *testing.T) {
	got := pythonStructuredRHS(structuredRHS{kind: rhsStatus, resource: "role", statusPath: "atProvider.arn"}, "")
	want := `ocds.get("role", {}).get("resource", {}).get("status", {}).get("atProvider", {}).get("arn")`
	if got != want {
		t.Errorf("pythonStructuredRHS = %q, want %q", got, want)
	}

	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	out, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatal(err)
	}
	line := lineContaining(t, string(out), `"queueUrl":`)
	if !strings.Contains(line, `.get("resource", {}).get("status", {}).get("atProvider", {}).get("url")`) {
		t.Errorf("queueUrl wire = %s", line)
	}
}

func TestTranslateForEachToPython_StatusBoundReadsPathOnce(t *testing.T) {
	got := translateForEachToPython("resources.main-queue.status.atProvider.nodeCount")
	want := `range(int(ocds.get("main-queue", {}).get("resource", {}).get("status", {}).get("atProvider", {}).get("nodeCount", 0)))`
	if got != want {
		t.Errorf("translateForEachToPython = %q, want %q", got, want)
	}
}

func TestPythonOptionalParamIsDroppedWhenAbsent(t *testing.T) {
	b := wireBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "params.maxMessageSize"}
	out, err := Composition(b, wireCRDs(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if n := strings.Count(s, "_present = lambda d: {k: v for k, v in d.items() if v is not None}"); n != 1 {
		t.Errorf("_present helper defined %d times, want exactly 1:\n%s", n, s)
	}
	if !strings.Contains(s, `"forProvider": _present({`) {
		t.Errorf("forProvider dict is not passed through _present:\n%s", s)
	}
	if !strings.Contains(s, `"maxMessageSize": spec.get("maxMessageSize"),`) {
		t.Errorf("optional param must read without a default:\n%s", s)
	}
}

func TestPythonEnvelopeNesting(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-env
spec:
  emit:
    engine: python
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
	if strings.Contains(s, `"writeConnectionSecretToRef.name":`) {
		t.Errorf("flattened dot path found in Python output:\n%s", s)
	}
	if !strings.Contains(s, `"writeConnectionSecretToRef": _present({`) {
		t.Errorf("expected nested writeConnectionSecretToRef object in Python:\n%s", s)
	}
	if !strings.Contains(s, `"name": spec.get("secretName")`) {
		t.Errorf("expected name child in Python:\n%s", s)
	}
	if !strings.Contains(s, `"namespace": "crossplane-system"`) {
		t.Errorf("expected namespace child in Python:\n%s", s)
	}
}

func TestPythonRefusesTemplateConventions(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-conv
spec:
  emit:
    engine: python
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
		t.Fatal("expected error on Python with conventions, got nil")
	}
	if !strings.Contains(err.Error(), "conventions") {
		t.Errorf("expected error to mention conventions, got: %v", err)
	}
}
