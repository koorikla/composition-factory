package emit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"sigs.k8s.io/yaml"
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
	want := `_get(ocds, "role", "resource", "status", "atProvider", "arn")`
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
	if !strings.Contains(line, `_get(ocds, "main-queue", "resource", "status", "atProvider", "url")`) {
		t.Errorf("queueUrl wire = %s", line)
	}
}

func TestTranslateForEachToPython_StatusBoundReadsPathOnce(t *testing.T) {
	got := translateForEachToPython("resources.main-queue.status.atProvider.nodeCount")
	want := `range(int(_get(ocds, "main-queue", "resource", "status", "atProvider", "nodeCount", default=0)))`
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
	var b blueprint.Blueprint
	if err := yaml.Unmarshal([]byte(bpYAML), &b); err != nil {
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
	_, err := Composition(&b, crds)
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
	if !strings.Contains(body, `range(int(env.get("count", 3) or 0))`) {
		t.Errorf("expected range(int(env.get(\"count\", 3) or 0)) in python body, got:\n%s", body)
	}
	if !strings.Contains(body, `bool(env.get("enabled", True))`) {
		t.Errorf("expected bool(env.get(\"enabled\", True)) in python body, got:\n%s", body)
	}
}

func TestCF301_PythonLoopedCustomMetadataNameIncludesIndex(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-looped-name-py"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XLoopedName", Plural: "xloopednames",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"prefix":   {Type: "string"},
					"replicas": {Type: "integer", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "worker",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					ForEach:  "params.replicas",
					Fields: map[string]blueprint.Field{
						"metadata.name": {From: "params.prefix"},
					},
				},
			},
		},
	}
	out, err := Composition(bp, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"name": f"{spec.get('prefix')}-{_i}" if spec.get("prefix") else f"{xr_name}-worker-{_i}",`) {
		t.Fatalf("expected Python looped custom metadata.name to incorporate loop index _i with fallback, got:\n%s", s)
	}
}

func TestCF301_PythonLoopedCustomMetadataNameLiteralValue(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-looped-name-py-lit"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XLoopedNameLit", Plural: "xloopednamelits",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"replicas": {Type: "integer", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "worker",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					ForEach:  "params.replicas",
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "worker-custom"},
					},
				},
			},
		},
	}
	out, err := Composition(bp, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"name": f"worker-custom-{_i}",`) {
		t.Fatalf("expected Python literal name to incorporate loop index _i, got:\n%s", s)
	}
}

