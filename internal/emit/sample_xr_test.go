package emit

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

// When parameters have enums, SampleXR must emit values matching the parameter's
// declared scalar type (int, float64, bool) rather than raw string values.
func TestSampleXRWithScalarEnums(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "enum-test"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "test.example.org",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Kind:    "TestXR",
				Plural:  "testxrs",
				Parameters: map[string]blueprint.Parameter{
					"port": {
						Type:     "integer",
						Required: true,
						Enum:     []string{"80", "443"},
					},
					"ratio": {
						Type:     "number",
						Required: true,
						Enum:     []string{"0.5", "1.0"},
					},
					"enabled": {
						Type:     "boolean",
						Required: true,
						Enum:     []string{"true", "false"},
					},
					"tier": {
						Type:     "string",
						Required: true,
						Enum:     []string{"standard", "pro"},
					},
				},
			},
		},
	}

	raw, err := SampleXR(b)
	if err != nil {
		t.Fatalf("SampleXR: %v", err)
	}

	var xr struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Metadata   map[string]any `json:"metadata"`
		Spec       map[string]any `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &xr); err != nil {
		t.Fatalf("SampleXR output is not YAML: %v\n%s", err, raw)
	}

	wantSpec := map[string]any{
		"port":    float64(80),
		"ratio":   float64(0.5),
		"enabled": true,
		"tier":    "standard",
	}
	if diff := cmp.Diff(wantSpec, xr.Spec); diff != "" {
		t.Errorf("spec (-want +got):\n%s", diff)
	}
}

// Object member parameters with scalar enums must also be synthesized with
// their declared types.
func TestSampleXRObjectMemberWithScalarEnums(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "enum-obj-test"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "test.example.org",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Kind:    "TestXR",
				Plural:  "testxrs",
				Parameters: map[string]blueprint.Parameter{
					"config": {
						Type:     "object",
						Required: true,
						Properties: map[string]blueprint.Parameter{
							"port": {
								Type:     "integer",
								Required: true,
								Enum:     []string{"80", "443"},
							},
							"ratio": {
								Type:     "number",
								Required: true,
								Enum:     []string{"0.5", "1.0"},
							},
							"enabled": {
								Type:     "boolean",
								Required: true,
								Enum:     []string{"true", "false"},
							},
							"tier": {
								Type:     "string",
								Required: true,
								Enum:     []string{"standard", "pro"},
							},
						},
					},
				},
			},
		},
	}

	raw, err := SampleXR(b)
	if err != nil {
		t.Fatalf("SampleXR: %v", err)
	}

	var xr struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Metadata   map[string]any `json:"metadata"`
		Spec       map[string]any `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &xr); err != nil {
		t.Fatalf("SampleXR output is not YAML: %v\n%s", err, raw)
	}

	wantSpec := map[string]any{
		"config": map[string]any{
			"port":    float64(80),
			"ratio":   float64(0.5),
			"enabled": true,
			"tier":    "standard",
		},
	}
	if diff := cmp.Diff(wantSpec, xr.Spec); diff != "" {
		t.Errorf("spec (-want +got):\n%s", diff)
	}
}
