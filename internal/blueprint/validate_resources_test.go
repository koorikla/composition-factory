package blueprint

import (
	"strings"
	"testing"
)

func TestValidateRejectsYamlKeywordResourceName(t *testing.T) {
	keywords := []string{"true", "false", "yes", "no", "on", "off", "null", "y", "n"}
	for _, kw := range keywords {
		body := strings.Replace(valid, "name: main-queue", `name: "`+kw+`"`, 1)
		_, err := Load(write(t, body))
		if err == nil {
			t.Fatalf("expected Validate to reject resource name %q, got nil", kw)
		}
		if !strings.Contains(err.Error(), "invalid resource name") {
			t.Errorf("expected error mentioning invalid resource name for %q, got: %v", kw, err)
		}
	}
}

func TestValidateRejectsControlCharacterInResourceName(t *testing.T) {
	b, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	b.Spec.Resources[0].Name = "main\nqueue"
	err = b.Validate()
	if err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("err = %v, want the checkScalar control-character rejection", err)
	}
	if !strings.Contains(err.Error(), "spec.resources[0].name") {
		t.Errorf("err = %v, want it to name spec.resources[0].name", err)
	}
}

func TestValidateResourceFieldPaths(t *testing.T) {
	tests := []struct {
		name      string
		fieldPath string
		wantErr   string
	}{
		{
			name:      "empty field path",
			fieldPath: "",
			wantErr:   "empty field path",
		},
		{
			name:      "control character newline in field path",
			fieldPath: "spec.forProvider.name\n  injected: true",
			wantErr:   "contains the control character",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp := &Blueprint{
				APIVersion: "factory.crossplane.io/v1alpha1",
				Kind:       "Blueprint",
				Metadata:   Metadata{Name: "test-bp"},
				Spec: Spec{
					XRD: XRD{
						Group:   "example.org",
						Kind:    "XTest",
						Plural:  "xtests",
						Version: "v1alpha1",
						Scope:   "Namespaced",
						Parameters: map[string]Parameter{
							"region":       {Type: "string"},
							"providerName": {Type: "string", Required: true},
						},
					},
					Resources: []Resource{
						{
							Name: "test-res",
							Kind: "Subnet",
							Fields: map[string]Field{
								tc.fieldPath: {Value: "test"},
							},
						},
					},
				},
			}

			err := bp.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateAcceptsObjectParameterIntoNativeMapLeaves(t *testing.T) {
	// Repro from issue #343 / CF-451: untyped object parameter wired to native resource labels
	reproYAML := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-native-labels
spec:
  resources:
  - kind: ConfigMap
    name: cm
    provider: k8s
    fields:
      metadata.labels:
        from: params.customLabels
  xrd:
    group: example.org
    kind: XApp
    plural: xapps
    scope: Namespaced
    version: v1alpha1
    parameters:
      customLabels:
        type: object
`
	b, err := Load(write(t, reproYAML))
	if err != nil {
		t.Fatalf("expected Load to accept untyped object parameter into metadata.labels, got: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil Blueprint")
	}

	// Test various dotted map leaf paths on native and managed resources
	cases := []struct {
		name      string
		fieldPath string
		paramType string
		props     map[string]Parameter
		wantErr   bool
	}{
		{
			name:      "metadata.labels untyped object",
			fieldPath: "metadata.labels",
			paramType: "object",
			wantErr:   false,
		},
		{
			name:      "metadata.annotations untyped object",
			fieldPath: "metadata.annotations",
			paramType: "object",
			wantErr:   false,
		},
		{
			name:      "spec.template.metadata.labels untyped object",
			fieldPath: "spec.template.metadata.labels",
			paramType: "object",
			wantErr:   false,
		},
		{
			name:      "spec.template.metadata.annotations untyped object",
			fieldPath: "spec.template.metadata.annotations",
			paramType: "object",
			wantErr:   false,
		},
		{
			name:      "nested spec.tags untyped object",
			fieldPath: "spec.tags",
			paramType: "object",
			wantErr:   false,
		},
		{
			name:      "metadata.labels typed object with properties",
			fieldPath: "metadata.labels",
			paramType: "object",
			props: map[string]Parameter{
				"env": {Type: "string"},
			},
			wantErr: false,
		},
		{
			name:      "non-map leaf field still rejected",
			fieldPath: "spec.config",
			paramType: "object",
			wantErr:   true,
		},
		{
			name:      "non-map leaf field ending with non-dot labels still rejected",
			fieldPath: "spec.showLabels",
			paramType: "object",
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := &Blueprint{
				APIVersion: "factory.crossplane.io/v1alpha1",
				Kind:       "Blueprint",
				Metadata:   Metadata{Name: "test-bp"},
				Spec: Spec{
					XRD: XRD{
						Group:   "example.org",
						Kind:    "XTest",
						Plural:  "xtests",
						Version: "v1alpha1",
						Scope:   "Namespaced",
						Parameters: map[string]Parameter{
							"custom": {
								Type:       tc.paramType,
								Properties: tc.props,
							},
						},
					},
					Resources: []Resource{
						{
							Name:     "test-res",
							Kind:     "ConfigMap",
							Provider: "k8s",
							Fields: map[string]Field{
								tc.fieldPath: {From: "params.custom"},
							},
						},
					},
				},
			}
			err := bp.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for field %q, got nil", tc.fieldPath)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected field %q to be accepted, got: %v", tc.fieldPath, err)
			}
		})
	}
}