func TestMetadataRefCustomNameTargetPython(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"image": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name":                {Value: "custom-sa"},
						"automountServiceAccountToken": {Value: "true"},
					},
				},
				{
					Name: "app", Kind: "Deployment", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector.matchLabels":              {Raw: "{app: web}"},
						"spec.template.metadata.labels":          {Raw: "{app: web}"},
						"spec.template.spec.containers[0].name":  {Value: "app"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
						"spec.template.spec.serviceAccountName":  {From: "resources.sa.metadata.name"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if !strings.Contains(s, `"serviceAccountName": "custom-sa"`) {
		t.Fatalf("expected Python to contain custom serviceAccountName, got:\n%s", s)
	}
}

func TestMetadataRefCustomNameTargetParamPython(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xapp"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XApp", Plural: "xapps",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"image":  {Type: "string", Required: true},
					"saName": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "sa", Kind: "ServiceAccount", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name":                {From: "params.saName"},
						"automountServiceAccountToken": {Value: "true"},
					},
				},
				{
					Name: "app", Kind: "Deployment", Provider: blueprint.NativeProvider,
					Annotations: map[string]blueprint.Field{
						"app.kubernetes.io/sa-ref": {From: "resources.sa.metadata.name"},
					},
					Fields: map[string]blueprint.Field{
						"spec.selector.matchLabels":              {Raw: "{app: web}"},
						"spec.template.metadata.labels":          {Raw: "{app: web}"},
						"spec.template.spec.containers[0].name":  {Value: "app"},
						"spec.template.spec.containers[0].image": {From: "params.image"},
						"spec.template.spec.serviceAccountName":  {From: "resources.sa.metadata.name"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	comp, err := Composition(b, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if !strings.Contains(s, `"serviceAccountName": spec.get("saName")`) {
		t.Fatalf("expected Python to contain serviceAccountName spec.get(\"saName\"), got:\n%s", s)
	}
	if !strings.Contains(s, `"app.kubernetes.io/sa-ref": spec.get("saName")`) {
		t.Fatalf("expected Python to contain annotation ref, got:\n%s", s)
	}
}

func TestCF428_PythonSafeGetNested(t *testing.T) {
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
      database:
        type: object
        properties:
          name:
            type: string
  resources:
    - name: main-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: "eu-north-1"
    - name: queue-policy
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: QueuePolicy
      fields:
        region:
          value: "eu-north-1"
        queueUrl:
          from: "resources.main-queue.status.atProvider.url"
        policy:
          from: "params.database.name"
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	if err := os.WriteFile(p, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crds := wireCRDs(t)
	body, err := pythonTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

	if !strings.Contains(body, `_get = lambda d, *keys, default=None:`) {
		t.Errorf("expected _get helper definition in python body:\n%s", body)
	}
	if !strings.Contains(body, `"policy": _get(spec, "database", "name")`) {
		t.Errorf("expected safe nested param access _get(spec, ...) in python body:\n%s", body)
	}
	if !strings.Contains(body, `"queueUrl": _str(_get(ocds, "main-queue", "resource", "status", "atProvider", "url"))`) {
		t.Errorf("expected safe status wire _get(ocds, ...) in python body:\n%s", body)
	}

	pyBin, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available in PATH")
	}

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
        self.desired = types.SimpleNamespace(
            resources={
                "main-queue": MockRes(),
                "queue-policy": MockRes(),
            }
        )

# Test case 1: atProvider is None, database is None
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {"database": None}}),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": {"atProvider": None}
            }),
        },
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp1 = MockRsp()
compose(req1, rsp1)
qp1 = rsp1.desired.resources["queue-policy"].resource.get("spec", {}).get("forProvider", {})
if "queueUrl" in qp1:
    print(f"FAIL: queueUrl present when atProvider is None: {qp1}")
    sys.exit(1)
if "policy" in qp1:
    print(f"FAIL: policy present when database is None: {qp1}")
    sys.exit(1)

# Test case 2: status is None
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {"database": None}}),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": None
            }),
        },
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp2 = MockRsp()
compose(req2, rsp2)
qp2 = rsp2.desired.resources["queue-policy"].resource.get("spec", {}).get("forProvider", {})
if "queueUrl" in qp2:
    print(f"FAIL: queueUrl present when status is None: {qp2}")
    sys.exit(2)

# Test case 3: populated values work correctly
req3 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {"database": {"name": "mydb"}}}),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": {"atProvider": {"url": "https://sqs.eu-north-1.amazonaws.com/123/q"}}
            }),
        },
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp3 = MockRsp()
compose(req3, rsp3)
qp3 = rsp3.desired.resources["queue-policy"].resource.get("spec", {}).get("forProvider", {})
if qp3.get("queueUrl") != "https://sqs.eu-north-1.amazonaws.com/123/q":
    print(f"FAIL: queueUrl not populated: {qp3}")
    sys.exit(3)
if qp3.get("policy") != "mydb":
    print(f"FAIL: policy not populated: {qp3}")
    sys.exit(3)

# Test case 4: unobserved resources and omitted params
req4 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {}}),
        resources={},
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp4 = MockRsp()
compose(req4, rsp4)
qp4 = rsp4.desired.resources["queue-policy"].resource.get("spec", {}).get("forProvider", {})
if "queueUrl" in qp4:
    print(f"FAIL: queueUrl present when unobserved: {qp4}")
    sys.exit(4)
if "policy" in qp4:
    print(f"FAIL: policy present when omitted: {qp4}")
    sys.exit(4)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, out)
	}
}

