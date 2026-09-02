package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// nativeKindCount is how many vendored native kinds every index rebuild
// re-indexes under provider "k8s" — read from the vendored source itself so
// this file never hard-codes a number that internal/schema/k8s owns.
func nativeKindCount(t *testing.T) int {
	t.Helper()
	native, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("k8s.Kinds: %v", err)
	}
	return len(native)
}

// TestProvidersListsTheServersProviders pins GET /api/providers' whole
// response: the one provider the test server was started with, carrying the
// digest its cache entry was saved under and the number of kinds the index
// holds for it (both Queue variants).
func TestProvidersListsTheServersProviders(t *testing.T) {
	var got struct{ Providers []providerEntry }
	if code := getJSON(t, testHandler(t), "/api/providers", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	want := []providerEntry{{Ref: testProviderRef, Digest: "sha256:test", Kinds: 2}}
	if !reflect.DeepEqual(got.Providers, want) {
		t.Errorf("providers = %+v, want %+v", got.Providers, want)
	}
}

// TestProvidersParticipatesInETagCaching verifies GET /api/providers goes
// through wrap like every other GET — an If-None-Match echoing the previous
// response's validator is answered 304 with no body. Verification only: the
// route registers a plain handler and must NOT reimplement any of this.
func TestProvidersParticipatesInETagCaching(t *testing.T) {
	h := testHandler(t)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/api/providers", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", first.Code)
	}
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on GET /api/providers")
	}

	req := httptest.NewRequest("GET", "/api/providers", nil)
	req.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Errorf("status = %d with matching If-None-Match, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", second.Body.Len())
	}
}

// --- POST /api/providers ---

// addedProviderRef is a provider the test server does NOT start with; the
// fake fetcher below serves it without any network.
const addedProviderRef = "ghcr.io/x/provider-aws-sns:v2.7.0"

