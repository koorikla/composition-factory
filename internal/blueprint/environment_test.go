package blueprint

import (
	"strings"
	"testing"
)

func TestValidateEnvironment_Valid(t *testing.T) {
	manifest := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xapp
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    region:
      type: string
      default: "us-east-1"
      description: "Target deployment region"
    clusterCount:
      type: integer
      default: 3
    costFactor:
      type: number
      default: 1.5
    featureFlagsEnabled:
      type: boolean
      default: true
  resources:
    - name: queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {from: env.region}
`
	bp, err := Load(write(t, manifest))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if len(bp.Spec.Environment) != 4 {
		t.Fatalf("expected 4 environment keys, got %d", len(bp.Spec.Environment))
	}
	if bp.Spec.Environment["region"].Default != "us-east-1" {
		t.Errorf("expected default us-east-1, got %q", bp.Spec.Environment["region"].Default)
	}
	if bp.Spec.Environment["clusterCount"].Default != "3" {
		t.Errorf("expected default 3, got %q", bp.Spec.Environment["clusterCount"].Default)
	}
	if bp.Spec.Environment["featureFlagsEnabled"].Default != "true" {
		t.Errorf("expected default true, got %q", bp.Spec.Environment["featureFlagsEnabled"].Default)
	}
}

func TestValidateEnvironment_InvalidKeyNames(t *testing.T) {
	cases := []struct {
		key     string
		wantErr string
	}{
		{"region-name", "must be camelCase"},
		{"123region", "must be camelCase"},
		{"true", "must be camelCase"},
		{"false", "must be camelCase"},
		{"null", "must be camelCase"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			b := &Blueprint{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "test"},
				Spec: Spec{
					Sources: []Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"}},
					XRD: XRD{
						Group:   "platform.example.org",
						Kind:    "XApp",
						Plural:  "xapps",
						Version: "v1alpha1",
						Scope:   "Namespaced",
						Parameters: map[string]Parameter{
							"providerName": {Type: "string", Required: true},
						},
					},
					Environment: map[string]EnvironmentKey{
						tc.key: {Type: "string"},
					},
				},
			}
			err := b.Validate()
			if err == nil {
				t.Fatalf("expected error for key %q, got nil", tc.key)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateEnvironment_InvalidTypesAndDefaults(t *testing.T) {
	cases := []struct {
		name    string
		envKey  EnvironmentKey
		wantErr string
	}{
		{
			name:    "invalid type",
			envKey:  EnvironmentKey{Type: "object"},
			wantErr: `unknown type "object" (must be string, integer, number, or boolean)`,
		},
		{
			name:    "invalid int default",
			envKey:  EnvironmentKey{Type: "integer", Default: "not-a-number"},
			wantErr: `default "not-a-number" is not a valid integer`,
		},
		{
			name:    "float for int default",
			envKey:  EnvironmentKey{Type: "integer", Default: "3.14"},
			wantErr: `default "3.14" is not a valid integer`,
		},
		{
			name:    "invalid number default",
			envKey:  EnvironmentKey{Type: "number", Default: "abc"},
			wantErr: `default "abc" is not a valid number`,
		},
		{
			name:    "invalid boolean default",
			envKey:  EnvironmentKey{Type: "boolean", Default: "yes"},
			wantErr: `default "yes" is not a valid boolean`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &Blueprint{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "test"},
				Spec: Spec{
					Sources: []Source{{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v2"}},
					XRD: XRD{
						Group:   "platform.example.org",
						Kind:    "XApp",
						Plural:  "xapps",
						Version: "v1alpha1",
						Scope:   "Namespaced",
						Parameters: map[string]Parameter{
							"providerName": {Type: "string", Required: true},
						},
					},
					Environment: map[string]EnvironmentKey{
						"myKey": tc.envKey,
					},
				},
			}
			err := b.Validate()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateEnvironment_NearestMatchSuggestions(t *testing.T) {
	manifest := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xapp
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    region: {type: string}
    environmentTier: {type: string}
  resources:
    - name: queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      fields:
        region: {from: env.regino}
`
	_, err := Load(write(t, manifest))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `unknown environment key "regino"; did you mean "region"?`) {
		t.Errorf("err = %q, want did you mean suggestion for region", err.Error())
	}
}

func TestValidateEnvironment_WhenAndForEach(t *testing.T) {
	manifest := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xapp
spec:
  sources:
    - provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
  xrd:
    group: platform.example.org
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  environment:
    tier: {type: string}
    enabled: {type: boolean}
    count: {type: integer}
  resources:
    - name: queue
      kind: Queue
      provider: xpkg.upbound.io/upbound/provider-aws-sqs:v2
      when: env.enabled
      forEach: env.count
      fields:
        tier: {from: env.tier}
`
	bp, err := Load(write(t, manifest))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bp.Spec.Resources[0].When != "env.enabled" {
		t.Errorf("expected when env.enabled, got %q", bp.Spec.Resources[0].When)
	}
	if bp.Spec.Resources[0].ForEach != "env.count" {
		t.Errorf("expected forEach env.count, got %q", bp.Spec.Resources[0].ForEach)
	}
}
