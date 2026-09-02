package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
)

func getJSON(t *testing.T, h http.Handler, path string, into any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code == http.StatusOK && into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: body is not JSON: %v\n%s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestListKindsReturnsTheIndex(t *testing.T) {
	var got struct{ Kinds []index.Kind }
	if code := getJSON(t, testHandler(t), "/api/kinds", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Kinds) != 2 {
		t.Fatalf("got %d kinds, want 2 (namespaced and cluster-scoped Queue)", len(got.Kinds))
	}
	for _, k := range got.Kinds {
		if k.APIVersion == "" || k.Kind == "" {
			t.Errorf("kind is missing identity fields: %+v", k)
		}
	}
}

func TestListKindsSearchAndLimit(t *testing.T) {
	h := testHandler(t)
	var got struct{ Kinds []index.Kind }
	getJSON(t, h, "/api/kinds?q=sqs.aws.m", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("q=sqs.aws.m returned %d, want 1", len(got.Kinds))
	}
	got.Kinds = nil
	getJSON(t, h, "/api/kinds?q=queue&limit=1", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("limit=1 returned %d", len(got.Kinds))
	}
}

func TestGetKindReturnsIdentityAndEnvelope(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	var got struct {
		Kind     index.Kind
		Envelope []index.Field
		Status   []index.Field
	}
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Queue", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if got.Kind.Kind != "Queue" || !got.Kind.Namespaced {
		t.Errorf("kind = %+v, want the namespaced Queue", got.Kind)
	}

	// The envelope must be the real, computed v2 shape (see
	// schema.CRD.Envelope and testFixtureCRDs' doc comment), not a
	// hand-written guess: it should carry the fields the fixture CRD's own
	// schema actually has (providerConfigRef.kind) and must not carry
	// legacy v1-only fields the API server prunes on v2 namespaced
	// resources (deletionPolicy) — a hand-written envelope is exactly the
	// bug that guessed the wrong one before this route existed.
	var paths []string
	hasProviderConfigRefKind := false
	hasDeletionPolicy := false
	for _, f := range got.Envelope {
		paths = append(paths, f.Path)
		if f.Path == "providerConfigRef.kind" {
			hasProviderConfigRefKind = true
		}
		if f.Path == "deletionPolicy" {
			hasDeletionPolicy = true
		}
	}
	if !hasProviderConfigRefKind {
		t.Errorf("envelope = %v, want it to contain providerConfigRef.kind (from the fixture's real schema)", paths)
	}
	if hasDeletionPolicy {
		t.Errorf("envelope = %v, must not contain deletionPolicy (a legacy v1 field this fixture's schema does not have)", paths)
	}

	// Status fields must also be returned from the real CRD status schema.
	var statusPaths []string
	for _, f := range got.Status {
		statusPaths = append(statusPaths, f.Path)
	}
	hasURL := false
	for _, p := range statusPaths {
		if p == "atProvider.url" {
			hasURL = true
		}
	}
	if !hasURL {
		t.Errorf("status = %v, want it to contain atProvider.url", statusPaths)
	}
}

func TestGetUnknownKindIs404WithJSON(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Nonexistent", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestFieldsHonoursRequiredOnly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	var all, req struct {
		Fields []index.Field
		Total  int
	}
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields", &all)
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields?required_only=true", &req)
	if len(all.Fields) != 2 {
		t.Errorf("all fields = %d, want 2 (region, tags)", len(all.Fields))
	}
	if len(req.Fields) != 1 || req.Fields[0].Path != "region" {
		t.Errorf("required_only = %+v, want just region", req.Fields)
	}
	if req.Total != 1 {
		t.Errorf("total = %d, want it to count the returned set", req.Total)
	}
}

