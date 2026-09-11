package emit

import (
	"os"
	"os/exec"
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
        required: true
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
	if !strings.Contains(compStr, "from google.protobuf.json_format import MessageToDict") {
		t.Errorf("expected MessageToDict import in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, "oxr = MessageToDict(req.observed.composite.resource)") {
		t.Errorf("expected oxr MessageToDict in Python, got:\n%s", compStr)
	}
	if !strings.Contains(compStr, `ocds = {k: {"resource": MessageToDict(v.resource)} for k, v in req.observed.resources.items()}`) {
		t.Errorf("expected ocds MessageToDict in Python, got:\n%s", compStr)
	}
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
	if strings.Contains(compStr, "ready = fnv1.READY_TRUE") {
		t.Errorf("ready should not be emitted unconditionally when function-auto-ready is in the pipeline:\n%s", compStr)
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

func TestTranslateWhenToPython_StringComparisons(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   `params.version == "1.0"`,
			want: `spec.get("version") == "1.0"`,
		},
		{
			in:   `params.code == "123"`,
			want: `spec.get("code") == "123"`,
		},
		{
			in:   `params.flag == "false"`,
			want: `spec.get("flag") == "false"`,
		},
		{
			in:   `params.flag == "true"`,
			want: `spec.get("flag") == "true"`,
		},
		{
			in:   "params.enabled",
			want: `bool(spec.get("enabled"))`,
		},
	}

	for _, tc := range cases {
		got := translateWhenToPython(tc.in)
		if got != tc.want {
			t.Errorf("translateWhenToPython(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPythonCompositionWhenCondition(t *testing.T) {
	b := testBlueprint()
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	b.Spec.XRD.Parameters["version"] = blueprint.Parameter{Type: "string", Default: "1.0"}
	b.Spec.XRD.Parameters["flag"] = blueprint.Parameter{Type: "string", Default: "false"}
	b.Spec.Resources = append(b.Spec.Resources,
		blueprint.Resource{
			Name:   "queue-v1",
			Kind:   "Queue",
			When:   `params.version == "1.0"`,
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
		blueprint.Resource{
			Name:   "queue-flag",
			Kind:   "Queue",
			When:   `params.flag == "false"`,
			Fields: map[string]blueprint.Field{"region": {Value: "eu-north-1"}},
		},
	)
	out, err := Composition(b, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `if spec.get("version") == "1.0":`) {
		t.Errorf("expected python composition to contain 'if spec.get(\"version\") == \"1.0\":', got:\n%s", s)
	}
	if !strings.Contains(s, `if spec.get("flag") == "false":`) {
		t.Errorf("expected python composition to contain 'if spec.get(\"flag\") == \"false\":', got:\n%s", s)
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

func TestPythonOptionalEnvelopeMappingOmittedWhenAbsent(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-env-opt
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
      secretNamespace:
        type: string
      configRefName:
        type: string
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
          from: params.secretNamespace
        publishConnectionDetailsTo.configRef.name:
          from: params.configRefName
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
              publishConnectionDetailsTo:
                type: object
                properties:
                  configRef:
                    type: object
                    required: [name]
                    properties: {name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	body, err := pythonTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

	// In the generated Python script, the envelope structure must be guarded with "or None"
	// so empty dictionaries are stripped when all optional child parameters evaluate to None.
	if !strings.Contains(body, `"writeConnectionSecretToRef": _present({`) {
		t.Errorf("expected writeConnectionSecretToRef in Python script:\n%s", body)
	}
	if !strings.Contains(body, `"publishConnectionDetailsTo": _present({`) {
		t.Errorf("expected publishConnectionDetailsTo in Python script:\n%s", body)
	}
	if !strings.Contains(body, `}) or None,`) {
		t.Errorf("expected envelope mapping to be guarded with '}) or None,' so empty dicts are omitted:\n%s", body)
	}

	// Verify runtime behavior: when optional params are absent, writeConnectionSecretToRef
	// and nested publishConnectionDetailsTo are omitted entirely from spec. When present,
	// they are included.
	if pyBin, err := exec.LookPath("python3"); err == nil {
		pyRunner := `
import sys, types

m = types.ModuleType("google.protobuf.json_format")
m.MessageToDict = lambda x: x
sys.modules["google.protobuf.json_format"] = m
m2 = types.ModuleType("crossplane.function.proto.v1")
m2.run_function_pb2 = types.ModuleType("run_function_pb2")
m2.run_function_pb2.RunFunctionRequest = object
m2.run_function_pb2.RunFunctionResponse = object
sys.modules["crossplane.function.proto.v1"] = m2
sys.modules["crossplane.function.proto.v1.run_function_pb2"] = m2.run_function_pb2

` + body + `

class MockRes:
    def __init__(self):
        self.resource = {}
    def update(self, d):
        self.resource.update(d)

class MockRsp:
    def __init__(self):
        self.desired = types.SimpleNamespace(resources={"work-queue": MockRes()})

# Test case 1: parameters absent (oxr["spec"] is empty)
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp1 = MockRsp()
compose(req1, rsp1)
spec1 = rsp1.desired.resources["work-queue"].resource.get("spec", {})
if "writeConnectionSecretToRef" in spec1:
    print(f"FAIL: writeConnectionSecretToRef present when params absent: {spec1}")
    sys.exit(1)
if "publishConnectionDetailsTo" in spec1:
    print(f"FAIL: publishConnectionDetailsTo present when params absent: {spec1}")
    sys.exit(1)

# Test case 2: secretName present
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {"secretName": "my-secret"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp2 = MockRsp()
compose(req2, rsp2)
spec2 = rsp2.desired.resources["work-queue"].resource.get("spec", {})
if spec2.get("writeConnectionSecretToRef") != {"name": "my-secret"}:
    print(f"FAIL: writeConnectionSecretToRef not correctly set: {spec2}")
    sys.exit(2)
if "publishConnectionDetailsTo" in spec2:
    print(f"FAIL: publishConnectionDetailsTo present when configRefName absent: {spec2}")
    sys.exit(2)

# Test case 3: nested configRefName present
req3 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {"configRefName": "cfg-1"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp3 = MockRsp()
compose(req3, rsp3)
spec3 = rsp3.desired.resources["work-queue"].resource.get("spec", {})
if spec3.get("publishConnectionDetailsTo") != {"configRef": {"name": "cfg-1"}}:
    print(f"FAIL: publishConnectionDetailsTo not correctly set: {spec3}")
    sys.exit(3)
if "writeConnectionSecretToRef" in spec3:
    print(f"FAIL: writeConnectionSecretToRef present when secretName absent: {spec3}")
    sys.exit(3)

print("OK")
`
		cmd := exec.Command(pyBin, "-c", pyRunner)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("python execution failed: %v\nOutput:\n%s", err, out)
		}
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

func TestPythonForEachLoopNaming(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue-loop
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
      providerName: {type: string, required: true}
      count: {type: integer, default: "3"}
  resources:
    - name: replica-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      forEach: params.count
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
	out, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `rsp.desired.resources[f"replica-queue-{_i}"].resource.update({`) {
		t.Errorf("expected loop instance naming replica-queue-{_i}, got:\n%s", s)
	}
	if strings.Contains(s, `f"{_i}-replica-queue"`) {
		t.Errorf("found old loop instance naming {_i}-replica-queue in output:\n%s", s)
	}
}

func TestPythonRefusesGoTemplateInRaw(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xqueue"},
	}
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	b.Spec.Sources = []blueprint.Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0"}}
	b.Spec.XRD = blueprint.XRD{
		Group: "aws.example.org", Version: "v1alpha1", Kind: "XQueue", Plural: "xqueues", Scope: "Namespaced",
		Parameters: map[string]blueprint.Parameter{"providerName": {Type: "string", Required: true}},
	}
	b.Spec.Resources = []blueprint.Resource{{
		Name: "work-queue", Kind: "Queue", Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
		Fields: map[string]blueprint.Field{"region": {Raw: "{{ $spec.region }}"}},
	}}

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
	_, err := Composition(b, crds)
	if err == nil {
		t.Fatal("expected error on Python with Go-template in raw field, got nil")
	}
	if !strings.Contains(err.Error(), "Go-template syntax") || !strings.Contains(err.Error(), "go-templating engine") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPythonDottedPathNesting(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xcomplex
spec:
  emit:
    engine: python
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XComplex
    plural: xcomplexes
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  resources:
    - name: bucket-item
      provider: xpkg.upbound.io/upbound/provider-aws-s3:v1.0.0
      kind: Bucket
      fields:
        bucketRef.name:
          value: "my-bucket"
        tags[team]:
          value: "infra"
        containers[0].name:
          value: "web"
        containers[0].image:
          value: "nginx:latest"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "xcomplex.cf.yaml")
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
metadata: {name: buckets.s3.aws.m.upbound.io}
spec:
  group: s3.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Bucket, plural: buckets, categories: [managed]}
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
                properties:
                  bucketRef:
                    type: object
                    properties:
                      name: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  containers:
                    type: array
                    items:
                      type: object
                      properties:
                        name: {type: string}
                        image: {type: string}
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
	s := string(compBytes)

	// Verify nested structure and ensure no flat dotted keys exist
	if strings.Contains(s, `"bucketRef.name":`) || strings.Contains(s, `"containers[0].name":`) {
		t.Fatalf("Python output contains literal dotted keys:\n%s", s)
	}
	if !strings.Contains(s, "\"bucketRef\": _present({\n") || !strings.Contains(s, "\"name\": \"my-bucket\"") {
		t.Errorf("Python output missing nested bucketRef object:\n%s", s)
	}
	if !strings.Contains(s, "\"containers\": [\n") || !strings.Contains(s, "\"image\": \"nginx:latest\"") {
		t.Errorf("Python output missing nested containers array:\n%s", s)
	}
	if !strings.Contains(s, "\"tags\": _present({\n") || !strings.Contains(s, "\"team\": \"infra\"") {
		t.Errorf("Python output missing nested tags map:\n%s", s)
	}
}

func TestEmitPythonNativeMetadataLabels(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "native-labels-python"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group: "example.org", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"appName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.labels[app]":  {From: "params.appName"},
						"metadata.labels[tier]": {Value: "backend"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	compBytes, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(compBytes)
	if !strings.Contains(s, `"labels": _present({`) ||
		!strings.Contains(s, `"app": spec.get("appName")`) ||
		!strings.Contains(s, `"tier": "backend"`) {
		t.Errorf("Python missing metadata.labels:\n%s", s)
	}
}

func TestPythonEnvironmentDefaults(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			Sources: []blueprint.Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"},
			},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Kind:    "XApp",
				Plural:  "xapps",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"region":    {Type: "string", Default: "us-east-1"},
				"retention": {Type: "integer", Default: "345600"},
				"count":     {Type: "integer", Default: "3"},
				"enabled":   {Type: "boolean", Default: "true"},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "queue",
					Kind:     "Queue",
					Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2",
					When:     "env.enabled",
					ForEach:  "env.count",
					Fields: map[string]blueprint.Field{
						"region":                  {From: "env.region"},
						"messageRetentionSeconds": {From: "env.retention"},
					},
					Envelope: map[string]blueprint.Field{
						"providerConfigRef.name": {From: "env.region"},
					},
				},
			},
		},
	}

	body, err := pythonTemplateBody(bp, testCRDs(t))
	if err != nil {
		t.Fatalf("pythonTemplateBody failed: %v", err)
	}

	if !strings.Contains(body, `"region": env.get("region", "us-east-1")`) {
		t.Errorf("expected env.get(\"region\", \"us-east-1\") in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `"messageRetentionSeconds": env.get("retention", 345600)`) {
		t.Errorf("expected env.get(\"retention\", 345600) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `"name": env.get("region", "us-east-1")`) {
		t.Errorf("expected envelope providerConfigRef name with default in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `range(int(env.get("count", 3)))`) {
		t.Errorf("expected range(int(env.get(\"count\", 3))) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `bool(env.get("enabled", True))`) {
		t.Errorf("expected bool(env.get(\"enabled\", True)) in python body, got:\n%s", body)
	}
}
