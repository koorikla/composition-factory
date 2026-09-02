package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromKubeconfigAndFetchCRDs(t *testing.T) {
	mockCRD := `{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind": "CustomResourceDefinition",
		"metadata": {"name": "certificates.cert-manager.io"},
		"spec": {
			"group": "cert-manager.io",
			"scope": "Namespaced",
			"names": {
				"kind": "Certificate",
				"plural": "certificates",
				"categories": ["cert-manager"]
			},
			"versions": [{
				"name": "v1",
				"served": true,
				"storage": true,
				"schema": {
					"openAPIV3Schema": {
						"type": "object",
						"properties": {
							"spec": {
								"type": "object",
								"properties": {
									"secretName": {"type": "string"}
								}
							}
						}
					}
				}
			}]
		}
	}`

	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions" {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"items": []json.RawMessage{json.RawMessage(mockCRD)},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	kubeconfigYAML := `
apiVersion: v1
clusters:
- cluster:
    server: ` + srv.URL + `
    insecure-skip-tls-verify: true
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-ctx
current-context: test-ctx
users:
- name: test-user
  user:
    token: secret-token-123
`

	client, err := FromKubeconfig([]byte(kubeconfigYAML), "")
	if err != nil {
		t.Fatalf("FromKubeconfig failed: %v", err)
	}

	if client.Server() != srv.URL {
		t.Errorf("Server() = %q, want %q", client.Server(), srv.URL)
	}
	if client.Context() != "test-ctx" {
		t.Errorf("Context() = %q, want %q", client.Context(), "test-ctx")
	}

	crds, err := client.FetchCRDs(context.Background())
	if err != nil {
		t.Fatalf("FetchCRDs failed: %v", err)
	}

	if authHeader != "Bearer secret-token-123" {
		t.Errorf("authHeader = %q, want Bearer secret-token-123", authHeader)
	}

	if len(crds) != 1 {
		t.Fatalf("got %d crds, want 1", len(crds))
	}
	c := crds[0]
	if c.Kind != "Certificate" || c.Group != "cert-manager.io" {
		t.Errorf("got CRD %s.%s, want Certificate.cert-manager.io", c.Kind, c.Group)
	}
	if !c.Native {
		t.Errorf("non-managed custom resource should have Native=true")
	}

	info := client.Info(context.Background())
	if !info.Connected || info.CRDCount != 1 {
		t.Errorf("Info() = %+v, want Connected=true, CRDCount=1", info)
	}
}
