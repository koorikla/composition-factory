package emit

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// intOrStringBlueprint drives a Service's targetPort — the canonical
// IntOrString field — from an integer parameter and from an integer literal.
func intOrStringBlueprint() *blueprint.Blueprint {
	return &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "xsvc"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group: "platform.sparky.ee", Kind: "XSvc", Plural: "xsvcs",
				Version: "v1alpha1", Scope: "Namespaced",
				Parameters: map[string]blueprint.Parameter{
					"port":     {Type: "integer"},
					"portName": {Type: "string"},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "svc", Kind: "Service", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector":            {Raw: "{app: web}"},
						"spec.ports[0].port":       {Value: "80"},
						"spec.ports[0].targetPort": {From: "params.port"},
					},
				},
				{
					Name: "svc-named", Kind: "Service", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector":            {Raw: "{app: web}"},
						"spec.ports[0].port":       {Value: "80"},
						"spec.ports[0].targetPort": {From: "params.portName"},
					},
				},
				{
					Name: "svc-literal", Kind: "Service", Provider: blueprint.NativeProvider,
					Fields: map[string]blueprint.Field{
						"spec.selector":            {Raw: "{app: web}"},
						"spec.ports[0].port":       {Value: "80"},
						"spec.ports[0].targetPort": {Value: "8080"},
					},
				},
			},
		},
	}
}

// An IntOrString field fed an integer must render an unquoted scalar. The
// vendored schema normalizes IntOrString to type string (it is the one
// spelling legal for both halves), but quoting the wire on that basis emits
// targetPort: "8080", and the API server reads a STRING targetPort as a named
// port — rejecting it with `must contain at least one letter (a-z)`.
func TestIntOrStringIntegerParameterIsNotQuoted(t *testing.T) {
	comp, err := Composition(intOrStringBlueprint(), nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmpl := extractTemplate(t, comp)

	if !strings.Contains(tmpl, "targetPort: {{ $spec.port }}") {
		t.Errorf("integer parameter into IntOrString targetPort should render unquoted, got:\n%s", tmpl)
	}
	if strings.Contains(tmpl, "targetPort: {{ $spec.port | quote }}") {
		t.Errorf("integer parameter into IntOrString targetPort must not be quoted, got:\n%s", tmpl)
	}
	// A string parameter names a port and must stay quoted.
	if !strings.Contains(tmpl, "targetPort: {{ $spec.portName | quote }}") {
		t.Errorf("string parameter into IntOrString targetPort should stay quoted, got:\n%s", tmpl)
	}
	// A numeric literal is a port number, not a port name.
	if !strings.Contains(tmpl, "targetPort: 8080") {
		t.Errorf("integer literal into IntOrString targetPort should render unquoted, got:\n%s", tmpl)
	}
}

// The rendered Service must survive a round-trip as an actual port number.
func TestIntOrStringRendersNumericTargetPort(t *testing.T) {
	comp, err := Composition(intOrStringBlueprint(), nativeTestCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	rendered, err := renderTemplate(t, extractTemplate(t, comp), map[string]any{
		"port":     8080,
		"portName": "http",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(rendered, "targetPort: 8080") {
		t.Errorf("rendered stream should carry a numeric targetPort, got:\n%s", rendered)
	}
	if strings.Contains(rendered, `targetPort: "8080"`) {
		t.Errorf("rendered stream must not carry a string targetPort, got:\n%s", rendered)
	}
}
