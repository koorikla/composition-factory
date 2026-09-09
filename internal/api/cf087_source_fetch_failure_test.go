// internal/api/cf087_source_fetch_failure_test.go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-087 — a document write that declares a source the server cannot fetch
// must tell the caller so; today the failure goes to stderr only and the
// write reports success.
func TestCF087WriteReportsSourceFetchFailure(t *testing.T) {
	const missing = "ghcr.io/crossplane-contrib/provider-aws-sns:v2.7.0"
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return nil, fmt.Errorf("registry unreachable for %s", ref)
	})

	current := mustLoadBlueprint(t, o.Blueprint)
	updated := *current
	updated.Spec.Sources = append(append([]blueprint.Source(nil), current.Spec.Sources...), blueprint.Source{Provider: missing})
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	got := rec.Body.String()
	if !strings.Contains(got, missing) || !strings.Contains(got, "registry unreachable") {
		t.Fatalf("PUT answered %d without naming the source that could not be fetched:\n%s", rec.Code, got)
	}

	list := do(t, h, "GET", "/api/providers", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), missing) {
		t.Fatalf("GET /api/providers = %d %s; the unfetched source must not be listed as served", list.Code, list.Body)
	}
}