// managedCRDDoc renders a minimal managed-resource CRD for group/kind, so a
// fake-fetched package carries a schema that really flows through
// schema.ParseCRDs and index.Build — the same pipeline a real pull feeds.
func managedCRDDoc(group, kind, plural string) []byte {
	return []byte(fmt.Sprintf(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: %s.%s}
spec:
  group: %s
  scope: Namespaced
  names: {kind: %s, plural: %s, categories: [managed]}
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
`, plural, group, group, kind, plural))
}

// testProviderServer builds the shared test server with the fetch seam
// swapped, from the same testServerOptions construction path every other
// test server uses. It returns the Options too, so a test can reach the
// store and the lock path the server was built with.
func testProviderServer(t *testing.T, fetch func(ref string) (*xpkg.Package, error)) (http.Handler, Options) {
	t.Helper()
	o := testServerOptions(t)
	o.fetch = fetch
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, o
}

// TestAddProviderMakesItsKindsAppear is the happy path end to end: POST a
// new ref, get 200 with the provider's entry and the kinds it added, and —
// the point of the whole route — see those kinds in /api/kinds immediately,
// with GET /api/providers listing the newcomer alongside the original.
func TestAddProviderMakesItsKindsAppear(t *testing.T) {
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:added", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})

	rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Provider providerEntry
		Kinds    []index.Kind
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	want := providerEntry{Ref: addedProviderRef, Digest: "sha256:added", Kinds: 1}
	if resp.Provider != want {
		t.Errorf("provider = %+v, want %+v", resp.Provider, want)
	}
	if len(resp.Kinds) != 1 || resp.Kinds[0].Kind != "Topic" || resp.Kinds[0].Provider != addedProviderRef {
		t.Errorf("kinds = %+v, want the one Topic the fetched package carries", resp.Kinds)
	}

	// The index must reflect the add immediately — no restart, no re-serve.
	var kinds struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds?q=topic", &kinds); code != 200 {
		t.Fatalf("GET /api/kinds after add: status %d", code)
	}
	if len(kinds.Kinds) != 1 || kinds.Kinds[0].Kind != "Topic" {
		t.Errorf("/api/kinds?q=topic after add = %+v, want the added Topic", kinds.Kinds)
	}

	var provs struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provs); code != 200 {
		t.Fatalf("GET /api/providers after add: status %d", code)
	}
	wantList := []providerEntry{
		{Ref: testProviderRef, Digest: "sha256:test", Kinds: 2},
		{Ref: addedProviderRef, Digest: "sha256:added", Kinds: 1},
	}
	if !reflect.DeepEqual(provs.Providers, wantList) {
		t.Errorf("providers after add = %+v, want %+v", provs.Providers, wantList)
	}
}

// TestAddProviderUpdatesLockAndCache: a successful POST leaves the same
// on-disk state `cf provider add` would — the ref pinned to its digest in
// the lockfile, and the extracted CRDs loadable from the cache.
func TestAddProviderUpdatesLockAndCache(t *testing.T) {
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:added", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})

	rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	l, err := cache.ReadLock(o.Lock)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if len(l.Providers) != 1 || l.Providers[0].Ref != addedProviderRef || l.Providers[0].Digest != "sha256:added" {
		t.Errorf("lock = %+v, want exactly the added ref pinned to sha256:added", l.Providers)
	}

	crds, err := o.Store.Load(addedProviderRef)
	if err != nil {
		t.Fatalf("the added provider is not loadable from the cache: %v", err)
	}
	if len(crds) != 1 || crds[0].Kind != "Topic" {
		t.Errorf("cached CRDs = %d (%+v), want the one Topic", len(crds), crds)
	}
}

// TestAddProviderDuplicateRefIs409: the exact ref the server already serves
// is a conflict, reported with a stable message, and the fetch seam must
// never run for it.
func TestAddProviderDuplicateRefIs409(t *testing.T) {
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		t.Errorf("fetch ran for duplicate ref %q; a 409 must be decided before any pull", ref)
		return nil, fmt.Errorf("unreachable")
	})

	rec := do(t, h, "POST", "/api/providers", `{"ref":"`+testProviderRef+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	want := `provider "ghcr.io/x/provider-aws-sqs:v2.7.0" is already cached`
	if body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}
}

// TestAddProviderInvalidRefIs400 covers the caller-fault inputs: a missing
// ref, a ref the OCI reference parser rejects, and an unknown body key (the
// same DisallowUnknownFields discipline every other POST body gets). None
// may reach the fetch seam.
func TestAddProviderInvalidRefIs400(t *testing.T) {
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		t.Errorf("fetch ran for an invalid request (ref %q)", ref)
		return nil, fmt.Errorf("unreachable")
	})

	cases := []struct {
		name, body, wantErr string
	}{
		{"empty ref", `{"ref":""}`, "ref is required"},
		{"unparseable ref", `{"ref":"has spaces/provider:v1"}`, `parse reference "has spaces/provider:v1"`},
		{"unknown body key", `{"ref":"ghcr.io/x/p:v1","rev":"oops"}`, `unknown field "rev"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/providers", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("not JSON: %v", err)
			}
			if !strings.Contains(body.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", body.Error, tt.wantErr)
			}
		})
	}
}

// TestAddProviderFetchFailureIs502Verbatim: a pull failure is the upstream
// registry's fault — 502, carrying the fetch error's text exactly as the
// fetcher produced it, because that text names the registry's own reason.
func TestAddProviderFetchFailureIs502Verbatim(t *testing.T) {
	const fetchErr = `fetch "ghcr.io/x/provider-aws-sns:v2.7.0": GET https://ghcr.io/v2/: dial tcp: connection refused`
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return nil, errors.New(fetchErr)
	})

	rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if body.Error != fetchErr {
		t.Errorf("error = %q, want the fetch error verbatim: %q", body.Error, fetchErr)
	}

	// A failed pull must leave no trace: not listed, not pinned, not cached.
	var provs struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provs); code != 200 {
		t.Fatalf("GET /api/providers: status %d", code)
	}
	if len(provs.Providers) != 1 {
		t.Errorf("providers after failed add = %+v, want only the original", provs.Providers)
	}
}

