package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-129 — After one failed source fetch, subsequent writes must not silently
// succeed without reporting that the declared source is still unloaded, and
// must retry fetching when requested.
func TestCF129SubsequentWriteReportsUnloadedSourceAndRetries(t *testing.T) {
	const missing = "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"
	var fetchAttempts int32

	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		atomic.AddInt32(&fetchAttempts, 1)
		return nil, fmt.Errorf("registry unreachable for %s", ref)
	})

	current := mustLoadBlueprint(t, o.Blueprint)
	updated := *current
	updated.Spec.Sources = append(append([]blueprint.Source(nil), current.Spec.Sources...), blueprint.Source{Provider: missing})
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// First write fails as expected
	rec1 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec1.Code != http.StatusBadRequest || !strings.Contains(rec1.Body.String(), "registry unreachable") {
		t.Fatalf("first PUT expected 400 with fetch error, got %d:\n%s", rec1.Code, rec1.Body.String())
	}

	// Second write with the same document must also report the fetch error, not 200 OK
	rec2 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("second PUT expected 400 reporting unloaded source, got %d:\n%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), missing) || !strings.Contains(rec2.Body.String(), "registry unreachable") {
		t.Fatalf("second PUT body missing source failure detail:\n%s", rec2.Body.String())
	}

	// Verify that a retry was actually attempted on the second write
	if attempts := atomic.LoadInt32(&fetchAttempts); attempts < 2 {
		t.Fatalf("expected at least 2 fetch attempts across 2 PUTs, got %d", attempts)
	}
}

func TestCF129RetrySuccessClearsFailedSourceAndServesProvider(t *testing.T) {
	const addedRef = "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"
	var fail int32 = 1

	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		if atomic.LoadInt32(&fail) == 1 {
			return nil, fmt.Errorf("temporary failure for %s", ref)
		}
		return &xpkg.Package{Ref: ref, Digest: "sha256:added-sns", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})

	current := mustLoadBlueprint(t, o.Blueprint)
	updated := *current
	updated.Spec.Sources = append(append([]blueprint.Source(nil), current.Spec.Sources...), blueprint.Source{Provider: addedRef})
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// First PUT fails due to temporary failure
	rec1 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on initial failure, got %d:\n%s", rec1.Code, rec1.Body.String())
	}

	// Network recovers
	atomic.StoreInt32(&fail, 0)

	// Second PUT retries and should succeed with 200 OK
	rec2 := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on retry recovery, got %d:\n%s", rec2.Code, rec2.Body.String())
	}

	// GET /api/providers should now list the newly fetched provider
	rec3 := do(t, h, "GET", "/api/providers", "")
	if rec3.Code != http.StatusOK || !strings.Contains(rec3.Body.String(), addedRef) {
		t.Fatalf("expected GET /api/providers to contain %s, got %d:\n%s", addedRef, rec3.Code, rec3.Body.String())
	}
}
