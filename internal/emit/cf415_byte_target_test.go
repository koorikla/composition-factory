package emit

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func byteContainerCRD(t *testing.T) []schema.CRD {
	t.Helper()
	crdDoc := []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: bytecontainers.example.org}
spec:
  group: example.org
  scope: Namespaced
  names: {kind: ByteContainer, plural: bytecontainers, categories: [managed]}
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            required: [forProvider]
            properties:
              forProvider:
                required: [payload]
                properties:
                  payload: {type: string, format: byte}
                  normalStr: {type: string}
              providerConfigRef:
                type: object
                required: [kind, name]
                properties: {kind: {type: string}, name: {type: string}}
          status:
            properties:
              atProvider:
                properties:
                  token: {type: string}
                  cert: {type: string, format: byte}
`)
	crds, err := schema.ParseCRDs([][]byte{crdDoc})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	return append(nativeTestCRDs(t), crds...)
}

func TestCF415_SecretDataB64KCLAndPython(t *testing.T) {
	// Repro from CF-415 issue:
	// Secret data[password] from params.password
	// Secret stringData[host] from params.password
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsec"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSec", Plural: "xsecs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"password": {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "db-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[password]":   {From: "params.password"},
						"stringData[host]": {From: "params.password"},
					},
				},
			},
		},
	}

	crds := nativeTestCRDs(t)

	// 1. KCL Engine
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	compKCL, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition(kcl): %v", err)
	}
	kclStr := string(compKCL)

	if !strings.Contains(kclStr, "import base64") {
		t.Errorf("KCL missing 'import base64':\n%s", kclStr)
	}
	if !strings.Contains(kclStr, "password = base64.encode(_spec?.password)") {
		t.Errorf("KCL missing base64.encode for Secret data wire:\n%s", kclStr)
	}
	if !strings.Contains(kclStr, "host = _spec?.password") {
		t.Errorf("KCL stringData should remain unencoded plain string:\n%s", kclStr)
	}

	// 2. Python Engine
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	compPy, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition(python): %v", err)
	}
	pyStr := string(compPy)

	if !strings.Contains(pyStr, "import base64") {
		t.Errorf("Python missing 'import base64':\n%s", pyStr)
	}
	if !strings.Contains(pyStr, "_b64 = lambda") {
		t.Errorf("Python missing '_b64 = lambda' helper:\n%s", pyStr)
	}
	if !strings.Contains(pyStr, `"password": _b64(spec.get("password"))`) {
		t.Errorf("Python missing _b64 for Secret data wire:\n%s", pyStr)
	}
	if !strings.Contains(pyStr, `"host": spec.get("password")`) {
		t.Errorf("Python stringData should remain unencoded plain string:\n%s", pyStr)
	}
}

func TestCF415_FormatByteCRDTargetKCLAndPython(t *testing.T) {
	crds := byteContainerCRD(t)

	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xbyte"},
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{{Provider: "example.org"}},
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XByte", Plural: "xbytes",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
					"rawPayload":   {Type: "string", Required: true},
					"text":         {Type: "string", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "src-container", Kind: "ByteContainer", Provider: "example.org",
					Fields: map[string]blueprint.Field{
						"payload":   {From: "params.rawPayload"},
						"normalStr": {From: "params.text"},
					},
				},
				{
					Name: "target-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[fromStatus]": {From: "resources.src-container.status.atProvider.token"},
						"data[fromCert]":   {From: "resources.src-container.status.atProvider.cert"},
					},
				},
			},
		},
	}

	// 1. KCL Engine
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EngineKCL}
	compKCL, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition(kcl): %v", err)
	}
	kclStr := string(compKCL)

	if !strings.Contains(kclStr, "payload = base64.encode(_spec?.rawPayload)") {
		t.Errorf("KCL missing base64.encode for format: byte CRD field:\n%s", kclStr)
	}
	if !strings.Contains(kclStr, "normalStr = _spec?.text") {
		t.Errorf("KCL normalStr should not be base64-encoded:\n%s", kclStr)
	}
	if !strings.Contains(kclStr, "fromStatus = base64.encode(ocds?[\"src-container\"]?.Resource?.status?.atProvider?.token)") {
		t.Errorf("KCL missing base64.encode for Secret data status wire:\n%s", kclStr)
	}
	if !strings.Contains(kclStr, "fromCert = base64.encode(ocds?[\"src-container\"]?.Resource?.status?.atProvider?.cert)") {
		t.Errorf("KCL missing base64.encode for Secret data status wire from byte cert:\n%s", kclStr)
	}

	// 2. Python Engine
	b.Spec.Emit = &blueprint.Emit{Engine: blueprint.EnginePython}
	compPy, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition(python): %v", err)
	}
	pyStr := string(compPy)

	if !strings.Contains(pyStr, `"payload": _b64(spec.get("rawPayload"))`) {
		t.Errorf("Python missing _b64 for format: byte CRD field:\n%s", pyStr)
	}
	if !strings.Contains(pyStr, `"normalStr": spec.get("text")`) {
		t.Errorf("Python normalStr should not be base64-encoded:\n%s", pyStr)
	}
	if !strings.Contains(pyStr, `"fromStatus": _b64(_get(ocds, "src-container", "resource", "status", "atProvider", "token"))`) {
		t.Errorf("Python missing _b64 for Secret data status wire:\n%s", pyStr)
	}
}

func TestCF415_PythonEnvWireBase64(t *testing.T) {
	crds := nativeTestCRDs(t)

	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsecenv"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSecEnv", Plural: "xsecenvs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {Type: "string", Required: true},
				},
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"apiToken": {Type: "string"},
			},
			Resources: []blueprint.Resource{
				{
					Name: "env-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[apiKey]":     {From: "env.apiToken"},
						"stringData[desc]": {From: "env.apiToken"},
					},
				},
			},
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
		},
	}

	compPy, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition(python): %v", err)
	}
	pyStr := string(compPy)

	if !strings.Contains(pyStr, `"apiKey": _b64(env.get("apiToken"))`) {
		t.Errorf("Python missing _b64 for Secret data env wire:\n%s", pyStr)
	}
	if !strings.Contains(pyStr, `"desc": env.get("apiToken")`) {
		t.Errorf("Python stringData env wire should remain plain:\n%s", pyStr)
	}
}

func TestCF415_StructuredRHS_IsByte(t *testing.T) {
	crds := byteContainerCRD(t)
	secRes := blueprint.Resource{
		Name: "db-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
	}
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsec"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSec", Plural: "xsecs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"password": {Type: "string", Required: true},
				},
			},
		},
	}

	// Secret data[password] -> isByte: true
	sData, _, _, err := resolveFieldRHS("data[password]", blueprint.Field{From: "params.password"}, secRes, b, crds, true, nil, true)
	if err != nil {
		t.Fatalf("resolveFieldRHS(data[password]): %v", err)
	}
	if !sData.isByte {
		t.Errorf("expected isByte: true for Secret data[password], got false")
	}

	// Secret stringData[host] -> isByte: false
	sStrData, _, _, err := resolveFieldRHS("stringData[host]", blueprint.Field{From: "params.password"}, secRes, b, crds, true, nil, true)
	if err != nil {
		t.Fatalf("resolveFieldRHS(stringData[host]): %v", err)
	}
	if sStrData.isByte {
		t.Errorf("expected isByte: false for Secret stringData[host], got true")
	}

	// ByteContainer payload (format: byte) -> isByte: true
	byteNode := &schema.Node{Type: "string", Format: "byte"}
	bcRes := blueprint.Resource{Name: "bc", Kind: "ByteContainer", Provider: "example.org"}
	sByte, _, _, err := resolveFieldRHS("payload", blueprint.Field{From: "params.password"}, bcRes, b, crds, true, byteNode, false)
	if err != nil {
		t.Fatalf("resolveFieldRHS(payload): %v", err)
	}
	if !sByte.isByte {
		t.Errorf("expected isByte: true for CRD field with format: byte, got false")
	}

	// ByteContainer normalStr (format: "") -> isByte: false
	strNode := &schema.Node{Type: "string"}
	sNormal, _, _, err := resolveFieldRHS("normalStr", blueprint.Field{From: "params.password"}, bcRes, b, crds, true, strNode, false)
	if err != nil {
		t.Fatalf("resolveFieldRHS(normalStr): %v", err)
	}
	if sNormal.isByte {
		t.Errorf("expected isByte: false for normal string CRD field, got true")
	}
}

func TestCF415_PythonRuntimeExecution(t *testing.T) {
	pyBin, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	crds := nativeTestCRDs(t)
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsec"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSec", Plural: "xsecs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"password": {Type: "string"},
					"host":     {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "db-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[password]":   {From: "params.password"},
						"stringData[host]": {From: "params.host"},
					},
				},
			},
			Emit: &blueprint.Emit{Engine: blueprint.EnginePython},
		},
	}

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
        self.desired = types.SimpleNamespace(resources={"db-secret": MockRes()})

# Test case 1: password provided -> base64 encoded, host -> plaintext
req1 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {"password": "supersecret", "host": "db.example.com"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp1 = MockRsp()
compose(req1, rsp1)
res1 = rsp1.desired.resources["db-secret"].resource
data1 = res1.get("data", {})
strData1 = res1.get("stringData", {})

if data1.get("password") != "c3VwZXJzZWNyZXQ=":
    print(f"FAIL: expected base64 password 'c3VwZXJzZWNyZXQ=', got: {data1.get('password')}")
    sys.exit(1)

if strData1.get("host") != "db.example.com":
    print(f"FAIL: expected plaintext host 'db.example.com', got: {strData1.get('host')}")
    sys.exit(2)

# Test case 2: password omitted -> password omitted from data
req2 = types.SimpleNamespace(
    observed=types.SimpleNamespace(composite=types.SimpleNamespace(resource={"spec": {"host": "db.example.com"}}), resources={}),
    desired=types.SimpleNamespace(composite=types.SimpleNamespace(resource={}), resources={}),
    context={}
)
rsp2 = MockRsp()
compose(req2, rsp2)
res2 = rsp2.desired.resources["db-secret"].resource
data2 = res2.get("data", {})
if "password" in data2:
    print(f"FAIL: expected password to be absent from data, got: {data2}")
    sys.exit(3)

print("OK")
`
	cmd := exec.Command(pyBin, "-c", pyRunner)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python execution failed: %v\nOutput:\n%s", err, out)
	}
}

