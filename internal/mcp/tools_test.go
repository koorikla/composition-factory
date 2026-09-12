package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEmptyIdentifierArgumentsRejected(t *testing.T) {
	s := newStack(t)

	tests := []struct {
		name      string
		tool      string
		args      any
		wantError string
	}{
		{
			name:      "get_kind_fields empty api_version",
			tool:      "get_kind_fields",
			args:      map[string]any{"api_version": "", "kind": "Queue"},
			wantError: "api_version is required",
		},
		{
			name:      "get_kind_fields empty kind",
			tool:      "get_kind_fields",
			args:      map[string]any{"api_version": "sqs.aws.m.upbound.io/v1beta1", "kind": ""},
			wantError: "kind is required",
		},
		{
			name:      "get_kind_fields both empty",
			tool:      "get_kind_fields",
			args:      map[string]any{"api_version": "", "kind": ""},
			wantError: "api_version is required",
		},
		{
			name:      "update_parameter empty name",
			tool:      "update_parameter",
			args:      map[string]any{"name": "", "parameter": map[string]any{"type": "string"}},
			wantError: "name is required",
		},
		{
			name:      "rename_parameter empty name",
			tool:      "rename_parameter",
			args:      map[string]any{"name": "", "to": "newParam"},
			wantError: "name is required",
		},
		{
			name:      "rename_parameter empty to",
			tool:      "rename_parameter",
			args:      map[string]any{"name": "location", "to": ""},
			wantError: "to is required",
		},
		{
			name:      "delete_parameter empty name",
			tool:      "delete_parameter",
			args:      map[string]any{"name": ""},
			wantError: "name is required",
		},
		{
			name:      "update_resource empty name",
			tool:      "update_resource",
			args:      map[string]any{"name": "", "resource": map[string]any{"kind": "Queue"}},
			wantError: "name is required",
		},
		{
			name:      "rename_resource empty name",
			tool:      "rename_resource",
			args:      map[string]any{"name": "", "to": "newRes"},
			wantError: "name is required",
		},
		{
			name:      "rename_resource empty to",
			tool:      "rename_resource",
			args:      map[string]any{"name": "main-queue", "to": ""},
			wantError: "to is required",
		},
		{
			name:      "delete_resource empty name",
			tool:      "delete_resource",
			args:      map[string]any{"name": ""},
			wantError: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := s.callTool(t, tt.tool, tt.args)
			if !isErr {
				t.Fatalf("%s(%+v) succeeded (%s), want isError: true", tt.tool, tt.args, text)
			}
			if !strings.Contains(text, tt.wantError) {
				t.Errorf("%s(%+v) error %q does not contain %q", tt.tool, tt.args, text, tt.wantError)
			}
		})
	}
}

func TestAddResourceNameMismatchRejected(t *testing.T) {
	s := newStack(t)
	text, isErr := s.callTool(t, "add_resource", map[string]any{
		"name": "res-a",
		"resource": map[string]any{
			"name":     "res-b",
			"kind":     "Queue",
			"provider": testProviderRef,
		},
	})
	if !isErr {
		t.Fatalf("add_resource with mismatched name succeeded (%s), want isError: true", text)
	}
	want := `resource name in body "res-b" does not match name argument "res-a"`
	if !strings.Contains(text, want) {
		t.Errorf("add_resource error %q does not contain %q", text, want)
	}

	// Same name in body and argument is accepted.
	s.toolOK(t, "add_resource", map[string]any{
		"name": "res-a",
		"resource": map[string]any{
			"name":     "res-a",
			"kind":     "Queue",
			"provider": testProviderRef,
		},
	})
}

func TestAdoptComposition_RevisionOCC(t *testing.T) {
	s := newStack(t)

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-adopted-mcp-occ
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

	// 1. Get current blueprint revision.
	getRes, err := s.session.CallTool(context.Background(), &sdk.CallToolParams{
		Name: "get_blueprint",
	})
	if err != nil {
		t.Fatalf("CallTool get_blueprint: %v", err)
	}
	currentRev, ok := getRes.Meta["revision"].(string)
	if !ok || currentRev == "" {
		t.Fatalf("get_blueprint returned empty revision in metadata: %+v", getRes.Meta)
	}

	// 2. Call adopt_composition with stale revision and persist: true.
	text, isErr := s.callTool(t, "adopt_composition", map[string]any{
		"manifest": manifest,
		"provider": testProviderRef,
		"persist":  true,
		"revision": "stale-revision",
	})
	if !isErr {
		t.Fatalf("adopt_composition succeeded with stale revision, want isError: true")
	}
	if !strings.Contains(text, "precondition failed") {
		t.Errorf("expected error text containing precondition failed, got %q", text)
	}

	// 3. Call adopt_composition with stale if_match alias and persist: true.
	text, isErr = s.callTool(t, "adopt_composition", map[string]any{
		"manifest": manifest,
		"provider": testProviderRef,
		"persist":  true,
		"if_match": "stale-revision",
	})
	if !isErr {
		t.Fatalf("adopt_composition succeeded with stale if_match, want isError: true")
	}
	if !strings.Contains(text, "precondition failed") {
		t.Errorf("expected error text containing precondition failed, got %q", text)
	}

	// 4. Call adopt_composition with matching revision and persist: true.
	s.toolOK(t, "adopt_composition", map[string]any{
		"manifest": manifest,
		"provider": testProviderRef,
		"persist":  true,
		"revision": currentRev,
	})

	if s.reload(t).Metadata.Name != "test-adopted-mcp-occ" {
		t.Errorf("reloaded blueprint name = %q, want test-adopted-mcp-occ", s.reload(t).Metadata.Name)
	}
}
