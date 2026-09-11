// internal/api/cf152_failed_source_repair_test.go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// CF-152 — a declared source that failed to load must be repairable over the
// API: GET /api/providers lists it in a failed state with the reason, DELETE
// removes it from spec.sources, and POST with `replaces` swaps it for a ref
// that loads. Before, the failed source was invisible, DELETE answered 404
// and the only way out was hand-editing the YAML.

const cf152Failed = "ghcr.io/x/provider-aws-sqs:v9.9.9"
const cf152Replacement = "ghcr.io/x/provider-aws-sns:v2.7.0"

// cf152Server is a server whose blueprint on disk already declares
// cf152Failed — the only way the state can arise, since every API write
// that declares an unfetchable source is refused (CF-087). The fake fetch
// fails cf152Failed the way a registry does and serves cf152Replacement.
func cf152Server(t *testing.T) (http.Handler, Options) {
	t.Helper()
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		if ref == cf152Failed {
			return nil, fmt.Errorf("fetch %q: MANIFEST_UNKNOWN: manifest unknown", ref)
		}
		return &xpkg.Package{Ref: ref, Digest: "sha256:replacement", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})
	b := mustLoadBlueprint(t, o.Blueprint)
	b.Spec.Sources = append(b.Spec.Sources, blueprint.Source{Provider: cf152Failed})
	if err := writeBlueprintFile(o.Blueprint, b); err != nil {
		t.Fatalf("declare failed source on disk: %v", err)
	}
	return h, o
}

func cf152Providers(t *testing.T, rec interface{ Body() string }) []providerEntry {
	t.Helper()
	var resp struct{ Providers []providerEntry }
	if err := json.Unmarshal([]byte(rec.Body()), &resp); err != nil {
		t.Fatalf("providers response is not JSON: %v\n%s", err, rec.Body())
	}
	return resp.Providers
}

func cf152Entry(entries []providerEntry, ref string) *providerEntry {
	for i := range entries {
		if entries[i].Ref == ref {
			return &entries[i]
		}
	}
	return nil
}

func cf152Sources(t *testing.T, path string) []string {
	t.Helper()
	b := mustLoadBlueprint(t, path)
	out := make([]string, 0, len(b.Spec.Sources))
	for _, s := range b.Spec.Sources {
		out = append(out, s.Provider)
	}
	return out
}

type recBody struct{ s string }

func (r recBody) Body() string { return r.s }

func TestCF152FailedSourceIsListedWithReasonAndDeletable(t *testing.T) {
	h, o := cf152Server(t)

	gen := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen.Code != http.StatusBadRequest {
		t.Fatalf("generate with a failed source = %d, want 400:\n%s", gen.Code, gen.Body)
	}

	list := do(t, h, "GET", "/api/providers", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET /api/providers = %d:\n%s", list.Code, list.Body)
	}
	entries := cf152Providers(t, recBody{list.Body.String()})
	failed := cf152Entry(entries, cf152Failed)
	if failed == nil {
		t.Fatalf("GET /api/providers does not list the declared source that failed to load:\n%s", list.Body)
	}
	if !strings.Contains(failed.Error, "MANIFEST_UNKNOWN") {
		t.Errorf("failed entry error = %q, want the fetch reason", failed.Error)
	}
	if failed.Digest != "" || failed.Kinds != 0 {
		t.Errorf("failed entry = %+v, want no digest and no kinds", *failed)
	}
	if healthy := cf152Entry(entries, testProviderRef); healthy == nil || healthy.Error != "" {
		t.Errorf("healthy provider entry = %+v, want listed without error", healthy)
	}

	del := do(t, h, "DELETE", "/api/providers/"+url.PathEscape(cf152Failed), "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE failed source = %d, want 200:\n%s", del.Code, del.Body)
	}
	if cf152Entry(cf152Providers(t, recBody{del.Body.String()}), cf152Failed) != nil {
		t.Errorf("DELETE response still lists the removed source:\n%s", del.Body)
	}
	if srcs := cf152Sources(t, o.Blueprint); strings.Join(srcs, ",") != testProviderRef {
		t.Errorf("spec.sources after DELETE = %v, want only %s", srcs, testProviderRef)
	}

	gen = do(t, h, "POST", "/api/generate", `{"write":false}`)
	if gen.Code != http.StatusOK {
		t.Fatalf("generate after removing the failed source = %d, want 200:\n%s", gen.Code, gen.Body)
	}
	again := do(t, h, "DELETE", "/api/providers/"+url.PathEscape(cf152Failed), "")
	if again.Code != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404 (neither held nor declared)", again.Code)
	}
}