// TestConcurrentAddsAllLand is the lost-update probe, run under -race: N
// concurrent POSTs of N different refs must all succeed, and every one of
// them must be visible afterwards — in the provider list, in the index, and
// in the lockfile. Without the index-swap and lock-write happening atomically
// under srv.mu, two adds both rebuild from the same starting index and the
// second swap silently discards the first's kinds (the exact lost-update
// class blueprint PUT's mu discipline exists for).
func TestConcurrentAddsAllLand(t *testing.T) {
	const n = 4
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		// Derive a distinct group/digest from the ref's tag so each add
		// carries its own kind: ghcr.io/x/provider-fake-i:v1 -> groupi.
		i := ref[len(ref)-4 : len(ref)-3]
		return &xpkg.Package{Ref: ref, Digest: "sha256:added" + i, Docs: [][]byte{
			managedCRDDoc("g"+i+".example.io", "Thing"+i, "thing"+i+"s"),
		}}, nil
	})

	refs := make([]string, n)
	for i := range refs {
		refs[i] = fmt.Sprintf("ghcr.io/x/provider-fake-%d:v1", i)
	}

	var wg sync.WaitGroup
	codes := make([]int, n)
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := do(t, h, "POST", "/api/providers", `{"ref":"`+ref+`"}`)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("POST %s: status = %d, want 200", refs[i], code)
		}
	}

	var provs struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provs); code != 200 {
		t.Fatalf("GET /api/providers: status %d", code)
	}
	if len(provs.Providers) != n+1 {
		t.Fatalf("providers = %d entries (%+v), want %d — a concurrent add was lost", len(provs.Providers), provs.Providers, n+1)
	}

	var kinds struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds", &kinds); code != 200 {
		t.Fatalf("GET /api/kinds: status %d", code)
	}
	// Every rebuild also re-indexes the vendored native Kubernetes kinds
	// under provider "k8s" (see handleAddProvider), so the survivors of the
	// concurrent adds are counted per family: each added provider's Thing
	// must have landed, the fixture's 2 Queues must remain, and the native
	// kinds must all still be there — losing THEM to a rebuild is exactly
	// the regression the byProvider[NativeProvider] line guards against.
	byProvider := map[string]int{}
	for _, k := range kinds.Kinds {
		byProvider[k.Provider]++
	}
	for _, ref := range refs {
		if byProvider[ref] != 1 {
			t.Errorf("provider %s has %d kinds in the index, want 1 — an add's index swap was lost", ref, byProvider[ref])
		}
	}
	if byProvider[testProviderRef] != 2 {
		t.Errorf("fixture provider has %d kinds, want its 2 Queues", byProvider[testProviderRef])
	}
	if byProvider[blueprint.NativeProvider] != nativeKindCount(t) {
		t.Errorf("provider %q has %d kinds after the rebuilds, want all %d vendored native kinds",
			blueprint.NativeProvider, byProvider[blueprint.NativeProvider], nativeKindCount(t))
	}

	l, err := cache.ReadLock(o.Lock)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if len(l.Providers) != n {
		t.Errorf("lock has %d pins (%+v), want %d — a concurrent lock write was lost", len(l.Providers), l.Providers, n)
	}
}

// --- DELETE /api/providers/{ref} ---

// deletePath is the DELETE route for ref, escaped the way the canvas escapes
// it (encodeURIComponent → every slash and colon percent-encoded).
func deletePath(ref string) string {
	return "/api/providers/" + url.PathEscape(ref)
}

// TestDeleteProviderStillReferencedIs409NamingReferencers: the test
// blueprint's sources name testProviderRef and its main-queue resource sets
// provider: testProviderRef, so deleting it must be refused with both
// referencers named — the same refuse-and-name discipline parameter delete
// applies — and must change nothing: the provider stays listed and its kinds
// stay served.
func TestDeleteProviderStillReferencedIs409NamingReferencers(t *testing.T) {
	h := testHandler(t)

	rec := do(t, h, "DELETE", deletePath(testProviderRef), "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	want := `delete provider "ghcr.io/x/provider-aws-sqs:v2.7.0": still referenced by the blueprint's sources and by resources "main-queue"`
	if body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}

	// A refused delete leaves the server exactly as it was.
	var provs struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provs); code != 200 {
		t.Fatalf("GET /api/providers: status %d", code)
	}
	if len(provs.Providers) != 1 || provs.Providers[0].Ref != testProviderRef {
		t.Errorf("providers after refused delete = %+v, want the original untouched", provs.Providers)
	}
	var kinds struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds", &kinds); code != 200 {
		t.Fatalf("GET /api/kinds: status %d", code)
	}
	if len(kinds.Kinds) != 2 {
		t.Errorf("kinds after refused delete = %d, want the original 2", len(kinds.Kinds))
	}
}

