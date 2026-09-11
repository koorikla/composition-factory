package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/cluster"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// saveTestProvider caches a test provider package with one dummy CRD in the store.
func saveTestProvider(t *testing.T, store *cache.Store, ref string) {
	t.Helper()
	pkg := &xpkg.Package{Ref: ref, Digest: "sha256:dummy"}
	crds := []schema.CRD{
		{
			Group:      "example.org",
			Kind:       "TestResource",
			Plural:     "testresources",
			Categories: []string{"managed"},
			Versions: []schema.Version{
				{Name: "v1alpha1", Served: true, Storage: true},
			},
		},
	}
	if err := store.Save(pkg, crds); err != nil {
		t.Fatalf("saveTestProvider %s: %v", ref, err)
	}
}

func TestAssembleProvidersDeduplicatesSources(t *testing.T) {
	dir := t.TempDir()
	store := cache.New(filepath.Join(dir, "cache"))
	ref := "example.org/provider-a:v1"
	saveTestProvider(t, store, ref)

	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: ref},
				{Provider: ref},
				{Provider: ref},
			},
		},
	}

	got := AssembleProviders(store, bp, nil, false)
	want := []string{ref}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AssembleProviders() = %v, want %v", got, want)
	}
}

func TestAssembleProvidersWarnsOnCacheMisses(t *testing.T) {
	dir := t.TempDir()
	store := cache.New(filepath.Join(dir, "cache"))
	refCached := "example.org/provider-cached:v1"
	refMissing := "example.org/provider-missing:v1"
	saveTestProvider(t, store, refCached)

	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: refCached},
				{Provider: refMissing},
			},
		},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	got := AssembleProviders(store, bp, nil, false)

	_ = w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	stderrOutput := buf.String()

	want := []string{refCached}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AssembleProviders() = %v, want %v", got, want)
	}

	expectedWarning := fmt.Sprintf("cf: warning: provider %q is not in the cache — continuing without it; schemas load on demand", refMissing)
	if !strings.Contains(stderrOutput, expectedWarning) {
		t.Errorf("stderr = %q, want it to contain %q", stderrOutput, expectedWarning)
	}
}

func TestAssembleProvidersLoadsClusterCRDsIfClientPresent(t *testing.T) {
	t.Run("SyncClusterNow", func(t *testing.T) {
		dir := t.TempDir()
		store := cache.New(filepath.Join(dir, "cache"))
		ref := "example.org/provider-a:v1"
		saveTestProvider(t, store, ref)

		mockCRD := `{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind": "CustomResourceDefinition",
			"metadata": {"name": "widgets.example.org"},
			"spec": {
				"group": "example.org",
				"names": {"kind": "Widget", "plural": "widgets"},
				"versions": [{"name": "v1", "served": true, "storage": true}]
			}
		}`

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions" {
				w.Header().Set("Content-Type", "application/json")
				resp := map[string]any{
					"items": []json.RawMessage{json.RawMessage(mockCRD)},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		kubeconfigYAML := fmt.Sprintf(`
apiVersion: v1
clusters:
- cluster:
    server: %s
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
`, srv.URL)

		cl, err := cluster.FromKubeconfig([]byte(kubeconfigYAML), "")
		if err != nil {
			t.Fatalf("FromKubeconfig: %v", err)
		}

		bp := &blueprint.Blueprint{
			Spec: blueprint.Spec{
				Sources: []blueprint.Source{
					{Provider: ref},
				},
			},
		}

		got := AssembleProviders(store, bp, cl, true)
		want := []string{ref, cluster.ProviderLabel}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("AssembleProviders() = %v, want %v", got, want)
		}

		// Verify that cluster CRDs were saved to store
		cachedCluster, err := store.Load(cluster.ProviderLabel)
		if err != nil || len(cachedCluster) == 0 {
			t.Fatalf("expected cluster CRDs to be saved in store: %v (count %d)", err, len(cachedCluster))
		}
	})

	t.Run("CachedClusterWithoutSync", func(t *testing.T) {
		dir := t.TempDir()
		store := cache.New(filepath.Join(dir, "cache"))
		ref := "example.org/provider-a:v1"
		saveTestProvider(t, store, ref)

		// Save cluster CRDs in cache ahead of time
		clusterCRDs := []schema.CRD{
			{
				Group: "cluster.example.org",
				Kind:  "ClusterThing",
				Versions: []schema.Version{
					{Name: "v1", Served: true, Storage: true},
				},
			},
		}
		if err := store.SaveCRDs(cluster.ProviderLabel, "test-ctx", clusterCRDs); err != nil {
			t.Fatalf("SaveCRDs: %v", err)
		}

		dummyKubeconfig := `
apiVersion: v1
clusters:
- cluster:
    server: http://127.0.0.1:1
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
  name: test-ctx
current-context: test-ctx
`
		cl, err := cluster.FromKubeconfig([]byte(dummyKubeconfig), "")
		if err != nil {
			t.Fatalf("FromKubeconfig: %v", err)
		}

		bp := &blueprint.Blueprint{
			Spec: blueprint.Spec{
				Sources: []blueprint.Source{
					{Provider: ref},
				},
			},
		}

		got := AssembleProviders(store, bp, cl, false)
		want := []string{ref, cluster.ProviderLabel}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("AssembleProviders() = %v, want %v", got, want)
		}
	})
}

func TestAssembleProvidersFallsBackToStoreListWhenSourcesEmpty(t *testing.T) {
	dir := t.TempDir()
	store := cache.New(filepath.Join(dir, "cache"))
	refA := "example.org/provider-a:v1"
	refB := "example.org/provider-b:v1"
	saveTestProvider(t, store, refA)
	saveTestProvider(t, store, refB)

	want := []string{refA, refB}

	t.Run("NilBlueprint", func(t *testing.T) {
		got := AssembleProviders(store, nil, nil, false)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("AssembleProviders(nil) = %v, want %v", got, want)
		}
	})

	t.Run("EmptySourcesInBlueprint", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Spec: blueprint.Spec{
				Sources: []blueprint.Source{},
			},
		}
		got := AssembleProviders(store, bp, nil, false)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("AssembleProviders(empty sources) = %v, want %v", got, want)
		}
	})
}