func TestCF420_TranslateForEachToPython_SafeIntFallback(t *testing.T) {
	// params.
	gotParam := translateForEachToPython("params.replicas")
	wantParam := `range(int(spec.get("replicas", 0) or 0))`
	if gotParam != wantParam {
		t.Errorf("translateForEachToPython(params.replicas) = %q, want %q", gotParam, wantParam)
	}

	// env. without default
	gotEnvNoDef := translateForEachToPython("env.count")
	wantEnvNoDef := `range(int(env.get("count", 0) or 0))`
	if gotEnvNoDef != wantEnvNoDef {
		t.Errorf("translateForEachToPython(env.count no default) = %q, want %q", gotEnvNoDef, wantEnvNoDef)
	}

	// env. with default
	envWithDef := map[string]blueprint.EnvironmentKey{
		"count": {Type: "integer", Default: "3"},
	}
	gotEnvDef := translateForEachToPython("env.count", envWithDef)
	wantEnvDef := `range(int(env.get("count", 3) or 0))`
	if gotEnvDef != wantEnvDef {
		t.Errorf("translateForEachToPython(env.count with default) = %q, want %q", gotEnvDef, wantEnvDef)
	}
}

func TestCF420_PythonForEachNullSafetyRuntime(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xloop-null-test
spec:
  emit:
    engine: python
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
  xrd:
    group: aws.example.org
    version: v1alpha1
    kind: XLoop
    plural: xloops
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      replicas:
        type: integer
        default: "3"
  environment:
    extraCount:
      type: integer
      default: "2"
  resources:
    - name: main-queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      fields:
        region:
          value: "eu-north-1"
    - name: replica-param
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      forEach: params.replicas
      fields:
        region:
          value: "eu-north-1"
    - name: replica-env
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      forEach: env.extraCount
      fields:
        region:
          value: "eu-north-1"
    - name: replica-status
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0
      kind: Queue
      forEach: resources.main-queue.status.atProvider.maxMessageSize
      fields:
        region:
          value: "eu-north-1"
`
	dir := t.TempDir()
	p := filepath.Join(dir, "bp.yaml")
	if err := os.WriteFile(p, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crds := wireCRDs(t)
	body, err := pythonTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

	if !strings.Contains(body, `range(int(spec.get("replicas", 0) or 0))`) {
		t.Errorf("expected safe param loop expression in python body:\n%s", body)
	}
	if !strings.Contains(body, `range(int(env.get("extraCount", 2) or 0))`) {
		t.Errorf("expected safe env loop expression in python body:\n%s", body)
	}
	if !strings.Contains(body, `range(int(_get(ocds, "main-queue", "resource", "status", "atProvider", "maxMessageSize", default=0)))`) {
		t.Errorf("expected safe status loop expression in python body:\n%s", body)
	}

	pyBin, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available in PATH")
	}

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

class MockDict(dict):
    def __missing__(self, key):
        self[key] = types.SimpleNamespace(resource={})
        return self[key]

class MockRsp:
    def __init__(self):
        self.desired = types.SimpleNamespace(
            resources=MockDict()
        )

# Test case 1: explicitly null integer fields (repro for CF-420)
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {"replicas": None}}),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": {"atProvider": {"maxMessageSize": None}}
            }),
        },
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={"apiextensions.crossplane.io/environment": {"extraCount": None}},
)
rsp1 = MockRsp()
# Must not raise TypeError: int() argument must be a string, a bytes-like object or a real number, not 'NoneType'
compose(req1, rsp1)

# All null-bounded loops must execute 0 iterations
for name in rsp1.desired.resources.keys():
    if name.startswith("replica-param-") or name.startswith("replica-env-") or name.startswith("replica-status-"):
        print(f"FAIL: unexpected resource emitted for null loop bound: {name}")
        sys.exit(1)

# Test case 2: populated positive counts
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"spec": {"replicas": 2}}),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": {"atProvider": {"maxMessageSize": 1}}
            }),
        },
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={"apiextensions.crossplane.io/environment": {"extraCount": 3}},
)
rsp2 = MockRsp()
compose(req2, rsp2)

keys2 = set(rsp2.desired.resources.keys())
expected = {
    "main-queue",
    "replica-param-0", "replica-param-1",
    "replica-env-0", "replica-env-1", "replica-env-2",
    "replica-status-0",
}
if not expected.issubset(keys2):
    print(f"FAIL: missing expected resources in populated case: expected {expected}, got {keys2}")
    sys.exit(2)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, out)
	}
}

func TestCF430_PythonArrayEmissionSuppressesEmptyElements(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-array"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group:   "test.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"extraEnvName": {Type: "string"},
					"extraEnvVal":  {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "app",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.template.spec.containers[0].name":         {Value: "my-app"},
						"spec.template.spec.containers[0].image":        {Value: "nginx"},
						"spec.template.spec.containers[0].env[0].name":  {From: "params.extraEnvName"},
						"spec.template.spec.containers[0].env[0].value": {From: "params.extraEnvVal"},
					},
				},
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	crds := nativeTestCRDs(t)
	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(comp)
	if !strings.Contains(s, `_clean_list([`) {
		t.Errorf("expected Python script to use _clean_list for conditional array, got:\n%s", s)
	}

	pyBin, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	pyBody, err := pythonTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

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
` + pyBody + `
class MockDict(dict):
    def __missing__(self, key):
        self[key] = types.SimpleNamespace(resource={})
        return self[key]

class MockRsp:
    def __init__(self):
        self.desired = types.SimpleNamespace(resources=MockDict())

# Test 1: optional parameters omitted -> env must not be present, or must not contain empty dicts
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={"metadata": {"name": "test"}, "spec": {}}),
        resources={},
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp1 = MockRsp()
compose(req1, rsp1)
dep1 = rsp1.desired.resources["app"].resource
containers1 = dep1.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
if not containers1:
    print(f"FAIL: containers missing in app: {dep1}")
    sys.exit(1)
c1 = containers1[0]
if "env" in c1:
    print(f"FAIL: env must be omitted when all child fields are omitted, got env={c1['env']}")
    sys.exit(1)

# Test 2: optional parameters populated -> env must contain populated element
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={
            "metadata": {"name": "test"},
            "spec": {"extraEnvName": "MY_VAR", "extraEnvVal": "hello"},
        }),
        resources={},
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp2 = MockRsp()
compose(req2, rsp2)
dep2 = rsp2.desired.resources["app"].resource
containers2 = dep2.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
c2 = containers2[0]
if c2.get("env") != [{"name": "MY_VAR", "value": "hello"}]:
    print(f"FAIL: env not populated correctly: {c2.get('env')}")
    sys.exit(2)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, out)
	}
}

func TestCF427_PythonOptionalMetadataNameFallback(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-optional-meta-name-py"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group:   "platform.example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"replicas":     {Type: "integer", Required: true},
					"prefix":       {Type: "string"}, // optional
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "worker",
					Kind:     "Deployment",
					Provider: blueprint.NativeProvider,
					ForEach:  "params.replicas",
					Fields: map[string]blueprint.Field{
						"metadata.name": {From: "params.prefix"},
					},
				},
				{
					Name:     "sa",
					Kind:     "ServiceAccount",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name": {From: "params.prefix"},
					},
				},
				{
					Name: "main-queue",
					Kind: "Queue",
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-west-1"},
					},
				},
				{
					Name:     "consumer",
					Kind:     "ServiceAccount",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name": {From: "resources.main-queue.status.atProvider.url"},
					},
				},
			},
		},
	}

	crds := append(nativeTestCRDs(t), wireCRDs(t)...)
	out, err := Composition(bp, crds)
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(out)

	// Verify generated Python code contains fallbacks
	wantLooped := `"name": f"{spec.get('prefix')}-{_i}" if spec.get("prefix") else f"{xr_name}-worker-{_i}",`
	if !strings.Contains(s, wantLooped) {
		t.Errorf("expected Python looped metadata.name to have fallback, got:\n%s", s)
	}

	wantUnlooped := `"name": spec.get("prefix") or f"{xr_name}-sa",`
	if !strings.Contains(s, wantUnlooped) {
		t.Errorf("expected Python unlooped metadata.name to have fallback, got:\n%s", s)
	}

	wantStatus := `"name": _str(_get(ocds, "main-queue", "resource", "status", "atProvider", "url")) or f"{xr_name}-consumer",`
	if !strings.Contains(s, wantStatus) {
		t.Errorf("expected Python status metadata.name to have fallback, got:\n%s", s)
	}

	// Runtime verification
	pyBin, err := exec.LookPath("python3")
	if err != nil {
		return
	}

	body, err := pythonTemplateBody(bp, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

	pyRunner := `
import sys
import types
import json

m1 = types.ModuleType("google.protobuf.json_format")
m1.MessageToDict = lambda msg: msg if isinstance(msg, dict) else (getattr(msg, "__dict__", {}) if msg is not None else {})
sys.modules["google.protobuf.json_format"] = m1

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
        self.desired = types.SimpleNamespace(resources={
            "sa": MockRes(),
            "worker-0": MockRes(),
            "main-queue": MockRes(),
            "consumer": MockRes(),
        })

# Case 1: optional prefix omitted, status unobserved -> fallback to deterministic default names
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={
            "metadata": {"name": "test-xr"},
            "spec": {"providerName": "default", "replicas": 1}
        }),
        resources={}
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp1 = MockRsp()
compose(req1, rsp1)
sa1_name = rsp1.desired.resources["sa"].resource.get("metadata", {}).get("name")
if sa1_name != "test-xr-sa":
    print(f"FAIL: sa name expected 'test-xr-sa', got: {sa1_name}")
    sys.exit(1)

worker1_name = rsp1.desired.resources["worker-0"].resource.get("metadata", {}).get("name")
if worker1_name != "test-xr-worker-0":
    print(f"FAIL: worker name expected 'test-xr-worker-0', got: {worker1_name}")
    sys.exit(1)

consumer1_name = rsp1.desired.resources["consumer"].resource.get("metadata", {}).get("name")
if consumer1_name != "test-xr-consumer":
    print(f"FAIL: consumer name expected 'test-xr-consumer', got: {consumer1_name}")
    sys.exit(1)

# Case 2: prefix provided and status observed -> use custom names
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={
            "metadata": {"name": "test-xr"},
            "spec": {"providerName": "default", "replicas": 1, "prefix": "custom"}
        }),
        resources={
            "main-queue": types.SimpleNamespace(resource={
                "status": {"atProvider": {"url": "https://sqs.aws/custom-url"}}
            })
        }
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp2 = MockRsp()
compose(req2, rsp2)
sa2_name = rsp2.desired.resources["sa"].resource.get("metadata", {}).get("name")
if sa2_name != "custom":
    print(f"FAIL: sa name expected 'custom', got: {sa2_name}")
    sys.exit(2)

worker2_name = rsp2.desired.resources["worker-0"].resource.get("metadata", {}).get("name")
if worker2_name != "custom-0":
    print(f"FAIL: worker name expected 'custom-0', got: {worker2_name}")
    sys.exit(2)

consumer2_name = rsp2.desired.resources["consumer"].resource.get("metadata", {}).get("name")
if consumer2_name != "https://sqs.aws/custom-url":
    print(f"FAIL: consumer name expected 'https://sqs.aws/custom-url', got: {consumer2_name}")
    sys.exit(2)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	pyOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, pyOut)
	}
}

