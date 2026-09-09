package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-088 — `cf serve` starts with a warning that an uncached declared source
// will "load on demand", but nothing loads it until a write happens: reads
// and generation on the freshly opened document fail with a CLI instruction.
func TestCF088DeclaredSourceLoadsOnDemand(t *testing.T) {
	store := cache.New(t.TempDir()) // empty: the declared provider is not cached
	idx, err := BuildIndex(store, nil, nil, "")
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	o := Options{
		Index:     idx,
		Store:     store,
		Blueprint: testBlueprintPath(t), // declares ghcr.io/x/provider-aws-sqs:v2.7.0
		OutDir:    t.TempDir(),
		Lock:      t.TempDir() + "/.cf.lock",
	}
	o.fetch = func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:ondemand", Docs: [][]byte{
			managedCRDDoc("sqs.aws.m.upbound.io", "Queue", "queues"),
		}}, nil
	}
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	kinds := do(t, h, "GET", "/api/kinds", "")
	if kinds.Code != http.StatusOK || !strings.Contains(kinds.Body.String(), `"Queue"`) {
		t.Errorf("GET /api/kinds = %d; the declared source's kinds are not served:\n%s", kinds.Code, kinds.Body)
	}
	gen := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen.Code != http.StatusOK {
		t.Errorf("POST /api/generate = %d: %s", gen.Code, gen.Body)
	}
	list := do(t, h, "GET", "/api/providers", "")
	if !strings.Contains(list.Body.String(), "provider-aws-sqs") {
		t.Errorf("GET /api/providers does not list the declared source once it has been loaded: %s", list.Body)
	}
}

// TestCF088DeclaredSourceFetchFailureAndNoRetryLoop verifies:
// 1. When a declared uncached source fails to fetch, generate returns 400 naming
// the source, the verbatim fetch reason, and the canvas SOURCES repair without prescribing CLI.
// 2. Fetch is attempted only once and not retried in a tight loop on subsequent requests.
func TestCF088DeclaredSourceFetchFailureAndNoRetryLoop(t *testing.T) {
	store := cache.New(t.TempDir())
	idx, err := BuildIndex(store, nil, nil, "")
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	o := Options{
		Index:     idx,
		Store:     store,
		Blueprint: testBlueprintPath(t),
		OutDir:    t.TempDir(),
		Lock:      t.TempDir() + "/.cf.lock",
	}
	fetchCalls := 0
	o.fetch = func(ref string) (*xpkg.Package, error) {
		fetchCalls++
		return nil, errors.New("connection refused: mock remote down")
	}
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First request: triggers fetch which fails
	gen := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen.Code != http.StatusBadRequest {
		t.Errorf("POST /api/generate = %d, want 400", gen.Code)
	}
	body := gen.Body.String()
	if !strings.Contains(body, "provider-aws-sqs") {
		t.Errorf("error %s does not name the provider", body)
	}
	if !strings.Contains(body, "connection refused: mock remote down") {
		t.Errorf("error %s does not contain the verbatim fetch error", body)
	}
	if !strings.Contains(body, "SOURCES tab") {
		t.Errorf("error %s does not mention the canvas SOURCES tab repair", body)
	}
	if strings.Contains(body, "cf provider add") {
		t.Errorf("error %s tells browser user to run CLI command", body)
	}

	if fetchCalls != 1 {
		t.Fatalf("fetch called %d times, want 1", fetchCalls)
	}

	// Second request: must not retry fetch in a tight loop
	gen2 := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen2.Code != http.StatusBadRequest {
		t.Errorf("second POST /api/generate = %d, want 400", gen2.Code)
	}
	if fetchCalls != 1 {
		t.Errorf("fetch called %d times after second request; want it NOT retried in tight loop", fetchCalls)
	}
}
