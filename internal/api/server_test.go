package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// testProviderRef is the xpkg ref testFixtureCRDs' two Queue CRDs are
// indexed under, and the provider the test blueprint's resource references.
const testProviderRef = "ghcr.io/x/provider-aws-sqs:v2.7.0"

// testFixtureCRDs returns the same two-Queue shape internal/index's own
// tests use: one namespaced Queue (sqs.aws.m.upbound.io) and one
// cluster-scoped Queue (sqs.aws.upbound.io), the pairing every upjet
// provider ships. internal/index's fixture helper is unexported to its own
// test package, so this is an independent copy rather than a shared import —
// but it is deliberately kept to the same two entries so a test failure here
// means the same thing it would there.
//
// The namespaced Queue's spec also carries providerConfigRef,
// managementPolicies and writeConnectionSecretToRef alongside forProvider —
// a realistic v2 envelope, not just forProvider with nothing around it. This
// is deliberate: GET /api/kinds/{apiVersion}/{kind}'s envelope is exactly
// spec minus forProvider/initProvider (see schema.CRD.Envelope), and a fixture
// with an empty envelope could never catch envelope content actually being
// wrong end-to-end over HTTP — only that it round-trips as an empty list.
// forProvider itself (region, tags) is untouched, so tests pinned to
// "2 forProvider fields" are unaffected.
func testFixtureCRDs(t *testing.T) map[string][]schema.CRD {
	t.Helper()
	docs := [][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  tags: {type: object, additionalProperties: {type: string}}
              providerConfigRef:
                type: object
                required: [name]
                properties:
                  kind: {type: string}
                  name: {type: string}
              managementPolicies:
                type: array
                items: {type: string}
              writeConnectionSecretToRef:
                type: object
                properties:
                  name: {type: string}
          status:
            properties:
              atProvider:
                properties:
                  arn: {type: string}
                  url: {type: string}
`), []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.upbound.io}
spec:
  group: sqs.aws.upbound.io
  scope: Cluster
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - {name: v1beta1, served: true, storage: true}
`)}
	parsed, err := schema.ParseCRDs(docs)
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}
	return map[string][]schema.CRD{testProviderRef: parsed}
}

// testIndex builds an *index.Index over testFixtureCRDs.
func testIndex(t *testing.T) *index.Index {
	t.Helper()
	idx, err := index.Build(testFixtureCRDs(t))
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	return idx
}

// testBlueprintYAML is a valid blueprint: a Namespaced XRD with the
// providerName parameter every Namespaced XRD requires (see
// internal/blueprint/load.go's Validate), plus maxMessageSize, and one
// resource — main-queue, kind Queue — whose maxMessageSize field is sourced
// from the maxMessageSize parameter. This mirrors internal/blueprint's own
// "valid" test fixture, adjusted to reference testProviderRef so it lines up
// with testIndex's fixture.
const testBlueprintYAML = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: ghcr.io/x/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/x/provider-aws-sqs:v2.7.0
      fields:
        maxMessageSize: {from: params.maxMessageSize}
        region: {value: "eu-west-1"}