// The managed Queue's requireds are all ROOT-level (forProvider requires
// region and nothing deeper), so effective requiredness changes NOTHING for
// it: requiredChain == required on every row, required_only still returns
// exactly region, and there are no required branches. This is the
// no-regression half of the effective-requiredness change — the native
// Deployment half lives in native_required_test.go.
func TestQueueChainEqualsRawAndHasNoRequiredBranches(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	var all fieldsResponse
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields", &all); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(all.Fields) == 0 {
		t.Fatal("Queue served no fields")
	}
	for _, f := range all.Fields {
		if f.RequiredChain != f.Required {
			t.Errorf("%s: requiredChain=%v != required=%v — the Queue's requireds are root-level, "+
				"so the chain must equal the raw flag on every row", f.Path, f.RequiredChain, f.Required)
		}
	}
	if len(all.RequiredBranches) != 0 {
		t.Errorf("requiredBranches = %+v, want none for the Queue", all.RequiredBranches)
	}
}

func TestFieldsRejectsBadQueryParamsLoudly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	for _, q := range []string{"?max_depth=abc", "?limit=-x", "?required_only=maybe"} {
		if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s -> status %d, want 400; silently ignoring a malformed filter would "+
				"return the wrong field set with no signal", q, code)
		}
	}
}

// TestListKindsRejectsBadLimitLoudly is the /api/kinds half of the test
// above. Both routes parse limit through the same parseIntParam, but only
// /fields' use of it was covered — so a change that made /api/kinds swallow a
// malformed limit (silently returning every kind for limit=abc, when the
// caller asked for a bounded list) would have kept the suite green. Fix round
// 2, minor finding.
func TestListKindsRejectsBadLimitLoudly(t *testing.T) {
	h := testHandler(t)
	for _, q := range []string{"?limit=abc", "?limit=-x", "?q=queue&limit=1.5"} {
		if code := getJSON(t, h, "/api/kinds"+q, nil); code != http.StatusBadRequest {
			t.Errorf("/api/kinds%s -> status %d, want 400; a malformed limit that is silently ignored "+
				"returns a different result set than the caller asked for, with no signal", q, code)
		}
	}
}

// --- Fix round 1: coverage for review findings 1 and 2 ---
//
// Neither of these was in the brief's given test list; both close gaps the
// first review pass identified as real bugs the given tests couldn't catch.

// TestFieldsTotalCountsPreLimitSet pins finding 1: total must report the
// size of the filtered field set BEFORE limit truncates it, not after — the
// whole point of total is letting a caller detect that limit cut the
// response short. Before the fix, total was computed as len(fields) AFTER
// index.Fields had already applied Limit internally, making it
// tautologically equal to len(fields) and useless for detecting truncation.
func TestFieldsTotalCountsPreLimitSet(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)

	var unlimited struct {
		Fields []index.Field
		Total  int
	}
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields", &unlimited); code != 200 {
		t.Fatalf("status %d", code)
	}
	if unlimited.Total != len(unlimited.Fields) {
		t.Errorf("no limit applied: total = %d, len(fields) = %d, want them equal", unlimited.Total, len(unlimited.Fields))
	}
	if unlimited.Total != 2 {
		t.Fatalf("total = %d, want 2 (region, tags) so the limit=1 case below actually truncates something", unlimited.Total)
	}

	var limited struct {
		Fields []index.Field
		Total  int
	}
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields?limit=1", &limited); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(limited.Fields) != 1 {
		t.Errorf("limit=1: len(fields) = %d, want 1", len(limited.Fields))
	}
	if limited.Total != 2 {
		t.Errorf("limit=1: total = %d, want 2 — total must count the pre-limit filtered set so a client can "+
			"tell limit truncated the response (2 fields exist, only 1 came back)", limited.Total)
	}
}

