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

// Defaulted XRD parameters must be populated in SampleXR, so unguarded
// references (such as when conditions) do not hard-fail under missingkey=error,
// while optional parameters without defaults remain omitted.
func TestSampleXRWithDefaultedParameters(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "defaulted-params-test"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "test.example.org",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Kind:    "TestXR",
				Plural:  "testxrs",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
					"tier": {
						Type:    "string",
						Default: "standard",
					},
					"comment": {
						Type: "string",
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name: "queue",
					Kind: "Queue",
					When: `params.tier == "pro"`,
					Fields: map[string]blueprint.Field{
						"region": {Value: "eu-north-1"},
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
		"providerName": "sample",
		"tier":         "standard",
	}
	if diff := cmp.Diff(wantSpec, xr.Spec); diff != "" {
		t.Errorf("spec (-want +got):\n%s", diff)
	}

	rawComp, err := Composition(b, testCRDs(t))
	if err != nil {
		t.Fatalf("Composition: %v", err)
	}
	tmplBody := extractTemplate(t, rawComp)
	if _, err := renderTemplate(t, tmplBody, xr.Spec); err != nil {
		t.Fatalf("render under missingkey=error failed: %v", err)
	}
}

// For object parameters:
//   - If an optional object parameter has a property that is required or has a default,
//     the object is populated in SampleXR with those properties.
//   - If an optional object parameter has neither required nor defaulted properties,
//     it is omitted from SampleXR.
func TestSampleXRWithObjectParameters(t *testing.T) {
	b := &blueprint.Blueprint{
		APIVersion: "factory.crossplane.io/v1alpha1",
		Kind:       "Blueprint",
		Metadata:   blueprint.Metadata{Name: "object-params-test"},
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "test.example.org",
				Version: "v1alpha1",
				Scope:   "Namespaced",
				Kind:    "TestXR",
				Plural:  "testxrs",
				Parameters: map[string]blueprint.Parameter{
					"providerName": {
						Type:     "string",
						Required: true,
					},
					"tuning": {
						Type: "object",
						Properties: map[string]blueprint.Parameter{
							"maxSize": {Type: "integer", Default: "2048"},
							"timeout": {Type: "integer"},
						},
					},
					"unspecified": {
						Type: "object",
						Properties: map[string]blueprint.Parameter{
							"extra": {Type: "string"},
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
		"providerName": "sample",
		"tuning": map[string]any{
			"maxSize": float64(2048),
		},
	}
	if diff := cmp.Diff(wantSpec, xr.Spec); diff != "" {
		t.Errorf("spec (-want +got):\n%s", diff)
	}
}

// CF-442: previewPlaceholderValue populates all declared object properties,
// whereas placeholderValue retains minimal properties for SampleXR.
func TestPreviewPlaceholderValueWithObjectParameters(t *testing.T) {
	param := blueprint.Parameter{
		Type: "object",
		Properties: map[string]blueprint.Parameter{
			"maxSize": {Type: "integer", Default: "2048"},
			"timeout": {Type: "integer"},
			"enabled": {Type: "boolean"},
			"tier":    {Type: "string", Enum: []string{"standard", "premium"}},
			"nested": {
				Type: "object",
				Properties: map[string]blueprint.Parameter{
					"subKey": {Type: "string"},
				},
			},
		},
	}

	minimal := placeholderValue(param)
	wantMinimal := map[string]any{
		"maxSize": 2048,
	}
	if diff := cmp.Diff(wantMinimal, minimal); diff != "" {
		t.Errorf("placeholderValue (-want +got):\n%s", diff)
	}

	preview := previewPlaceholderValue(param)
	wantPreview := map[string]any{
		"maxSize": 2048,
		"timeout": 1,
		"enabled": true,
		"tier":    "standard",
		"nested": map[string]any{
			"subKey": "sample",
		},
	}
	if diff := cmp.Diff(wantPreview, preview); diff != "" {
		t.Errorf("previewPlaceholderValue (-want +got):\n%s", diff)
	}
}
