package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/cluster"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func TestClusterAPIEndpoints(t *testing.T) {
	// Mock k8s server
	mockCRD := `{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind": "CustomResourceDefinition",
		"metadata": {"name": "issuers.cert-manager.io"},
		"spec": {
			"group": "cert-manager.io",
			"scope": "Namespaced",
			"names": {
				"kind": "Issuer",
				"plural": "issuers",
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
									"acme": {"type": "object"}
								}
							}
						}
					}
				}
			}]
		}
	}`

	mockK8s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"items": []json.RawMessage{json.RawMessage(mockCRD)},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mockK8s.Close()

	kubeconfigYAML := `
apiVersion: v1
clusters:
- cluster:
    server: ` + mockK8s.URL + `
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
    token: test-token
`

	cl, err := cluster.FromKubeconfig([]byte(kubeconfigYAML), "")
	if err != nil {
		t.Fatalf("FromKubeconfig failed: %v", err)
	}

	tempDir := t.TempDir()
	bpPath := filepath.Join(tempDir, "blueprint.yaml")
	_ = os.WriteFile(bpPath, []byte("spec:\n  resources: []\n"), 0o644)

	store := cache.New(filepath.Join(tempDir, "cache"))
	idx, _ := index.Build(map[string][]schema.CRD{})

	h, err := New(Options{
		Blueprint:     bpPath,
		OutDir:        tempDir,
		Lock:          filepath.Join(tempDir, ".cf.lock"),
		Store:         store,
		Index:         idx,
		ClusterClient: cl,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// 1. GET /api/cluster
	req := httptest.NewRequest(http.MethodGet, "/api/cluster", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/cluster code = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var info cluster.ClusterInfo
	if err := json.NewDecoder(rr.Body).Decode(&info); err != nil {
		t.Fatalf("decode GET /api/cluster body: %v", err)
	}
	if !info.Connected || info.CRDCount != 1 {
		t.Errorf("info = %+v, want Connected=true, CRDCount=1", info)
	}

	// 2. POST /api/cluster/sync
	reqSync := httptest.NewRequest(http.MethodPost, "/api/cluster/sync", nil)
	rrSync := httptest.NewRecorder()
	h.ServeHTTP(rrSync, reqSync)

	if rrSync.Code != http.StatusOK {
		t.Fatalf("POST /api/cluster/sync code = %d, want 200: %s", rrSync.Code, rrSync.Body.String())
	}

	// 3. GET /api/kinds should now contain Issuer
	reqKinds := httptest.NewRequest(http.MethodGet, "/api/kinds", nil)
	rrKinds := httptest.NewRecorder()
	h.ServeHTTP(rrKinds, reqKinds)

	if rrKinds.Code != http.StatusOK {
		t.Fatalf("GET /api/kinds code = %d, want 200", rrKinds.Code)
	}
	var kindsResp struct {
		Kinds []struct {
			Kind     string `json:"kind"`
			Provider string `json:"provider"`
		} `json:"kinds"`
	}
	if err := json.NewDecoder(rrKinds.Body).Decode(&kindsResp); err != nil {
		t.Fatalf("decode GET /api/kinds: %v", err)
	}

	found := false
	for _, k := range kindsResp.Kinds {
		if k.Kind == "Issuer" && k.Provider == cluster.ProviderLabel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Issuer from cluster in /api/kinds, got: %+v", kindsResp.Kinds)
	}
}