// TestGetKindResolvesConsistentlyAcrossProviderCollision pins finding 2: two
// different providers shipping the exact same apiVersion+kind is a genuine
// collision (see index.Lookup's doc comment) that index.Build resolves via
// "last write wins" in sorted-provider order. GET
// /api/kinds/{apiVersion}/{kind} must report a Kind and fields/envelope that
// describe the SAME provider's resource. Before the fix, the Kind came from
// a scan of All() (first match = lexicographically smallest provider) while
// the CRD came from Lookup (last write = lexicographically greatest
// provider) — two independently-resolved halves of the same collision that
// could silently disagree.
//
// This builds two providers, "aaa" and "zzz", each shipping
// collision.example.io/v1 Widget with a deliberately distinguishing
// forProvider field (fromA vs fromZ) so the test can tell which provider's
// CRD actually got served, and checks the response's kind.provider names
// that same provider.
func TestGetKindResolvesConsistentlyAcrossProviderCollision(t *testing.T) {
	widgetCRD := func(field string) []byte {
		return []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.collision.example.io}
spec:
  group: collision.example.io
  scope: Namespaced
  names: {kind: Widget, plural: widgets, categories: [managed]}
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [` + field + `]
                properties:
                  ` + field + `: {type: string}
`)
	}

	const providerA = "ghcr.io/x/provider-aaa:v1.0.0" // sorts first
	const providerZ = "ghcr.io/x/provider-zzz:v1.0.0" // sorts last, processed last, wins the collision

	crdA, err := schema.ParseCRDs([][]byte{widgetCRD("fromA")})
	if err != nil {
		t.Fatalf("ParseCRDs (providerA): %v", err)
	}
	crdZ, err := schema.ParseCRDs([][]byte{widgetCRD("fromZ")})
	if err != nil {
		t.Fatalf("ParseCRDs (providerZ): %v", err)
	}

	idx, err := index.Build(map[string][]schema.CRD{
		providerA: crdA,
		providerZ: crdZ,
	})
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}

	h, err := New(Options{
		Index:     idx,
		Store:     cache.New(t.TempDir()),
		Blueprint: testBlueprintPath(t),
		OutDir:    t.TempDir(),
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	esc := url.PathEscape("collision.example.io/v1")

	var fieldsResp struct {
		Fields []index.Field
		Total  int
	}
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Widget/fields", &fieldsResp); code != 200 {
		t.Fatalf("fields: status %d", code)
	}
	if len(fieldsResp.Fields) != 1 || fieldsResp.Fields[0].Path != "fromZ" {
		t.Fatalf("fields = %+v, want just fromZ (providerZ is the documented tie-break winner)", fieldsResp.Fields)
	}

	var kindResp struct {
		Kind     index.Kind
		Envelope []index.Field
	}
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Widget", &kindResp); code != 200 {
		t.Fatalf("kind: status %d", code)
	}
	if kindResp.Kind.Provider != providerZ {
		t.Errorf("kind.provider = %q, want %q — it must match the provider whose fields/envelope are actually "+
			"served (fromZ), not an independently-resolved Kind from the other provider in the collision",
			kindResp.Kind.Provider, providerZ)
	}
}

func TestKindsQueryStrictnessAndSearchAlias(t *testing.T) {
	h := testHandler(t)

	// Search alias
	var got struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds?search=sqs.aws.m", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Kinds) != 1 {
		t.Errorf("search=sqs.aws.m returned %d, want 1", len(got.Kinds))
	}

	// Unknown query parameter on /api/kinds
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds?unknown_param=foo", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 Bad Request on unknown param", rec.Code)
	}

	// Unknown query parameter on /api/kinds/.../fields
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds/"+esc+"/Queue/fields?invalid_field_query=1", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 Bad Request on unknown field query param", rec.Code)
	}

	// Status query param on /api/kinds/.../fields
	var statusResp struct {
		Fields []index.Field
		Total  int
	}
	if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields?status=true", &statusResp); code != 200 {
		t.Fatalf("fields status query: code %d", code)
	}
	if len(statusResp.Fields) == 0 {
		t.Fatal("expected status fields to be returned")
	}
	foundURL := false
	for _, f := range statusResp.Fields {
		if f.Path == "atProvider.url" {
			foundURL = true
			break
		}
	}
	if !foundURL {
		t.Errorf("status fields %+v missing atProvider.url", statusResp.Fields)
	}
}
