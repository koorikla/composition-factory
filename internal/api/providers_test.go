package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

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
