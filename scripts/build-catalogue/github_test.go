package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// githubPage builds one page's worth of raw GitHub repo-list JSON.
func githubPage(names ...string) []byte {
	type ghRepo struct {
		Name string `json:"name"`
		Fork bool   `json:"fork"`
	}
	var repos []ghRepo
	for _, n := range names {
		repos = append(repos, ghRepo{Name: n})
	}
	b, _ := json.Marshal(repos)
	return b
}

// TestFetchGitHubReposPaginatesUntilAShortPage verifies pagination stops the
// same way the live API behaved when this catalogue was first built:
// crossplane-contrib returned exactly 100 repos on page 1 and 40 on page 2,
// and a client has to keep requesting pages until one comes back short of
// githubPerPage.
func TestFetchGitHubReposPaginatesUntilAShortPage(t *testing.T) {
	names1 := make([]string, githubPerPage)
	for i := range names1 {
		names1[i] = fmt.Sprintf("provider-%d", i)
	}
	names2 := []string{"function-x", "function-y"}

	var pagesServed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pagesServed = append(pagesServed, page)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			w.Write(githubPage(names1...))
		case "2":
			w.Write(githubPage(names2...))
		default:
			t.Errorf("unexpected page requested: %s", page)
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	got, err := fetchGitHubRepos(context.Background(), srv.Client(), srv.URL, "crossplane-contrib")
	if err != nil {
		t.Fatalf("fetchGitHubRepos: %v", err)
	}
	if len(got) != len(names1)+len(names2) {
		t.Fatalf("got %d repos, want %d", len(got), len(names1)+len(names2))
	}
	if len(pagesServed) != 2 {
		t.Fatalf("requested %d pages (%v), want exactly 2 — a page shorter than githubPerPage must stop pagination", len(pagesServed), pagesServed)
	}
}

// TestFetchGitHubReposRateLimitIsGraceful pins the "rate-limit-graceful"
// requirement: a 403 with X-RateLimit-Remaining: 0 on page 2 returns the
// repos page 1 already collected, plus a non-nil error describing the
// cutoff — not a total failure that discards page 1's results.
func TestFetchGitHubReposRateLimitIsGraceful(t *testing.T) {
	// A page 1 exactly githubPerPage long, so fetchGitHubRepos' pagination
	// loop requests page 2 — which this server rate-limits.
	fullPage := make([]string, githubPerPage)
	for i := range fullPage {
		fullPage[i] = "provider-filler"
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(githubPage(fullPage...))
			return
		}
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"rate limit exceeded"}`))
	}))
	defer srv.Close()

	got, err := fetchGitHubRepos(context.Background(), srv.Client(), srv.URL, "crossplane-contrib")
	if err == nil {
		t.Fatal("fetchGitHubRepos with an exhausted rate limit = nil error, want one describing the cutoff")
	}
	if len(got) != githubPerPage {
		t.Fatalf("got %d repos after the rate-limited page, want page 1's %d preserved", len(got), githubPerPage)
	}
}

func TestFilterCatalogueReposKeepsOnlyProviderAndFunctionNonForks(t *testing.T) {
	in := []githubRepo{
		{Name: "provider-aws"},
		{Name: "function-go-templating"},
		{Name: "provider-checkly", Fork: true},
		{Name: "crossview"}, // neither prefix
		{Name: "function-cue-fork", Fork: true},
	}
	got := filterCatalogueRepos(in)
	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	want := []string{"provider-aws", "function-go-templating"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("filterCatalogueRepos names = %v, want %v", names, want)
	}
}

func TestToReposHandlesMissingLicense(t *testing.T) {
	in := []githubRepo{
		{Name: "provider-aws", Description: "d", HTMLURL: "u", License: &struct {
			SPDXID string `json:"spdx_id"`
		}{SPDXID: "Apache-2.0"}},
		{Name: "provider-no-license", Description: "d2", HTMLURL: "u2", License: nil},
	}
	got := toRepos("crossplane-contrib", in)
	want := []repo{
		{Name: "provider-aws", Owner: "crossplane-contrib", Description: "d", SourceURL: "u", LicenseSPDX: "Apache-2.0"},
		{Name: "provider-no-license", Owner: "crossplane-contrib", Description: "d2", SourceURL: "u2", LicenseSPDX: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("toRepos = %+v, want %+v", got, want)
	}
}