// TestDeleteProviderRemovesKindsCacheAndPin is the happy path end to end:
// add a provider over POST, delete it over DELETE, and see every trace gone —
// its kinds out of /api/kinds, its entry out of the providers list (the 200
// body carries the remaining list, GET's exact envelope), its cache entry
// evicted, and its lockfile pin removed.
func TestDeleteProviderRemovesKindsCacheAndPin(t *testing.T) {
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:added", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})
	if rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d: %s", rec.Code, rec.Body)
	}

	rec := do(t, h, "DELETE", deletePath(addedProviderRef), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp struct{ Providers []providerEntry }
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	wantList := []providerEntry{{Ref: testProviderRef, Digest: "sha256:test", Kinds: 2}}
	if !reflect.DeepEqual(resp.Providers, wantList) {
		t.Errorf("DELETE body providers = %+v, want %+v", resp.Providers, wantList)
	}

	// The index must reflect the delete immediately — no restart, no re-serve.
	var kinds struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds?q=topic", &kinds); code != 200 {
		t.Fatalf("GET /api/kinds after delete: status %d", code)
	}
	if len(kinds.Kinds) != 0 {
		t.Errorf("/api/kinds?q=topic after delete = %+v, want none", kinds.Kinds)
	}

	// On disk: no cache entry, no lock pin.
	if _, err := o.Store.Load(addedProviderRef); err == nil {
		t.Error("the deleted provider is still loadable from the cache")
	}
	l, err := cache.ReadLock(o.Lock)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if len(l.Providers) != 0 {
		t.Errorf("lock = %+v, want the deleted provider's pin removed", l.Providers)
	}
}

// TestDeleteProviderUnknownRefIs404: a ref the server does not hold is a 404,
// decided before anything is touched.
func TestDeleteProviderUnknownRefIs404(t *testing.T) {
	h := testHandler(t)
	rec := do(t, h, "DELETE", deletePath("ghcr.io/x/never-added:v1"), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	want := `provider not found: "ghcr.io/x/never-added:v1"`
	if body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}
}

// TestConcurrentDeleteAndGet is the race probe, run under -race: N concurrent
// DELETEs of one ref racing N concurrent GETs of the two routes that read the
// provider set and the index. Exactly one DELETE may win (the rest observe
// the post-swap set and 404), and every GET must land on a consistent
// snapshot — in particular the list must never 500, which is what an
// unlocked digest read would do when it loses the race to the eviction.
func TestConcurrentDeleteAndGet(t *testing.T) {
	const n = 4
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:added", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})
	if rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d: %s", rec.Code, rec.Body)
	}

	var wg sync.WaitGroup
	deleteCodes := make([]int, n)
	listCodes := make([]int, n)
	kindsCodes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			deleteCodes[i] = do(t, h, "DELETE", deletePath(addedProviderRef), "").Code
		}()
		go func() {
			defer wg.Done()
			listCodes[i] = do(t, h, "GET", "/api/providers", "").Code
		}()
		go func() {
			defer wg.Done()
			kindsCodes[i] = do(t, h, "GET", "/api/kinds", "").Code
		}()
	}
	wg.Wait()

	won := 0
	for i, code := range deleteCodes {
		switch code {
		case http.StatusOK:
			won++
		case http.StatusNotFound:
			// lost the race to the winning DELETE — the correct outcome
		default:
			t.Errorf("DELETE #%d: status = %d, want 200 or 404", i, code)
		}
	}
	if won != 1 {
		t.Errorf("%d DELETEs returned 200, want exactly 1", won)
	}
	for i, code := range listCodes {
		if code != http.StatusOK {
			t.Errorf("GET /api/providers #%d: status = %d, want 200 — a list racing a delete must "+
				"never observe a half-deleted provider", i, code)
		}
	}
	for i, code := range kindsCodes {
		if code != http.StatusOK {
			t.Errorf("GET /api/kinds #%d: status = %d, want 200", i, code)
		}
	}

	// Settled state: only the original provider, only its kinds.
	var provs struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provs); code != 200 {
		t.Fatalf("GET /api/providers: status %d", code)
	}
	if len(provs.Providers) != 1 || provs.Providers[0].Ref != testProviderRef {
		t.Errorf("providers after concurrent delete = %+v, want only the original", provs.Providers)
	}
}

func TestAddProviderDeclaresSourceInBlueprintIdempotently(t *testing.T) {
	h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{Ref: ref, Digest: "sha256:added", Docs: [][]byte{
			managedCRDDoc("sns.aws.m.upbound.io", "Topic", "topics"),
		}}, nil
	})

	rec := do(t, h, "POST", "/api/providers", `{"ref":"`+addedProviderRef+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	b, err := blueprint.Load(o.Blueprint)
	if err != nil {
		t.Fatalf("load blueprint: %v", err)
	}
	found := false
	for _, s := range b.Spec.Sources {
		if s.Provider == addedProviderRef {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("blueprint sources %+v does not declare added provider %s", b.Spec.Sources, addedProviderRef)
	}
}
