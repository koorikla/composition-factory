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

func TestEmptyNestedMappingsSuppressedAcrossEngines(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xbucket
spec:
  xrd:
    group: storage.example.org
    version: v1alpha1
    kind: XBucket
    plural: xbuckets
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      sseAlgorithm:
        type: string
        required: false
  resources:
    - name: bucket
      kind: Bucket
      fields:
        serverSideEncryptionConfiguration.rule.sseAlgorithm:
          from: params.sseAlgorithm
`
	dir := t.TempDir()
	p := filepath.Join(dir, "xbucket.cf.yaml")
	if err := os.WriteFile(p, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: buckets.s3.aws.upbound.io}
spec:
  group: s3.aws.upbound.io
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
                  serverSideEncryptionConfiguration:
                    type: object
                    required: [rule]
                    properties:
                      rule:
                        type: object
                        required: [sseAlgorithm]
                        properties:
                          sseAlgorithm: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Go-template", func(t *testing.T) {
		out, err := Composition(b, crds)
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, `{{- if or (hasKey $spec "sseAlgorithm") }}`) {
			t.Errorf("expected guard in Go-template output, got:\n%s", s)
		}
		if !strings.Contains(s, "{{- else }}") || !strings.Contains(s, "{}") {
			t.Errorf("expected empty dict fallback in Go-template output, got:\n%s", s)
		}
	})

	t.Run("Python", func(t *testing.T) {
		bPython := *b
		bPython.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
		out, err := Composition(&bPython, crds)
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		s := string(out)

		// Nested branch nodes in Python must collapse empty child dicts to None with "}) or None," (CF-416)
		wantNestedOrNone := `"serverSideEncryptionConfiguration": _present({`
		if !strings.Contains(s, wantNestedOrNone) {
			t.Errorf("expected serverSideEncryptionConfiguration in Python output, got:\n%s", s)
		}
		if !strings.Contains(s, "}) or None,") {
			t.Errorf("expected nested branch node to append '}) or None,' so empty dicts collapse to None, got:\n%s", s)
		}

		// Verify runtime behavior: when optional params are absent, forProvider evaluates to {}
		if _, err := exec.LookPath("python3"); err == nil {
			pyBody, err := pythonTemplateBody(&bPython, crds)
			if err != nil {
				t.Fatalf("pythonTemplateBody: %v", err)
			}
			pyRunner := `
import sys, types, json

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

class MockRes:
    def __init__(self):
        self.resource = {}
    def update(self, d):
        self.resource.update(d)

class MockRsp:
    def __init__(self):
        self.desired = types.SimpleNamespace(resources={"bucket": MockRes()})

# Case 1: sseAlgorithm omitted
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp1 = MockRsp()
compose(req1, rsp1)
fp1 = rsp1.desired.resources["bucket"].resource.get("spec", {}).get("forProvider")
if fp1 != {}:
    print(f"FAIL Case 1: expected forProvider == {{}}, got: {json.dumps(fp1)}")
    sys.exit(1)

# Case 2: sseAlgorithm provided
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {"sseAlgorithm": "AES256"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp2 = MockRsp()
compose(req2, rsp2)
fp2 = rsp2.desired.resources["bucket"].resource.get("spec", {}).get("forProvider")
expected2 = {"serverSideEncryptionConfiguration": {"rule": {"sseAlgorithm": "AES256"}}}
if fp2 != expected2:
    print(f"FAIL Case 2: expected {json.dumps(expected2)}, got: {json.dumps(fp2)}")
    sys.exit(2)
`
			cmd := exec.Command("python3", "-c", pyRunner)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("python test runner failed: %v\nOutput: %s", err, string(out))
			}
		}
	})

	t.Run("KCL", func(t *testing.T) {
		bKCL := *b
		bKCL.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
		out, err := Composition(&bKCL, crds)
		if err != nil {
			t.Fatalf("Composition: %v", err)
		}
		s := string(out)

		wantGuard := "if _spec?.sseAlgorithm != None:"
		if !strings.Contains(s, wantGuard) {
			t.Errorf("expected conditional guard %q for nested optional mapping in KCL, got:\n%s", wantGuard, s)
		}

		// Ensure unconditional nested mapping is NOT emitted outside the guard
		unconditionalBranch := "\n            serverSideEncryptionConfiguration = {\n"
		if strings.Contains(s, unconditionalBranch) {
			t.Errorf("found unconditional nested branch mapping in KCL output:\n%s", s)
		}
	})
}

func TestKCLMultiLeafNestedObjectDisjunctionGuard(t *testing.T) {
	bpYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xbucket-multi
spec:
  emit:
    engine: kcl
  xrd:
    group: storage.example.org
    version: v1alpha1
    kind: XBucketMulti
    plural: xbucketmultis
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
      sseAlgorithm:
        type: string
        required: false
      bucketKeyEnabled:
        type: boolean
        required: false
  resources:
    - name: bucket
      kind: Bucket
      fields:
        serverSideEncryptionConfiguration.rule.sseAlgorithm:
          from: params.sseAlgorithm
        serverSideEncryptionConfiguration.rule.bucketKeyEnabled:
          from: params.bucketKeyEnabled
`
	dir := t.TempDir()
	p := filepath.Join(dir, "xbucket-multi.cf.yaml")
	if err := os.WriteFile(p, []byte(bpYAML), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := blueprint.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: buckets.s3.aws.upbound.io}
spec:
  group: s3.aws.upbound.io
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
                  serverSideEncryptionConfiguration:
                    type: object
                    properties:
                      rule:
                        type: object
                        properties:
                          sseAlgorithm: {type: string}
                          bucketKeyEnabled: {type: boolean}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
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

	// Parent branch must be guarded with disjunction
	wantDisjunction := "if _spec?.bucketKeyEnabled != None or _spec?.sseAlgorithm != None:"
	if !strings.Contains(s, wantDisjunction) {
		t.Errorf("expected disjunction guard %q for serverSideEncryptionConfiguration in KCL output, got:\n%s", wantDisjunction, s)
	}

	// Children must have their own individual guards
	if !strings.Contains(s, "if _spec?.sseAlgorithm != None:") || !strings.Contains(s, "sseAlgorithm = _spec?.sseAlgorithm") {
		t.Errorf("expected child guard for sseAlgorithm in KCL output, got:\n%s", s)
	}
	if !strings.Contains(s, "if _spec?.bucketKeyEnabled != None:") || !strings.Contains(s, "bucketKeyEnabled = _spec?.bucketKeyEnabled") {
		t.Errorf("expected child guard for bucketKeyEnabled in KCL output, got:\n%s", s)
	}
}
