package blueprint

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateEnvironmentConfigs_RejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		wantErr    string
	}{
		{
			name:       "slash in name",
			configName: `"invalid/name"`,
			wantErr:    `spec.environmentConfigs[0].name: "invalid/name" is not a valid config name`,
		},
		{
			name:       "uppercase in name",
			configName: `"CustomConfig"`,
			wantErr:    `spec.environmentConfigs[0].name: "CustomConfig" is not a valid config name`,
		},
		{
			name:       "yaml keyword yes",
			configName: `yes`,
			wantErr:    `spec.environmentConfigs[0].name: "yes" is not a valid config name`,
		},
		{
			name:       "control character newline",
			configName: `"custom\ninjected: true"`,
			wantErr:    `control character`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources: []
  xrd:
    group: test.org
    version: v1alpha1
    kind: Test
    plural: tests
    scope: Namespaced
  environment:
    region:
      type: string
  environmentConfigs:
    - name: %s
      data:
        region: us-east-1`, tc.configName)

			_, err := Load(write(t, manifest))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateEnvironmentConfigs_RejectsMatchLabelsControlCharacters(t *testing.T) {
	cases := []struct {
		name     string
		selector string
		wantErr  string
	}{
		{
			name: "newline in matchLabels value",
			selector: `selector:
        matchLabels:
          stage: "prod\ninjected: true"`,
			wantErr: "control character",
		},
		{
			name: "newline in matchLabels key",
			selector: `selector:
        matchLabels:
          "stage\ninjected: true": prod`,
			wantErr: "control character",
		},
		{
			name: "carriage return in matchLabels value",
			selector: `selector:
        matchLabels:
          stage: "prod\rinjected: true"`,
			wantErr: "control character",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources: []
  xrd:
    group: test.org
    version: v1alpha1
    kind: Test
    plural: tests
    scope: Namespaced
  environment:
    region:
      type: string
  environmentConfigs:
    - %s
      data:
        region: us-east-1`, tc.selector)

			_, err := Load(write(t, manifest))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateEnvironmentConfigs_RejectsInvalidEffectiveNames(t *testing.T) {
	cases := []struct {
		name     string
		selector string
		wantErr  string
	}{
		{
			name: "matchLabels without alphanumeric characters",
			selector: `selector:
        matchLabels:
          "---": "///"`,
			wantErr: `spec.environmentConfigs[0]: effective config name "" is not a valid config name`,
		},
		{
			name: "matchLabels sanitizing to yaml keyword",
			selector: `selector:
        matchLabels:
          "_": "yes"`,
			wantErr: `spec.environmentConfigs[0]: effective config name "yes" is not a valid config name`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := fmt.Sprintf(`
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  sources: []
  xrd:
    group: test.org
    version: v1alpha1
    kind: Test
    plural: tests
    scope: Namespaced
  environment:
    region:
      type: string
  environmentConfigs:
    - %s
      data:
        region: us-east-1`, tc.selector)

			_, err := Load(write(t, manifest))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}
