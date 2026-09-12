package blueprint

import (
	"strings"
	"testing"
)

func TestValidateRootMetadataName(t *testing.T) {
	newBP := func(name string) *Blueprint {
		return &Blueprint{
			APIVersion: APIVersion,
			Kind:       Kind,
			Metadata:   Metadata{Name: name},
		}
	}

	validNames := []string{
		"sqs-queue",
		"xworkloadfulls.platform.sparky.ee",
		"a",
		"1",
		"my-app-1",
		"a.b.c",
		"sub-domain.example.com",
		strings.Repeat("a", 253),
	}

	for _, name := range validNames {
		t.Run("valid_"+name, func(t *testing.T) {
			bp := newBP(name)
			if err := bp.validateRoot(); err != nil {
				t.Errorf("validateRoot() returned unexpected error for valid name %q: %v", name, err)
			}
		})
	}

	t.Run("empty name is required", func(t *testing.T) {
		bp := newBP("")
		err := bp.validateRoot()
		if err == nil {
			t.Fatal("validateRoot() returned nil for empty metadata.name, want error")
		}
		want := "metadata.name is required"
		if err.Error() != want {
			t.Errorf("validateRoot() error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("exceeds max length of 253", func(t *testing.T) {
		longName := strings.Repeat("a", 254)
		bp := newBP(longName)
		err := bp.validateRoot()
		if err == nil {
			t.Fatal("validateRoot() returned nil for name exceeding 253 chars, want error")
		}
		want := `metadata.name: "` + longName + `" exceeds maximum length of 253 characters`
		if err.Error() != want {
			t.Errorf("validateRoot() error = %q, want %q", err.Error(), want)
		}
	})

	invalidDNSNames := []struct {
		name   string
		reason string
	}{
		{"My Blueprint", "contains spaces and uppercase"},
		{"UPPER", "all uppercase"},
		{"myQueue", "camelCase"},
		{"true", "YAML keyword true"},
		{"false", "YAML keyword false"},
		{"yes", "YAML keyword yes"},
		{"no", "YAML keyword no"},
		{"on", "YAML keyword on"},
		{"off", "YAML keyword off"},
		{"null", "YAML keyword null"},
		{"y", "YAML keyword y"},
		{"n", "YAML keyword n"},
		{"-queue", "leading hyphen"},
		{"queue-", "trailing hyphen"},
		{".queue", "leading dot"},
		{"queue.", "trailing dot"},
		{"queue..name", "consecutive dots"},
		{"queue_name", "contains underscore"},
		{"-sqs-queue-", "hyphens at boundaries"},
		{"queue@name", "contains at symbol"},
	}

	for _, tc := range invalidDNSNames {
		t.Run("invalid_"+tc.name, func(t *testing.T) {
			bp := newBP(tc.name)
			err := bp.validateRoot()
			if err == nil {
				t.Fatalf("validateRoot() returned nil for invalid name %q (%s), want error", tc.name, tc.reason)
			}
			wantPrefix := `metadata.name: "` + tc.name + `" is not a valid DNS subdomain name (must be lowercase alphanumeric characters, '-' or '.', and start and end with an alphanumeric character)`
			if err.Error() != wantPrefix {
				t.Errorf("validateRoot() error = %q, want %q", err.Error(), wantPrefix)
			}
		})
	}
}

func TestValidateSourcesRejectsClusterPseudoProvider(t *testing.T) {
	bp := &Blueprint{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "app"},
		Spec: Spec{
			XRD: XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "App",
				Plural:  "apps",
				Scope:   "Namespaced",
				Parameters: map[string]Parameter{
					"region": {Type: "string"},
				},
			},
			Sources: []Source{
				{Provider: "cluster"},
			},
		},
	}
	err := bp.Validate()
	if err == nil {
		t.Fatal("expected bp.Validate() to reject provider: cluster, got nil")
	}
	if !strings.Contains(err.Error(), "cluster") || !strings.Contains(err.Error(), "not a package source") {
		t.Fatalf("expected error mentioning cluster not a package source, got: %v", err)
	}
}

func TestValidateRejectsDuplicateSources(t *testing.T) {
	tests := []struct {
		name       string
		sources    []Source
		wantSubstr string
	}{
		{
			name: "duplicate provider source",
			sources: []Source{
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.2.0"},
				{Provider: "xpkg.upbound.io/upbound/provider-aws-sqs:v1.2.0"},
			},
			wantSubstr: "duplicate provider source",
		},
		{
			name: "duplicate crds source",
			sources: []Source{
				{CRDs: "crds/queue.yaml"},
				{CRDs: "crds/queue.yaml"},
			},
			wantSubstr: "duplicate crds source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Blueprint{
				APIVersion: APIVersion,
				Kind:       Kind,
				Metadata:   Metadata{Name: "test-app"},
				Spec: Spec{
					XRD: XRD{
						Group:   "example.org",
						Version: "v1alpha1",
						Kind:    "App",
						Plural:  "apps",
						Scope:   "Namespaced",
					},
					Sources: tt.sources,
				},
			}
			err := b.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted duplicate sources: %+v, want error containing %q", tt.sources, tt.wantSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("Validate() err = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}