`

// testBlueprintPath writes testBlueprintYAML into t.TempDir() and returns
// its path, after confirming (via blueprint.Load) that the fixture itself
// is actually valid — New does not parse the blueprint eagerly (see
// Options.validate), so nothing else would catch a broken fixture here.
func testBlueprintPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xqueue.cf.yaml")
	if err := os.WriteFile(path, []byte(testBlueprintYAML), 0o644); err != nil {
		t.Fatalf("write test blueprint: %v", err)
	}
	if _, err := blueprint.Load(path); err != nil {
		t.Fatalf("test blueprint fixture does not itself validate: %v", err)
	}
	return path
}

// testGenerateFixtureCRDs is testFixtureCRDs' namespaced Queue schema plus a
// maxMessageSize field, for seeding the cache.Store that Task 6's
// /api/generate route reads via Store.Load (mirroring cf gen — see
// cmd/cf/gen.go's run).
//
// This exists to reconcile two fixture requirements that would otherwise be
// mutually exclusive. internal/emit/composition.go's checkFieldPaths rejects
// any blueprint resource field the resolved CRD's own schema does not
// define, and testBlueprintYAML's main-queue resource sets maxMessageSize —
// but testFixtureCRDs' namespaced Queue schema deliberately has only region
// and tags, because two Task 5 tests (kinds_test.go's
// TestFieldsHonoursRequiredOnly and TestFieldsTotalCountsPreLimitSet) pin
// that exact CRD's forProvider field count at 2. Adding maxMessageSize to
// testFixtureCRDs itself would generate correctly but break those two
// existing tests, and kinds_test.go is outside the file list Task 6 is
// scoped to touch. Nor can testBlueprintYAML's resource field be pointed at
// "region" instead: TestRenameRewritesReferencesOnDisk looks up
// reloaded.Spec.Resources[0].Fields["maxMessageSize"] by that literal key,
// so both the parameter name and the field key are fixed by the given
// tests.
//
// The resolution: index.Index (what /api/kinds serves) and cache.Store (what
// /api/generate reads) are independent inputs to Options — nothing requires
// a provider's indexed schema and its cached schema to be the same object —
// so only the seed for the Store gets this field added; testIndex and every
// existing /api/kinds test are untouched. See the Task 6 report for the full
// reasoning; this was flagged rather than silently patched because it is
// exactly the "test's fixture/lookup mechanics forcing an unnatural
// implementation" case, not an ordinary prose/test conflict.
func testGenerateFixtureCRDs(t *testing.T) []schema.CRD {
	t.Helper()
	withMaxMessageSize, err := schema.ParseCRDs([][]byte{[]byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: queues.sqs.aws.m.upbound.io}
spec:
  group: sqs.aws.m.upbound.io
  scope: Namespaced
  names: {kind: Queue, plural: queues, categories: [managed]}
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              forProvider:
                required: [region]
                properties:
                  region: {type: string}
                  tags: {type: object, additionalProperties: {type: string}}
                  maxMessageSize: {type: integer}
              providerConfigRef:
                type: object
                required: [name]
                properties:
                  kind: {type: string}
                  name: {type: string}
              managementPolicies:
                type: array
                items: {type: string}
              writeConnectionSecretToRef:
                type: object
                properties:
                  name: {type: string}
          status:
            properties:
              atProvider:
                properties:
                  arn: {type: string}
                  url: {type: string}
`)})
	if err != nil {
		t.Fatalf("ParseCRDs: %v", err)
	}

	out := append([]schema.CRD(nil), withMaxMessageSize...)
	for _, c := range testFixtureCRDs(t)[testProviderRef] {
		if !c.Namespaced() {
			out = append(out, c) // the cluster-scoped variant, unmodified
		}
	}
	return out
}

// testHandlerWithPath builds the http.Handler New produces, wired to an
// index over the two-Queue fixture (testFixtureCRDs), a cache.Store rooted
// at a fresh t.TempDir() and seeded with testGenerateFixtureCRDs so
// /api/generate has cached schemas to render against, and the valid
// blueprint above written to another t.TempDir() — returning that
// blueprint's path too, since Task 6's tests need to confirm a mutation
// actually persisted (or, for a rejected edit, did not) by reloading the
// file directly, something the handler's return value alone can't expose.
// Tasks 4, 5 and 6 all reuse this helper — see the task brief — so its
// shape is a cross-task contract, not just a convenience for this file's own
// tests.
func testHandlerWithPath(t *testing.T) (http.Handler, string) {
	t.Helper()
	h, path, _, _ := testServerParts(t)
	return h, path
}