func TestCF415_KCLRuntimeExecution(t *testing.T) {
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker not available")
	}

	crds := nativeTestCRDs(t)
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsec"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSec", Plural: "xsecs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"password": {Type: "string"},
					"host":     {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "db-secret", Kind: "Secret", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"data[password]":   {From: "params.password"},
						"stringData[host]": {From: "params.host"},
					},
				},
			},
			Emit: &blueprint.Emit{Engine: blueprint.EngineKCL},
		},
	}

	kclBody, err := kclTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("kclTemplateBody: %v", err)
	}

	cmd := exec.Command(dockerBin, "run", "-i", "--rm", "kcllang/kcl:v0.11.0", "kcl", "run",
		"-D", `params={"oxr": {"metadata": {"name": "test-sec"}, "spec": {"password": "supersecret", "host": "db.example.com"}}}`, "-")
	cmd.Stdin = strings.NewReader(kclBody)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kcl docker execution failed: %v\nOutput:\n%s", err, out)
	}

	outStr := string(out)
	if !strings.Contains(outStr, "password: c3VwZXJzZWNyZXQ=") {
		t.Errorf("expected base64 encoded password in KCL output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "host: db.example.com") {
		t.Errorf("expected plaintext host in KCL output, got:\n%s", outStr)
	}
}
