package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestKubeconfigErrorPaths(t *testing.T) {
	// 1. Invalid YAML
	_, err := FromKubeconfig([]byte("not: valid: yaml: ["), "")
	if err == nil {
		t.Errorf("expected error for invalid YAML, got nil")
	}

	// 2. No contexts
	_, err = FromKubeconfig([]byte("clusters: []\nusers: []\n"), "")
	if err == nil || !strings.Contains(err.Error(), "no contexts") {
		t.Errorf("expected 'no contexts' error, got %v", err)
	}

	// 3. Specified context not found
	cfgWithCtx := `
current-context: default
contexts:
- name: default
  context:
    cluster: main-cluster
    user: main-user
clusters:
- name: main-cluster
  cluster:
    server: https://127.0.0.1:6443
`
	_, err = FromKubeconfig([]byte(cfgWithCtx), "non-existent-ctx")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected context not found error, got %v", err)
	}

	// 4. Cluster not found for context
	missingClusterCfg := `
contexts:
- name: default
  context:
    cluster: missing-cluster
    user: main-user
clusters: []
`
	_, err = FromKubeconfig([]byte(missingClusterCfg), "default")
	if err == nil || !strings.Contains(err.Error(), "cluster \"missing-cluster\" not found") {
		t.Errorf("expected cluster not found error, got %v", err)
	}
}

func TestFetchCRDsErrorsAndInfo(t *testing.T) {
	// 1. Server returns 500 error
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv500.Close()

	cfg500 := `
contexts:
- name: default
  context:
    cluster: c
clusters:
- name: c
  cluster:
    server: ` + srv500.URL + `
`
	client500, err := FromKubeconfig([]byte(cfg500), "default")
	if err != nil {
		t.Fatalf("FromKubeconfig failed: %v", err)
	}

	_, err = client500.FetchCRDs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 error, got %v", err)
	}

	info500 := client500.Info(context.Background())
	if info500.Connected || info500.Error == "" {
		t.Errorf("expected Info to report disconnected on 500, got %+v", info500)
	}

	// 2. Server returns malformed JSON
	srvBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{not-valid-json"))
	}))
	defer srvBadJSON.Close()

	cfgBadJSON := `
contexts:
- name: default
  context:
    cluster: c
clusters:
- name: c
  cluster:
    server: ` + srvBadJSON.URL + `
`
	clientBadJSON, _ := FromKubeconfig([]byte(cfgBadJSON), "default")
	_, err = clientBadJSON.FetchCRDs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode crd list") {
		t.Errorf("expected decode error, got %v", err)
	}

	// 3. Unreachable server
	clientUnreachable, _ := FromKubeconfig([]byte(`
contexts:
- name: default
  context:
    cluster: c
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:65530
`), "default")
	_, err = clientUnreachable.FetchCRDs(context.Background())
	if err == nil {
		t.Errorf("expected connection error for unreachable server, got nil")
	}
}

func TestInClusterClient(t *testing.T) {
	// When KUBERNETES_SERVICE_HOST is unset
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	_, err := InClusterClient()
	if err == nil || !strings.Contains(err.Error(), "not running in-cluster") {
		t.Errorf("expected not running in-cluster error, got %v", err)
	}
}

func TestDefaultKubeconfigPath(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/custom-kubeconfig:/tmp/other")
	if got := DefaultKubeconfigPath(); got != "/tmp/custom-kubeconfig" {
		t.Errorf("DefaultKubeconfigPath() = %q, want /tmp/custom-kubeconfig", got)
	}
}

func TestNewClient(t *testing.T) {
	// 1. Non-existent file path
	_, err := NewClient("/non/existent/kubeconfig/path", "")
	if err == nil || !strings.Contains(err.Error(), "read kubeconfig") {
		t.Errorf("expected read error, got %v", err)
	}

	// 2. Valid file path
	tmpDir := t.TempDir()
	validKubeconfig := filepath.Join(tmpDir, "config")
	cfg := `
current-context: dev
contexts:
- name: dev
  context:
    cluster: local
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:6443
`
	if err := os.WriteFile(validKubeconfig, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write temp kubeconfig: %v", err)
	}

	c, err := NewClient(validKubeconfig, "dev")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.Context() != "dev" || c.Server() != "https://127.0.0.1:6443" {
		t.Errorf("NewClient context/server = %s/%s, want dev/https://127.0.0.1:6443", c.Context(), c.Server())
	}
}

func TestKubeconfigTLSAuth(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.crt")
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")

	os.WriteFile(caFile, []byte("fake-ca"), 0o644)
	os.WriteFile(certFile, []byte("fake-cert"), 0o644)
	os.WriteFile(keyFile, []byte("fake-key"), 0o644)

	cfg := `
current-context: tls-ctx
contexts:
- name: tls-ctx
  context:
    cluster: tls-cluster
    user: tls-user
clusters:
- name: tls-cluster
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority: ` + caFile + `
users:
- name: tls-user
  user:
    client-certificate: ` + certFile + `
    client-key: ` + keyFile + `
`
	c, err := FromKubeconfig([]byte(cfg), "tls-ctx")
	if err != nil {
		t.Fatalf("FromKubeconfig with TLS paths failed: %v", err)
	}
	if c.Context() != "tls-ctx" {
		t.Errorf("Context() = %q, want tls-ctx", c.Context())
	}
}
