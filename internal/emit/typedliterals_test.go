package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func typedLiteralsCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: databases.rds.aws.m.upbound.io}
spec:
  group: rds.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Instance, plural: instances, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  engineVersion: {type: string}
                  allocatedStorage: {type: integer}
                  performanceScore: {type: number}
                  enableDnsHostnames: {type: boolean}
                  securityGroupIds:
                    type: array
                    items: {type: string}
                  tags:
                    type: object
                    additionalProperties: {type: string}
                  monitoringConfig:
                    type: object
                    properties:
                      enabled: {type: boolean}
                  maintenanceWindows:
                    type: array
                    items:
                      type: object
                      properties:
                        day: {type: string}
`)}
	crds, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatal(err)
	}
	return crds
}

func typedLiteralsBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xrds"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.example.com", Kind: "XRDS", Plural: "xrdss",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"providerName":  {Type: "string", Required: true},
					"engineVersion": {Type: "string", Required: true},
					"storage":       {Type: "integer", Required: true},
					"enabled":       {Type: "boolean", Required: true},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "db",
					Kind: "Instance",
					Fields: map[string]blueprint.Field{
						"region":             {Value: "eu-north-1"},
						"enableDnsHostnames": {Value: "true"},
						"allocatedStorage":   {Value: "20"},
						"securityGroupIds":   {Value: "sg-123, sg-456"},
						"engineVersion":      {From: "params.engineVersion"},
					},
				},
			},
		},
	}
}

func TestTypedLiteralsEmission(t *testing.T) {
	b := typedLiteralsBlueprint()
	crds := typedLiteralsCRDs(t)

	got, err := Composition(b, crds)
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	s := string(got)

	// Boolean literal must be unquoted true
	if !strings.Contains(s, "enableDnsHostnames: true") {
		t.Errorf("enableDnsHostnames should be unquoted true, got:\n%s", s)
	}
	// Integer literal must be unquoted 20
	if !strings.Contains(s, "allocatedStorage: 20") {
		t.Errorf("allocatedStorage should be unquoted 20, got:\n%s", s)
	}
	// String literal must be quoted 'eu-north-1'
	if !strings.Contains(s, "region: 'eu-north-1'") {
		t.Errorf("region should be quoted 'eu-north-1', got:\n%s", s)
	}
	// String array literal must be ['sg-123', 'sg-456']
	if !strings.Contains(s, "securityGroupIds: ['sg-123', 'sg-456']") {
		t.Errorf("securityGroupIds should be ['sg-123', 'sg-456'], got:\n%s", s)
	}
	// String param must have | quote so strings like '16.3' or '0x1F' stay strings
	if !strings.Contains(s, "engineVersion: {{ $spec.engineVersion | quote }}") {
		t.Errorf("engineVersion string parameter wire should be quoted with | quote, got:\n%s", s)
	}
}

func TestInvalidBooleanLiteralRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["enableDnsHostnames"] = blueprint.Field{Value: "notabool"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "not a valid boolean") {
		t.Fatalf("expected error complaining about not a valid boolean, got: %v", err)
	}
}

func TestInvalidIntegerLiteralRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["allocatedStorage"] = blueprint.Field{Value: "twenty"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "not a valid integer") {
		t.Fatalf("expected error complaining about not a valid integer, got: %v", err)
	}
}

func TestInvalidNumberLiteralRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["performanceScore"] = blueprint.Field{Value: "NaN"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "not a valid number") {
		t.Fatalf("expected error complaining about not a valid number, got: %v", err)
	}
}

func TestValueOnObjectCompositeRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["monitoringConfig"] = blueprint.Field{Value: "enabled"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "cannot render a composite") {
		t.Fatalf("expected error complaining about setting an object with value, got: %v", err)
	}
}

func TestValueOnArrayOfObjectsRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["maintenanceWindows"] = blueprint.Field{Value: "monday, tuesday"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "array of objects") {
		t.Fatalf("expected error complaining about array of objects, got: %v", err)
	}
}

func TestScalarFromWireIntoArrayLeafRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["securityGroupIds"] = blueprint.Field{From: "params.engineVersion"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "is an array, and a from: wire cannot render a list") {
		t.Fatalf("expected error complaining about wiring scalar into array, got: %v", err)
	}
}

func TestScalarFromWireIntoObjectRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	b.Spec.Resources[0].Fields["monitoringConfig"] = blueprint.Field{From: "params.engineVersion"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "is an object; a from: wire renders one scalar and cannot fill it") {
		t.Fatalf("expected error complaining about wiring scalar into object, got: %v", err)
	}
}

func TestParamTypeMismatchRefused(t *testing.T) {
	b := typedLiteralsBlueprint()
	// Trying to wire string parameter into boolean field
	b.Spec.Resources[0].Fields["enableDnsHostnames"] = blueprint.Field{From: "params.engineVersion"}
	crds := typedLiteralsCRDs(t)

	_, err := Composition(b, crds)
	if err == nil || !strings.Contains(err.Error(), "the wire would render a YAML scalar of the wrong type") {
		t.Fatalf("expected error complaining about wrong type, got: %v", err)
	}
}

func TestTypedLiteralsKCLAndPythonParity(t *testing.T) {
	b := typedLiteralsBlueprint()
	crds := typedLiteralsCRDs(t)

	kclCode, err := kclTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("KCL: %v", err)
	}
	if !strings.Contains(kclCode, "enableDnsHostnames = True") {
		t.Errorf("KCL missing enableDnsHostnames = True:\n%s", kclCode)
	}
	if !strings.Contains(kclCode, "allocatedStorage = 20") {
		t.Errorf("KCL missing allocatedStorage = 20:\n%s", kclCode)
	}
	if !strings.Contains(kclCode, "region = \"eu-north-1\"") {
		t.Errorf("KCL missing quoted region:\n%s", kclCode)
	}
	if !strings.Contains(kclCode, "securityGroupIds = [\"sg-123\", \"sg-456\"]") {
		t.Errorf("KCL missing formatted list for securityGroupIds:\n%s", kclCode)
	}

	pythonCode, err := pythonTemplateBody(b, crds)
	if err != nil {
		t.Fatalf("Python: %v", err)
	}
	if !strings.Contains(pythonCode, "\"enableDnsHostnames\": True") {
		t.Errorf("Python missing enableDnsHostnames: True:\n%s", pythonCode)
	}
	if !strings.Contains(pythonCode, "\"allocatedStorage\": 20") {
		t.Errorf("Python missing allocatedStorage: 20:\n%s", pythonCode)
	}
	if !strings.Contains(pythonCode, "\"region\": \"eu-north-1\"") {
		t.Errorf("Python missing quoted region:\n%s", pythonCode)
	}
	if !strings.Contains(pythonCode, "\"securityGroupIds\": [\"sg-123\", \"sg-456\"]") {
		t.Errorf("Python missing formatted list for securityGroupIds:\n%s", pythonCode)
	}
}