func TestCF152FailedSourceIsReplaceable(t *testing.T) {
	h, o := cf152Server(t)
	do(t, h, "GET", "/api/providers", "") // records the fetch failure

	body := fmt.Sprintf(`{"ref":%q,"replaces":%q}`, cf152Replacement, cf152Failed)
	rec := do(t, h, "POST", "/api/providers", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST replace = %d, want 200:\n%s", rec.Code, rec.Body)
	}
	if srcs := cf152Sources(t, o.Blueprint); strings.Join(srcs, ",") != testProviderRef+","+cf152Replacement {
		t.Errorf("spec.sources after replace = %v, want [%s %s]", srcs, testProviderRef, cf152Replacement)
	}

	list := do(t, h, "GET", "/api/providers", "")
	entries := cf152Providers(t, recBody{list.Body.String()})
	if cf152Entry(entries, cf152Failed) != nil {
		t.Errorf("replaced source still listed:\n%s", list.Body)
	}
	repl := cf152Entry(entries, cf152Replacement)
	if repl == nil || repl.Error != "" || repl.Kinds != 1 {
		t.Errorf("replacement entry = %+v, want served with its 1 kind and no error", repl)
	}
	if gen := do(t, h, "POST", "/api/generate", `{"write":false}`); gen.Code != http.StatusOK {
		t.Fatalf("generate after replace = %d, want 200:\n%s", gen.Code, gen.Body)
	}

	// Replacing something the document does not declare is a caller error.
	rec = do(t, h, "POST", "/api/providers", fmt.Sprintf(`{"ref":%q,"replaces":%q}`, cf152Replacement, "ghcr.io/x/nothing:v1"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("replace of an undeclared source = %d, want 404", rec.Code)
	}
}

// replaceProvider re-points every resource pinned to the old ref — a
// sources-only swap would fail validation ("provider is not declared") —
// and never lists a source twice when the new ref is already declared.
func TestCF152ReplaceProviderRepointsResourcesAndDedupes(t *testing.T) {
	b := &blueprint.Blueprint{}
	b.Spec.Sources = []blueprint.Source{{Provider: "a:v1"}, {Provider: "b:v1"}}
	b.Spec.Resources = []blueprint.Resource{{Name: "x", Provider: "a:v1"}, {Name: "y", Provider: "b:v1"}}

	replaceProvider(b, "a:v1", "a:v2")
	if got := fmt.Sprint(b.Spec.Sources); got != "[{a:v2 } {b:v1 }]" {
		t.Errorf("sources = %s, want a:v2 in a:v1's place", got)
	}
	if b.Spec.Resources[0].Provider != "a:v2" || b.Spec.Resources[1].Provider != "b:v1" {
		t.Errorf("resources = %+v, want only x re-pointed", b.Spec.Resources)
	}

	replaceProvider(b, "a:v2", "b:v1")
	if got := fmt.Sprint(b.Spec.Sources); got != "[{b:v1 }]" {
		t.Errorf("sources = %s, want the duplicate collapsed", got)
	}
	if b.Spec.Resources[0].Provider != "b:v1" {
		t.Errorf("resource x = %+v, want re-pointed to b:v1", b.Spec.Resources[0])
	}
}
