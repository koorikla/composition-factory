package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/catalogue"
)

// TestCatalogueServesTheEmbeddedList checks the route against the real
// embedded catalogue (there is no seam to swap it — see catalogue.go's doc
// comment on why it is a package-level var, not something Options carries):
// every entry is present, and each has the five documented fields.
func TestCatalogueServesTheEmbeddedList(t *testing.T) {
	want, err := catalogue.Load()
	if err != nil {
		t.Fatalf("catalogue.Load: %v", err)
	}

	var got struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, testHandler(t), "/api/catalogue", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Providers) != len(want) {
		t.Fatalf("got %d providers, want %d (the whole embedded catalogue)", len(got.Providers), len(want))
	}
	for i, p := range got.Providers {
		if p.Name == "" {
			t.Errorf("entry %d has an empty name", i)
		}
		if p != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, p, want[i])
		}
	}
}

// TestCatalogueQFiltersByNameOrDescription pins the ?q= contract: a
// case-insensitive substring match against name OR description, same style
// as GET /api/kinds.
func TestCatalogueQFiltersByNameOrDescription(t *testing.T) {
	h := testHandler(t)

	var byName struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, h, "/api/catalogue?q=function-go-templating", &byName); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(byName.Providers) != 1 || byName.Providers[0].Name != "function-go-templating" {
		t.Fatalf("q=function-go-templating = %+v, want exactly the one matching entry", byName.Providers)
	}

	// Every entry with "aws" in its name, description, or neither must be
	// handled consistently: this just checks the query returns a strict
	// subset of the full list and every returned entry actually matches.
	all, err := catalogue.Load()
	if err != nil {
		t.Fatalf("catalogue.Load: %v", err)
	}
	var byAws struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, h, "/api/catalogue?q=aws", &byAws); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(byAws.Providers) == 0 || len(byAws.Providers) >= len(all) {
		t.Fatalf("q=aws returned %d of %d entries, want a non-empty strict subset", len(byAws.Providers), len(all))
	}
	for _, p := range byAws.Providers {
		if !strings.Contains(strings.ToLower(p.Name), "aws") && !strings.Contains(strings.ToLower(p.Description), "aws") {
			t.Errorf("q=aws matched %+v, which contains \"aws\" in neither name nor description", p)
		}
	}

	var none struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, h, "/api/catalogue?q=this-matches-nothing-at-all", &none); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(none.Providers) != 0 {
		t.Errorf("q=this-matches-nothing-at-all = %+v, want an empty list", none.Providers)
	}
}

// TestCatalogueEmptyQMatchesEverything checks the "no q" fallback matches
// GET /api/kinds' own: an absent query string returns the whole list, not
// an empty one.
func TestCatalogueEmptyQMatchesEverything(t *testing.T) {
	want, err := catalogue.Load()
	if err != nil {
		t.Fatalf("catalogue.Load: %v", err)
	}
	var got struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, testHandler(t), "/api/catalogue", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Providers) != len(want) {
		t.Errorf("got %d providers with no q, want all %d", len(got.Providers), len(want))
	}
}

// TestCatalogueParticipatesInETagCaching verifies GET /api/catalogue goes
// through wrap like every other GET — an If-None-Match echoing the previous
// response's validator is answered 304 with no body.
func TestCatalogueParticipatesInETagCaching(t *testing.T) {
	h := testHandler(t)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest("GET", "/api/catalogue", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", first.Code)
	}
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag on GET /api/catalogue")
	}

	req := httptest.NewRequest("GET", "/api/catalogue", nil)
	req.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Errorf("status = %d with matching If-None-Match, want 304", second.Code)
	}
}

// TestCatalogueTypeFilter verifies filtering by type=function and type=provider.
func TestCatalogueTypeFilter(t *testing.T) {
	h := testHandler(t)

	var fns struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, h, "/api/catalogue?type=function", &fns); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(fns.Providers) == 0 {
		t.Fatal("type=function returned 0 results")
	}
	for _, p := range fns.Providers {
		if !strings.HasPrefix(p.Name, "function-") {
			t.Errorf("type=function matched non-function: %s", p.Name)
		}
	}

	var provs struct {
		Providers []catalogue.Provider `json:"providers"`
	}
	if code := getJSON(t, h, "/api/catalogue?type=provider", &provs); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(provs.Providers) == 0 {
		t.Fatal("type=provider returned 0 results")
	}
	for _, p := range provs.Providers {
		if strings.HasPrefix(p.Name, "function-") {
			t.Errorf("type=provider matched function: %s", p.Name)
		}
	}
}