// testServerParts is testHandlerWithPath's full wiring, exposing the two
// Options fields its narrower signature drops: the seeded *cache.Store and
// the OutDir. TestGenerateProducesTheSameBytesAsTheEngine needs both — it
// calls emit.Generate directly with the very inputs the handler will use, so
// it has to reach the same store and the same output directory the server
// was built with. Everything else goes through testHandlerWithPath, whose
// signature stays the cross-task contract it has always been; this exists so
// there is still exactly ONE construction path for the test server rather
// than a second, independently-drifting copy of it.
func testServerParts(t *testing.T) (h http.Handler, blueprintPath string, store *cache.Store, outDir string) {
	t.Helper()
	o := testServerOptions(t)
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, o.Blueprint, o.Store, o.OutDir
}

// testServerOptions is the one place the shared test server's Options are
// assembled — testServerParts and providers_test.go's fetch-seam variant
// both build from it, so there is still exactly ONE construction path (per
// the comment above) even though the providers tests need to set the
// unexported fetch field before calling New.
func testServerOptions(t *testing.T) Options {
	t.Helper()
	store := cache.New(t.TempDir())
	if err := store.Save(&xpkg.Package{Ref: testProviderRef, Digest: "sha256:test"}, testGenerateFixtureCRDs(t)); err != nil {
		t.Fatalf("seed provider cache: %v", err)
	}
	return Options{
		Index:     testIndex(t),
		Store:     store,
		Blueprint: testBlueprintPath(t),
		OutDir:    t.TempDir(),
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
		Providers: []string{testProviderRef},
	}
}

// testHandler is testHandlerWithPath without the blueprint path, for the
// (Task 4 and 5) tests that only need a working handler. A thin wrapper
// rather than a parallel implementation, so the two helpers cannot drift out
// of sync with each other.
func testHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := testHandlerWithPath(t)
	return h
}

func TestHealthzIsPlainAndCheap(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownRouteIs404WithJSONError(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON so a browser client can parse the error", ct)
	}
}

func TestResponsesAreGzippedWhenAccepted(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("GET", "/api/kinds", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip — schemas compress about 18:1 and this is the "+
			"highest-leverage line of server code in the project", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil || len(body) == 0 {
		t.Fatalf("gzip body unreadable: %v", err)
	}
}

func TestResponsesAreNotGzippedWhenNotAccepted(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/kinds", nil))
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q with no Accept-Encoding; must not compress unasked", got)
	}
}

func TestETagIsStableAndReturns304(t *testing.T) {
	h := testHandler(t)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/api/kinds", nil))
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on /api/kinds")
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest("GET", "/api/kinds", nil))
	if got := second.Header().Get("ETag"); got != tag {
		t.Errorf("ETag changed between identical requests: %q then %q", tag, got)
	}
	req := httptest.NewRequest("GET", "/api/kinds", nil)
	req.Header.Set("If-None-Match", tag)
	third := httptest.NewRecorder()
	h.ServeHTTP(third, req)
	if third.Code != http.StatusNotModified {
		t.Errorf("status = %d with matching If-None-Match, want 304", third.Code)
	}
	if third.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", third.Body.Len())
	}
}

func TestMethodNotAllowedRatherThan404(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/kinds", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d for DELETE /api/kinds, want 405", rec.Code)
	}
}

// --- Additional coverage beyond the brief's verbatim tests ---
//
// These are not from the brief's Step 1 listing; they cover two other
// requirements the brief's Step 3 states explicitly (New must error rather
// than panic on incomplete Options, and a handler's own JSON error must
// survive normalization unchanged) but that the given test list does not
// exercise on its own.

// TestNewRejectsIncompleteOptions covers "New returns an error rather than
// panicking if Options is incomplete": each required field, zeroed one at a
// time, must produce an error instead of a handler built on a nil Index or
// empty path.
func TestNewRejectsIncompleteOptions(t *testing.T) {
	valid := func() Options {
		return Options{
			Index:     testIndex(t),
			Store:     cache.New(t.TempDir()),
			Blueprint: testBlueprintPath(t),
			OutDir:    t.TempDir(),
			Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
		}
	}

	cases := []struct {
		name   string
		mutate func(*Options)
	}{
		{"nil Index", func(o *Options) { o.Index = nil }},
		{"nil Store", func(o *Options) { o.Store = nil }},
		{"empty Blueprint path", func(o *Options) { o.Blueprint = "" }},
		{"empty OutDir", func(o *Options) { o.OutDir = "" }},
		{"empty Lock", func(o *Options) { o.Lock = "" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := valid()
			tt.mutate(&o)
			if _, err := New(o); err == nil {
				t.Errorf("New(%s) = nil error, want a complaint about the missing field", tt.name)
			}
		})
	}
}

