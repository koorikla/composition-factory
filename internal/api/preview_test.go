package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreviewExpressionEndpoint(t *testing.T) {
	h, _ := testHandlerWithPath(t)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantRender string
		wantSubErr string
	}{
		{
			name:       "valid expression with $xr",
			body:       `{"expression": "{{ $xr }}-queue", "resource": "main-queue"}`,
			wantStatus: 200,
			wantRender: "sample-xqueue-queue",
		},
		{
			name:       "valid expression with $spec",
			body:       `{"expression": "region: {{ $spec.providerName }}"}`,
			wantStatus: 200,
			wantRender: "region: sample",
		},
		{
			name:       "undeclared environment key on empty env returns 400",
			body:       `{"expression": "env is {{ $env.nonexistentKey }}"}`,
			wantStatus: 400,
			wantSubErr: "map has no entry for key",
		},
		{
			name: "valid expression with declared environment in blueprint",
			body: `{
				"expression": "env is {{ $env.clusterEnv }}",
				"blueprint": {
					"apiVersion": "factory.crossplane.io/v1alpha1",
					"kind": "Blueprint",
					"metadata": {"name": "test"},
					"spec": {
						"environment": {
							"clusterEnv": {"type": "string", "default": "staging"}
						},
						"xrd": {
							"group": "example.org",
							"kind": "Test",
							"plural": "tests",
							"version": "v1",
							"scope": "Cluster"
						}
					}
				}
			}`,
			wantStatus: 200,
			wantRender: "env is staging",
		},
		{
			name:       "valid expression with sibling status",
			body:       `{"expression": "{{ (index $.observed.resources \"main-queue\").resource.status.atProvider.id }}"}`,
			wantStatus: 200,
			wantRender: "main-queue-id-12345",
		},
		{
			name:       "undeclared resource returns 400",
			body:       `{"expression": "hello", "resource": "nonexistent-resource"}`,
			wantStatus: 400,
			wantSubErr: "not declared in blueprint",
		},
		{
			name:       "invalid template syntax returns 400",
			body:       `{"expression": "{{ $xr"}`,
			wantStatus: 400,
			wantSubErr: "unclosed action",
		},
		{
			name:       "invalid missing key returns 400",
			body:       `{"expression": "{{ $spec.nonexistentField }}"}`,
			wantStatus: 400,
			wantSubErr: "map has no entry for key",
		},
		{
			name:       "malformed request body",
			body:       `{invalid json`,
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/preview-expression", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("POST /api/preview-expression returned %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == 200 {
				var resp previewExpressionResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if tt.wantRender != "" && !strings.Contains(resp.Rendered, tt.wantRender) {
					t.Errorf("rendered %q does not contain %q", resp.Rendered, tt.wantRender)
				}
			} else if tt.wantSubErr != "" {
				var errResp map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to unmarshal error response: %v", err)
				}
				errStr, _ := errResp["error"].(string)
				if !strings.Contains(errStr, tt.wantSubErr) {
					t.Errorf("error %q does not contain %q", errStr, tt.wantSubErr)
				}
			}
		})
	}
}
