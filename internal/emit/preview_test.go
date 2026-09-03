package emit

import (
	"context"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func TestPreviewExpression(t *testing.T) {
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Group:   "example.org",
				Version: "v1alpha1",
				Kind:    "XDatabase",
				Plural:  "xdatabases",
				Parameters: map[string]blueprint.Parameter{
					"tier": {
						Type:    "string",
						Default: "standard",
					},
					"nodes": {
						Type:    "integer",
						Default: "3",
					},
					"tags": {
						Type: "object",
						Properties: map[string]blueprint.Parameter{
							"env": {Type: "string", Default: "prod"},
						},
					},
				},
			},
			Resources: []blueprint.Resource{
				{
					Name:     "db-instance",
					Kind:     "Instance",
					Provider: "aws",
				},
				{
					Name:     "db-cluster",
					Kind:     "DBCluster",
					Provider: "aws",
				},
			},
			Templates: map[string]string{
				"cf.customTag": `env: {{ .spec.tier }}`,
			},
			Environment: map[string]blueprint.EnvironmentKey{
				"region": {Type: "string", Default: "us-west-2"},
			},
		},
	}

	tests := []struct {
		name    string
		expr    string
		res     string
		wantSub string
		wantErr bool
		errSub  string
	}{
		{
			name:    "empty expression",
			expr:    "",
			wantSub: "",
		},
		{
			name:    "plain string",
			expr:    "plain-string",
			wantSub: "plain-string",
		},
		{
			name:    "composite name interpolation",
			expr:    "{{ $xr }}-instance",
			wantSub: "sample-xdatabase-instance",
		},
		{
			name:    "parameter dereference",
			expr:    "tier is {{ $spec.tier }}",
			wantSub: "tier is standard",
		},
		{
			name:    "integer parameter",
			expr:    "count: {{ $spec.nodes }}",
			wantSub: "count: 3",
		},
		{
			name:    "nested object parameter",
			expr:    "tag env: {{ $spec.tags.env }}",
			wantSub: "tag env: prod",
		},
		{
			name:    "loop index variable",
			expr:    `{{ printf "%s-%d" $xr $i }}`,
			wantSub: "sample-xdatabase-0",
		},
		{
			name:    "sibling resource status",
			expr:    `{{ (index $.observed.resources "db-instance").resource.status.atProvider.id }}`,
			wantSub: "db-instance-id-12345",
		},
		{
			name:    "sibling resource arn",
			expr:    `{{ (index $.observed.resources "db-instance").resource.status.atProvider.arn }}`,
			wantSub: "arn:aws:service:region:123456789012:db-instance",
		},
		{
			name:    "environment variable",
			expr:    `{{ $env.region }}`,
			wantSub: "us-west-2",
		},
		{
			name:    "sprig function and toYaml",
			expr:    `{{ $spec.tier | upper }}`,
			wantSub: "STANDARD",
		},
		{
			name:    "include user template",
			expr:    `{{ include "cf.customTag" (dict "spec" $spec) }}`,
			wantSub: "env: standard",
		},
		{
			name:    "syntax error",
			expr:    `{{ $xr`,
			wantErr: true,
			errSub:  "unclosed action",
		},
		{
			name:    "missing key error under missingkey=error",
			expr:    `{{ $spec.nonexistentField }}`,
			wantErr: true,
			errSub:  "map has no entry for key",
		},
		{
			name:    "unknown function error",
			expr:    `{{ unknownFunction 123 }}`,
			wantErr: true,
			errSub:  "function \"unknownFunction\" not defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := PreviewExpression(bp, tt.res, tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("PreviewExpression(%q) expected error, got rendered %q", tt.expr, rendered)
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("PreviewExpression(%q) unexpected error: %v", tt.expr, err)
			}
			if !strings.Contains(rendered, tt.wantSub) {
				t.Errorf("rendered %q does not contain %q", rendered, tt.wantSub)
			}
		})
	}
}

func TestPreviewExpression_ExecutionBounds(t *testing.T) {
	t.Run("infinite recursion in template include", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Spec: blueprint.Spec{
				Templates: map[string]string{
					"cf.recursive": `{{ include "cf.recursive" . }}`,
				},
			},
		}
		_, err := PreviewExpression(bp, "", `{{ include "cf.recursive" . }}`)
		if err == nil {
			t.Fatal("expected recursion limit error, got nil")
		}
		if !strings.Contains(err.Error(), "maximum template include depth") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("output size cap exceeded", func(t *testing.T) {
		bp := &blueprint.Blueprint{}
		// Generate an expression that produces >1MB output
		_, err := PreviewExpression(bp, "", `{{ range $i := until 10000 }}{{ repeat 120 "A" }}{{ end }}`)
		if err == nil {
			t.Fatal("expected output size cap error, got nil")
		}
		if !strings.Contains(err.Error(), "preview output size exceeded maximum limit") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("until upper bound exceeded", func(t *testing.T) {
		bp := &blueprint.Blueprint{}
		_, err := PreviewExpression(bp, "", `{{ until 20000 }}`)
		if err == nil {
			t.Fatal("expected until limit error, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds maximum limit of 10000") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("context cancellation terminates execution", func(t *testing.T) {
		bp := &blueprint.Blueprint{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately
		_, err := PreviewExpressionContext(ctx, bp, "", `{{ $spec }}`)
		if err == nil {
			t.Fatal("expected canceled context error, got nil")
		}
	})
}
