package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
)

// TestCF485AssembleProvidersExcludesCachedProvidersWhenBlueprintHasNoSources verifies
// CF-485 (issue #397): AssembleProviders must not fall back to store.List() when
// a blueprint declares no sources (or is nil). Undeclared cached providers must
// remain deferred and never reported as installed providers at startup.
func TestCF485AssembleProvidersExcludesCachedProvidersWhenBlueprintHasNoSources(t *testing.T) {
	dir := t.TempDir()
	store := cache.New(filepath.Join(dir, "cache"))
	refA := "example.org/provider-a:v1"
	refB := "example.org/provider-b:v1"
	saveTestProvider(t, store, refA)
	saveTestProvider(t, store, refB)

	t.Run("NilBlueprint", func(t *testing.T) {
		got := AssembleProviders(store, nil, nil, false)
		if len(got) != 0 {
			t.Errorf("AssembleProviders(nil) returned %v, want empty/no providers", got)
		}
	})

	t.Run("EmptySourcesInBlueprint", func(t *testing.T) {
		bp := &blueprint.Blueprint{
			Spec: blueprint.Spec{
				Sources: []blueprint.Source{},
			},
		}
		got := AssembleProviders(store, bp, nil, false)
		if len(got) != 0 {
			t.Errorf("AssembleProviders(empty sources) returned %v, want empty/no providers", got)
		}
	})
}

// TestCF485BuildAPIOptionsWithBlankBlueprint verifies that buildAPIOptions with a blank
// blueprint does not populate Providers with cached providers, but places them in CachedProviders.
func TestCF485BuildAPIOptionsWithBlankBlueprint(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	store := cache.New(cacheDir)
	refA := "example.org/provider-a:v1"
	refB := "example.org/provider-b:v1"
	saveTestProvider(t, store, refA)
	saveTestProvider(t, store, refB)

	bpPath := filepath.Join(dir, "blank.cf.yaml")
	blankYAML := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: blank-bp
spec:
  sources: []
  xrd:
    group: example.org
    kind: XTest
    plural: xtests
    version: v1alpha1
    scope: Namespaced
`
	if err := os.WriteFile(bpPath, []byte(blankYAML), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}

	opts, err := buildAPIOptions(bpPath, cacheDir, filepath.Join(dir, "out"), filepath.Join(dir, ".cf.lock"), nil, false)
	if err != nil {
		t.Fatalf("buildAPIOptions: %v", err)
	}

	if len(opts.Providers) != 0 {
		t.Errorf("opts.Providers = %v, want empty", opts.Providers)
	}

	wantCached := []string{refA, refB}
	if !reflect.DeepEqual(opts.CachedProviders, wantCached) {
		t.Errorf("opts.CachedProviders = %v, want %v", opts.CachedProviders, wantCached)
	}
}

// TestCF485ServeBlankBlueprintDoesNotReportCachedProvidersAsInstalled reproduces the exact issue #397:
// Starting cf serve with a blank blueprint and a populated cache must NOT report cached providers
// as installed via GET /api/providers, and must NOT serve undeclared provider kinds via GET /api/kinds.
func TestCF485ServeBlankBlueprintDoesNotReportCachedProvidersAsInstalled(t *testing.T) {
	dir, _, cacheDir := seed(t)
	blankPath := filepath.Join(dir, "doc.cf.yaml")
	created, err := ensureBlueprint(blankPath)
	if err != nil || !created {
		t.Fatalf("ensureBlueprint: created=%v err=%v", created, err)
	}

	ready := make(chan string, 1)
	c := &ServeCmd{
		Addr:      "127.0.0.1:0",
		Blueprint: blankPath,
		Out:       filepath.Join(dir, "out"),
		CacheDir:  cacheDir,
		Lock:      filepath.Join(dir, ".cf.lock"),
		NoUI:      true,
		ready:     ready,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.run(ctx, os.Stderr)
	}()

	var addr string
	select {
	case addr = <-ready:
	case err := <-errCh:
		t.Fatalf("server exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server ready")
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. GET /api/providers must return empty providers array, not the cached providers
	resp, err := client.Get("http://" + addr + "/api/providers")
	if err != nil {
		t.Fatalf("GET /api/providers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/providers status = %d", resp.StatusCode)
	}
	var provResp struct {
		Providers []struct {
			Ref string `json:"ref"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&provResp); err != nil {
		t.Fatalf("decode providers response: %v", err)
	}
	if len(provResp.Providers) != 0 {
		t.Fatalf("expected 0 installed providers on blank blueprint, got %d: %+v", len(provResp.Providers), provResp.Providers)
	}

	// 2. GET /api/kinds must only contain native k8s kinds, not cached provider kinds
	respKinds, err := client.Get("http://" + addr + "/api/kinds")
	if err != nil {
		t.Fatalf("GET /api/kinds: %v", err)
	}
	defer respKinds.Body.Close()
	if respKinds.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/kinds status = %d", respKinds.StatusCode)
	}
	var kindsResp struct {
		Kinds []index.Kind `json:"kinds"`
	}
	if err := json.NewDecoder(respKinds.Body).Decode(&kindsResp); err != nil {
		t.Fatalf("decode kinds response: %v", err)
	}
	for _, k := range kindsResp.Kinds {
		if k.Provider != "" && k.Provider != blueprint.NativeProvider {
			t.Fatalf("found undeclared provider kind %s from provider %s in kinds palette", k.Kind, k.Provider)
		}
	}
}