// --- Fix round 2 ---
//
// Conditional-request handling (If-None-Match -> 304) used to run on every
// method and every status. The two tests below pin the two ways that was
// wrong; see wrap's comment in server.go for the full reasoning.

// TestConditionalRequestsDoNotApplyToMutations is the serious half: a POST
// carrying `If-None-Match: *` used to come back 304 with an empty body —
// AFTER the handler had already edited and persisted the blueprint. The
// caller was told nothing had changed while the file on disk said otherwise,
// which is exactly the silent-wrongness class this project exists to
// prevent.
func TestConditionalRequestsDoNotApplyToMutations(t *testing.T) {
	h, path := testHandlerWithPath(t)

	req := httptest.NewRequest("POST", "/api/blueprint/parameters",
		bytes.NewBufferString(`{"name":"location","parameter":{"type":"string"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a mutation is never a cache hit, whatever the request's "+
			"If-None-Match says: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() == 0 {
		t.Error("the edit's response body was suppressed as if it were a 304")
	}

	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["location"]; !ok {
		t.Error("the edit did not persist — this test's premise (the mutation ran) is broken")
	}
}

// TestErrorResponsesAreNeverAnsweredWith304 is the other half: two identical
// GETs of an unknown route produce the same 404 body and therefore the same
// ETag, so echoing the first response's validator back used to turn the
// second 404 into a 304 — telling the client its cached copy of a resource
// that does not exist is still fresh.
func TestErrorResponsesAreNeverAnsweredWith304(t *testing.T) {
	h := testHandler(t)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/api/nope", nil))
	if first.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", first.Code)
	}
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on the 404 — this test needs one to echo back")
	}

	// The previous response's own validator, and the wildcard, which matches
	// unconditionally.
	for _, inm := range []string{tag, "*"} {
		req := httptest.NewRequest("GET", "/api/nope", nil)
		req.Header.Set("If-None-Match", inm)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("If-None-Match: %s on a 404 route gave status %d, want 404 — an error is never "+
				"'your cached copy is still fresh'", inm, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("If-None-Match: %s: body = %q, want the JSON error shape", inm, rec.Body.String())
		}
	}
}

// TestVaryAcceptEncodingIsSetOnEveryResponse pins the header that keeps a
// shared cache from serving a gzipped body to a client that did not ask for
// one. It is set on the 200 path and, separately, on the 304 path (they are
// two different lines in wrap, so either can drift alone).
func TestVaryAcceptEncodingIsSetOnEveryResponse(t *testing.T) {
	h := testHandler(t)

	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, httptest.NewRequest("GET", "/api/kinds", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", ok.Code)
	}
	if got := ok.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("200: Vary = %q, want Accept-Encoding — the same URL is served gzipped or plain "+
			"depending on the request, and a cache that does not know that will serve the wrong one", got)
	}

	req := httptest.NewRequest("GET", "/api/kinds", nil)
	req.Header.Set("If-None-Match", ok.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	h.ServeHTTP(notModified, req)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", notModified.Code)
	}
	if got := notModified.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("304: Vary = %q, want Accept-Encoding", got)
	}
}

// TestHandlerJSONErrorsPassThroughUnnormalized checks the other half of
// error normalization: a handler that already wrote the project's
// {"error": "..."} shape with Content-Type: application/json (via
// writeJSONError) must not be rewritten by wrap's normalization step, which
// exists only to catch ServeMux's own plain-text 404/405 responses.
func TestHandlerJSONErrorsPassThroughUnnormalized(t *testing.T) {
	h := wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusBadRequest, "specific handler-authored message")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/whatever", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "specific handler-authored message") {
		t.Errorf("body = %q, want the handler's own message preserved verbatim", rec.Body.String())
	}
}

func TestRebuildIndexLockedReturnsErrorOnMissingProvider(t *testing.T) {
	tempDir := t.TempDir()
	store := cache.New(filepath.Join(tempDir, "cache"))
	srv := &server{
		Options: Options{
			Store:     store,
			Providers: []string{"example.org/missing:v1"},
		},
	}
	err := srv.rebuildIndexLocked()
	if err == nil {
		t.Fatalf("expected rebuildIndexLocked to fail for missing provider, got nil")
	}
	if !strings.Contains(err.Error(), "load provider schemas") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRebuildIndexLockedReturnsErrorOnInvalidBlueprintYAML(t *testing.T) {
	tempDir := t.TempDir()
	bpPath := filepath.Join(tempDir, "blueprint.yaml")
	_ = os.WriteFile(bpPath, []byte("invalid: yaml: [broken"), 0o644)
	store := cache.New(filepath.Join(tempDir, "cache"))

	srv := &server{
		Options: Options{
			Store:     store,
			Blueprint: bpPath,
		},
	}
	err := srv.rebuildIndexLocked()
	if err == nil {
		t.Fatalf("expected rebuildIndexLocked to fail for invalid blueprint yaml, got nil")
	}
	if !strings.Contains(err.Error(), "parse blueprint") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSyncBlueprintSourcesLockedPinsCachedProvider(t *testing.T) {
	tempDir := t.TempDir()
	store := cache.New(filepath.Join(tempDir, "cache"))
	lockPath := filepath.Join(tempDir, ".cf.lock")

	// Pre-populate store with a provider
	pkg := &xpkg.Package{
		Ref:    "example.org/cached-provider:v1",
		Digest: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	crds := []schema.CRD{}
	if err := store.Save(pkg, crds); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
		Spec: blueprint.Spec{
			Sources: []blueprint.Source{
				{Provider: "example.org/cached-provider:v1"},
			},
		},
	}

	srv := &server{
		Options: Options{
			Store: store,
			Lock:  lockPath,
		},
	}

	// syncBlueprintSourcesLocked should pin the cached provider to lockfile
	err := srv.syncBlueprintSourcesLocked(context.Background(), bp)
	if err != nil {
		t.Fatalf("syncBlueprintSourcesLocked failed: %v", err)
	}

	l, err := cache.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock failed: %v", err)
	}
	found := false
	for _, p := range l.Providers {
		if p.Ref == "example.org/cached-provider:v1" && p.Digest == pkg.Digest {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("lock entry not found or mismatch in: %+v", l.Providers)
	}
}

func TestAPIVersionReturnsVersionAndEngines(t *testing.T) {
	tmp := t.TempDir()
	bpPath := filepath.Join(tmp, "blueprint.yaml")
	if err := os.WriteFile(bpPath, []byte("apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(tmp, ".cf.lock")
	if err := os.WriteFile(lockPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := index.Build(map[string][]schema.CRD{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{
		Version:   "v1.2.3-test",
		Index:     idx,
		Store:     cache.New(tmp),
		Blueprint: bpPath,
		OutDir:    tmp,
		Lock:      lockPath,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var res struct {
		Version string   `json:"version"`
		Engines []string `json:"engines"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if res.Version != "v1.2.3-test" {
		t.Errorf("Version = %q, want %q", res.Version, "v1.2.3-test")
	}

	wantEngines := []string{"go-templating", "kcl", "python"}
	if len(res.Engines) != len(wantEngines) {
		t.Fatalf("Engines length = %d, want %d (%v)", len(res.Engines), len(wantEngines), res.Engines)
	}
	for i, e := range wantEngines {
		if res.Engines[i] != e {
			t.Errorf("Engines[%d] = %q, want %q", i, res.Engines[i], e)
		}
	}
}