func TestMetadataNameByteTarget_Python(t *testing.T) {
	crds := nativeTestCRDs(t)

	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xmeta"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
			XRD: blueprint.XRD{
				Group:   "platform.sparky.ee",
				Kind:    "XMeta",
				Plural:  "xmetas",
				Version: "v1alpha1",
				Scope:   "Namespaced",
			},
			Resources: []blueprint.Resource{
				{
					Name:     "my-svc",
					Kind:     "Service",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"metadata.name": {Value: "my-static-svc"},
					},
				},
				{
					Name:     "my-secret",
					Kind:     "Secret",
					Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[svc_name]": {From: "resources.my-svc.metadata.name"},
					},
				},
			},
		},
	}

	comp, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition failed: %v", err)
	}
	s := string(comp)

	expected := `"svc_name": _b64("my-static-svc")`
	if !strings.Contains(s, expected) {
		t.Errorf("expected Python to _b64 static metadata name %q, got:\n%s", expected, s)
	}

	pyBin, err := exec.LookPath("python3")
	if err == nil {
		body, err := pythonTemplateBody(b, crds)
		if err != nil {
			t.Fatalf("pythonTemplateBody: %v", err)
		}
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
        self.desired = types.SimpleNamespace(resources={
            "my-svc": MockRes(),
            "my-secret": MockRes(),
        })

req = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"metadata": {"name": "test-xr"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp = MockRsp()
compose(req, rsp)
sec = rsp.desired.resources["my-secret"].resource
data = sec.get("data", {})
if data.get("svc_name") != "bXktc3RhdGljLXN2Yw==":
    print(f"FAIL: expected base64 static name 'bXktc3RhdGljLXN2Yw==', got: {data.get('svc_name')}")
    sys.exit(1)
print("OK")
`
		cmd := exec.Command(pyBin, "-c", pyRunner)
		pyOut, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("python execution failed: %v\nOutput:\n%s", err, pyOut)
		}
	}
}

func TestCF453_PythonEmitterWrapsNativeResourceAndMetadataInPresent(t *testing.T) {
	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-native-opt"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: "python"},
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"ns":           {Type: "string"},
					"automount":    {Type: "boolean"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "sa",
					Provider: "k8s",
					Kind:     "ServiceAccount",
					Fields: map[string]blueprint.Field{
						"metadata.namespace":           {From: "params.ns"},
						"automountServiceAccountToken": {From: "params.automount"},
					},
				},
			},
		},
	}

	out, err := Composition(bp, nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition emit failed: %v", err)
	}

	outStr := string(out)

	if !strings.Contains(outStr, "\"metadata\": _present({") {
		t.Errorf("expected native metadata to be wrapped in _present({, got:\n%s", outStr)
	}

	if !strings.Contains(outStr, "rsp.desired.resources[\"sa\"].resource.update(_present({") {
		t.Errorf("expected native resource update dictionary to be wrapped in _present({, got:\n%s", outStr)
	}
}

func TestCF453_PythonEmitterNativeOmissionRuntime(t *testing.T) {
	pyBin, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	bp := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "test-native-opt"},
		Spec: blueprint.Spec{
			Emit: &blueprint.Emit{Engine: "python"},
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XApp",
				Plural:  "xapps",
				Scope:   "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"ns":           {Type: "string"},
					"automount":    {Type: "boolean"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "sa",
					Provider: "k8s",
					Kind:     "ServiceAccount",
					Fields: map[string]blueprint.Field{
						"metadata.namespace":           {From: "params.ns"},
						"automountServiceAccountToken": {From: "params.automount"},
					},
				},
			},
		},
	}

	crds := nativeTestCRDs(t)
	body, err := pythonTemplateBody(bp, crds)
	if err != nil {
		t.Fatalf("pythonTemplateBody: %v", err)
	}

	pyRunner := `
import sys, types

m1 = types.ModuleType("google.protobuf.json_format")
m1.MessageToDict = lambda msg: msg if isinstance(msg, dict) else (getattr(msg, "__dict__", {}) if msg is not None else {})
sys.modules["google.protobuf.json_format"] = m1

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
        self.desired = types.SimpleNamespace(resources={
            "sa": MockRes(),
        })

# Case 1: ns and automount omitted -> metadata.namespace and automountServiceAccountToken stripped
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={
            "metadata": {"name": "test-xr"},
            "spec": {"providerName": "default"},
        }),
        resources={},
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp1 = MockRsp()
compose(req1, rsp1)
sa1 = rsp1.desired.resources["sa"].resource

if "automountServiceAccountToken" in sa1:
    print(f"FAIL: automountServiceAccountToken should be omitted when None, got: {sa1}")
    sys.exit(1)
if "namespace" in sa1.get("metadata", {}):
    print(f"FAIL: namespace should be omitted when None, got: {sa1.get('metadata')}")
    sys.exit(2)
if sa1.get("metadata", {}).get("name") != "test-xr-sa":
    print(f"FAIL: name missing or wrong: {sa1.get('metadata')}")
    sys.exit(3)

# Case 2: ns and automount populated (even automount=False) -> both preserved
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(
        composite=types.SimpleNamespace(resource={
            "metadata": {"name": "test-xr"},
            "spec": {"providerName": "default", "ns": "kube-system", "automount": False},
        }),
        resources={},
    ),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={},
)
rsp2 = MockRsp()
compose(req2, rsp2)
sa2 = rsp2.desired.resources["sa"].resource

if sa2.get("automountServiceAccountToken") is not False:
    print(f"FAIL: automountServiceAccountToken should be False, got: {sa2.get('automountServiceAccountToken')}")
    sys.exit(4)
if sa2.get("metadata", {}).get("namespace") != "kube-system":
    print(f"FAIL: namespace should be 'kube-system', got: {sa2.get('metadata')}")
    sys.exit(5)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	pyOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, pyOut)
	}
}
