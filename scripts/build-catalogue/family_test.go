package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/koorikla/compositionfactory/catalogue"
)

func TestFamilyCloudDerivesTheVendorSuffix(t *testing.T) {
	cases := map[string]string{
		"provider-upjet-aws":          "aws",
		"provider-upjet-gcp":          "gcp",
		"provider-upjet-azure":        "azure",
		"provider-upjet-alibabacloud": "alibabacloud",
	}
	for repoName, want := range cases {
		if got := familyCloud(repoName); got != want {
			t.Errorf("familyCloud(%q) = %q, want %q", repoName, got, want)
		}
	}
}

func TestServiceAndFamilyPackageNames(t *testing.T) {
	if got, want := servicePackageName("aws", "rds"), "provider-aws-rds"; got != want {
		t.Errorf("servicePackageName = %q, want %q", got, want)
	}
	if got, want := familyPackageName("aws"), "provider-family-aws"; got != want {
		t.Errorf("familyPackageName = %q, want %q", got, want)
	}
}

// githubContentsServer serves GitHub's contents API
// (repos/{owner}/{repo}/contents/{path}) from a byName map of raw JSON
// bodies keyed by "owner/repo" — used to drive fetchDirNames/discoverFamilies
// against recorded-fixture-shaped responses (see testdata/cmd_provider_family.json
// and testdata/cmd_provider_monolithic.json, both trimmed copies of what
// api.github.com actually returned for cmd/provider in
// crossplane-contrib/provider-upjet-aws and crossplane-contrib/provider-upjet-mysql
// respectively while this file was written) without any real network access.
func githubContentsServer(t *testing.T, byRepo map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path is /repos/{owner}/{repo}/contents/{...path}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/repos/"), "/", 4)
		if len(parts) < 4 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		owner, repoName := parts[0], parts[1]
		key := owner + "/" + repoName
		body, ok := byRepo[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
}

func TestFetchDirNamesFiltersOutFiles(t *testing.T) {
	familyBody := readFile(t, "testdata/cmd_provider_family.json")
	monolithicBody := readFile(t, "testdata/cmd_provider_monolithic.json")

	srv := githubContentsServer(t, map[string][]byte{
		"crossplane-contrib/provider-upjet-aws":   familyBody,
		"crossplane-contrib/provider-upjet-mysql": monolithicBody,
	})
	defer srv.Close()

	dirs, err := fetchDirNames(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", "provider-upjet-aws", "cmd/provider")
	if err != nil {
		t.Fatalf("fetchDirNames: %v", err)
	}
	sort.Strings(dirs)
	want := []string{"config", "monolith", "rds", "s3", "sqs"}
	if !reflect.DeepEqual(dirs, want) {
		t.Errorf("dirs = %v, want %v", dirs, want)
	}

	dirs, err = fetchDirNames(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", "provider-upjet-mysql", "cmd/provider")
	if err != nil {
		t.Fatalf("fetchDirNames (monolithic): %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("dirs for a monolithic repo (cmd/provider/main.go only) = %v, want none — files are not directories", dirs)
	}
}

func TestFetchDirNamesErrorsOnMissingRepo(t *testing.T) {
	srv := githubContentsServer(t, map[string][]byte{})
	defer srv.Close()

	if _, err := fetchDirNames(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", "does-not-exist", "cmd/provider"); err == nil {
		t.Fatal("fetchDirNames for a 404 = nil error, want one")
	}
}

// TestDiscoverFamiliesFindsOnlyRealFamilies is the policy test for
// discoverFamilies: of several provider-upjet-* candidates, only the ones
// whose cmd/provider directory has real per-service subdirectories become a
// family; a monolithic provider-upjet-* repo (bare cmd/provider/main.go, no
// subdirectories — provider-upjet-mysql's actual live shape) is silently
// excluded rather than erroring, and a non-upjet repo is never even probed.
func TestDiscoverFamiliesFindsOnlyRealFamilies(t *testing.T) {
	familyBody := readFile(t, "testdata/cmd_provider_family.json")
	monolithicBody := readFile(t, "testdata/cmd_provider_monolithic.json")

	srv := githubContentsServer(t, map[string][]byte{
		"crossplane-contrib/provider-upjet-aws":   familyBody,
		"crossplane-contrib/provider-upjet-mysql": monolithicBody,
	})
	defer srv.Close()

	repos := []repo{
		{Name: "provider-upjet-aws", Owner: "crossplane-contrib", Description: "aws family", SourceURL: "https://github.com/crossplane-contrib/provider-upjet-aws", LicenseSPDX: "Apache-2.0"},
		{Name: "provider-upjet-mysql", Owner: "crossplane-contrib", Description: "mysql provider", SourceURL: "https://github.com/crossplane-contrib/provider-upjet-mysql", LicenseSPDX: "Apache-2.0"},
		{Name: "function-go-templating", Owner: "crossplane-contrib"}, // not even provider-upjet-*, never probed
	}

	families := discoverFamilies(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", repos, nil)
	if len(families) != 1 {
		t.Fatalf("discoverFamilies found %d families, want 1: %+v", len(families), families)
	}
	fam := families[0]
	if fam.Repo.Name != "provider-upjet-aws" || fam.Cloud != "aws" {
		t.Errorf("family = %+v, want provider-upjet-aws/aws", fam)
	}
	want := []string{"rds", "s3", "sqs"}
	if !reflect.DeepEqual(fam.Services, want) {
		t.Errorf("services = %v, want %v (config/monolith excluded, sorted)", fam.Services, want)
	}
}

func TestDiscoverFamiliesWarnsAndSkipsOnFetchError(t *testing.T) {
	srv := githubContentsServer(t, map[string][]byte{}) // every repo 404s
	defer srv.Close()

	repos := []repo{{Name: "provider-upjet-aws", Owner: "crossplane-contrib"}}

	var mu sync.Mutex
	var warnings []string
	warn := func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, fmt.Sprintf(format, a...))
	}

	families := discoverFamilies(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", repos, warn)
	if len(families) != 0 {
		t.Errorf("families = %+v, want none — the fetch failed", families)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want exactly 1", warnings)
	}
}

// TestResolveFamilyServiceTagsFallsBackToFamilyTag is
// resolveFamilyServiceTags' own policy test: a service with its own raw tag
// entry uses it; a service without one falls back to the family package's
// tag; a family with no resolvable tag at all leaves every service
// unresolved (Ref == "" downstream, in buildFamilyCatalogue).
func TestResolveFamilyServiceTagsFallsBackToFamilyTag(t *testing.T) {
	fam := family{
		Repo:     repo{Name: "provider-upjet-aws", Owner: "crossplane-contrib"},
		Cloud:    "aws",
		Services: []string{"ec2", "rds", "s3"},
	}

	rawTags := map[string][]string{
		"provider-family-aws": {"v2.6.0", "v2.7.0"},
		"provider-aws-rds":    {"v2.6.0", "v2.7.0"}, // sampled, matches family
		// ec2, s3 not individually resolved: must fall back to the family tag.
	}

	got := resolveFamilyServiceTags(fam, rawTags)
	want := map[string]string{"ec2": "v2.7.0", "rds": "v2.7.0", "s3": "v2.7.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveFamilyServiceTags = %v, want %v", got, want)
	}
}

func TestResolveFamilyServiceTagsHonoursAnExplicitMismatch(t *testing.T) {
	fam := family{
		Repo:     repo{Name: "provider-upjet-gcp", Owner: "crossplane-contrib"},
		Cloud:    "gcp",
		Services: []string{"storage", "compute"},
	}
	// "compute" was individually resolved and disagrees with the family tag
	// (e.g. it lagged a release) — its own value must win, not the family's.
	rawTags := map[string][]string{
		"provider-family-gcp":  {"v3.0.0"},
		"provider-gcp-compute": {"v2.6.0"},
	}

	got := resolveFamilyServiceTags(fam, rawTags)
	want := map[string]string{"storage": "v3.0.0", "compute": "v2.6.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveFamilyServiceTags = %v, want %v", got, want)
	}
}

func TestResolveFamilyServiceTagsUnresolvedFamilyLeavesEveryServiceEmpty(t *testing.T) {
	fam := family{
		Repo:     repo{Name: "provider-upjet-aws", Owner: "crossplane-contrib"},
		Cloud:    "aws",
		Services: []string{"rds", "s3"},
	}
	got := resolveFamilyServiceTags(fam, nil)
	want := map[string]string{"rds": "", "s3": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveFamilyServiceTags = %v, want %v (label, don't hide)", got, want)
	}
}

// familyGhcrServer is ghcrFakeServer's shape (see ghcr_test.go) reused here
// only for readability at the call site — it is the same function.
func familyGhcrServer(t *testing.T, tagsByPkg map[string][]string) *httptest.Server {
	return ghcrFakeServer(t, tagsByPkg)
}

// TestResolveFamilyTagsFastPathTrustsAMatchingSample pins the politeness
// optimization documented on resolveFamilyTags: when every sampled service
// agrees with the family package's own tag, no other service in the family
// is fetched individually at all.
func TestResolveFamilyTagsFastPathTrustsAMatchingSample(t *testing.T) {
	srv := familyGhcrServer(t, map[string][]string{
		"provider-family-aws": {"v2.7.0"},
		"provider-aws-ec2":    {"v2.7.0"},
		"provider-aws-rds":    {"v2.7.0"},
		"provider-aws-s3":     {"v2.7.0"},
		"provider-aws-sqs":    {"v2.7.0"},
		"provider-aws-iam":    {"v2.7.0"},
		// glue and wafv2 deliberately NOT registered on the fake server: with
		// 7 services and familySampleSize == 5, they fall outside the sample.
		// If resolveFamilyTags fetched them individually anyway, the token
		// request would 403 and this test would fail — proving the fast path
		// really did stop after the sample instead of quietly resolving
		// everything.
	})
	defer srv.Close()

	fam := family{
		Repo:     repo{Name: "provider-upjet-aws", Owner: "crossplane-contrib"},
		Cloud:    "aws",
		Services: []string{"ec2", "rds", "s3", "sqs", "iam", "glue", "wafv2"}, // 7 > familySampleSize (5)
	}

	rawTags := resolveFamilyTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", fam, nil)
	serviceTags := resolveFamilyServiceTags(fam, rawTags)
	want := map[string]string{
		"ec2": "v2.7.0", "rds": "v2.7.0", "s3": "v2.7.0", "sqs": "v2.7.0", "iam": "v2.7.0",
		// glue, wafv2: never fetched individually, correctly inherit the
		// family tag via resolveFamilyServiceTags' fallback.
		"glue": "v2.7.0", "wafv2": "v2.7.0",
	}
	if !reflect.DeepEqual(serviceTags, want) {
		t.Errorf("serviceTags = %v, want %v", serviceTags, want)
	}
	if _, attempted := rawTags["provider-aws-glue"]; attempted {
		t.Error("provider-aws-glue was fetched individually — the fast path should have trusted the family tag for services beyond the sample")
	}
}

// TestResolveFamilyTagsFallsBackWhenSampleDisagrees is the correctness half:
// one sampled service resolving to a different tag than the family package
// must force every remaining service to be resolved individually, not
// silently inherit the (wrong, for it) family tag.
func TestResolveFamilyTagsFallsBackWhenSampleDisagrees(t *testing.T) {
	srv := familyGhcrServer(t, map[string][]string{
		"provider-family-gcp":    {"v3.0.0"},
		"provider-gcp-storage":   {"v3.0.0"},
		"provider-gcp-compute":   {"v2.6.0"}, // disagrees with the family tag
		"provider-gcp-bigquery":  {"v3.0.0"},
		"provider-gcp-alloydb":   {"v3.0.0"},
		"provider-gcp-apigee":    {"v3.0.0"},
		"provider-gcp-appengine": {"v3.0.0"}, // 6th service, only reachable via the fallback's full resolution
	})
	defer srv.Close()

	fam := family{
		Repo:     repo{Name: "provider-upjet-gcp", Owner: "crossplane-contrib"},
		Cloud:    "gcp",
		Services: []string{"storage", "compute", "bigquery", "alloydb", "apigee", "appengine"},
	}

	rawTags := resolveFamilyTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", fam, nil)
	serviceTags := resolveFamilyServiceTags(fam, rawTags)
	want := map[string]string{
		"storage":   "v3.0.0",
		"compute":   "v2.6.0",
		"bigquery":  "v3.0.0",
		"alloydb":   "v3.0.0",
		"apigee":    "v3.0.0",
		"appengine": "v3.0.0",
	}
	if !reflect.DeepEqual(serviceTags, want) {
		t.Errorf("serviceTags = %v, want %v (fallback must resolve every service, including the 6th beyond the sample)", serviceTags, want)
	}
}

func TestBuildFamilyCatalogueLabelsUnresolvedServices(t *testing.T) {
	fam := family{
		Repo: repo{
			Name:        "provider-upjet-aws",
			Owner:       "crossplane-contrib",
			Description: "AWS support for Crossplane, generated by Upjet",
			SourceURL:   "https://github.com/crossplane-contrib/provider-upjet-aws",
			LicenseSPDX: "Apache-2.0",
		},
		Cloud:    "aws",
		Services: []string{"rds", "zzz-unresolved"},
	}
	serviceTags := map[string]string{"rds": "v2.7.0"} // "zzz-unresolved" absent -> ""

	got := buildFamilyCatalogue(fam, serviceTags)
	want := []catalogue.Provider{
		{
			Name:        "provider-aws-rds",
			Ref:         "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0",
			Description: "AWS support for Crossplane, generated by Upjet — rds service package",
			Source:      "https://github.com/crossplane-contrib/provider-upjet-aws",
			License:     "Apache-2.0",
		},
		{
			Name:        "provider-aws-zzz-unresolved",
			Ref:         "", // label, don't hide
			Description: "AWS support for Crossplane, generated by Upjet — zzz-unresolved service package",
			Source:      "https://github.com/crossplane-contrib/provider-upjet-aws",
			License:     "Apache-2.0",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildFamilyCatalogue = %+v, want %+v", got, want)
	}
}

func TestMergeCatalogueRepoDerivedWinsOnCollisionAndWarns(t *testing.T) {
	repoEntries := []catalogue.Provider{
		{Name: "provider-aws-rds", Ref: "ghcr.io/crossplane-contrib/provider-aws-rds:v1.0.0", Description: "repo-derived"},
	}
	familyEntries := []catalogue.Provider{
		{Name: "provider-aws-rds", Ref: "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0", Description: "family-derived"},
		{Name: "provider-aws-s3", Ref: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0", Description: "family-derived"},
	}

	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }

	got := mergeCatalogue(repoEntries, familyEntries, warn)
	want := []catalogue.Provider{
		{Name: "provider-aws-rds", Ref: "ghcr.io/crossplane-contrib/provider-aws-rds:v1.0.0", Description: "repo-derived"},
		{Name: "provider-aws-s3", Ref: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0", Description: "family-derived"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCatalogue = %+v, want %+v", got, want)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want exactly 1 (for the dropped provider-aws-rds collision)", warnings)
	}
}

func TestMergeCatalogueSortsByName(t *testing.T) {
	repoEntries := []catalogue.Provider{{Name: "provider-c"}, {Name: "provider-a"}}
	familyEntries := []catalogue.Provider{{Name: "provider-b"}}

	got := mergeCatalogue(repoEntries, familyEntries, nil)
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	want := []string{"provider-a", "provider-b", "provider-c"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestFamilyServicesSurviveTheFullBuildCatalogueValidatePipeline is a small
// end-to-end sanity check: a family entry built by this file's own functions
// passes catalogue.Validate alongside ordinary repo-derived entries, exactly
// as writeCatalogue requires before anything is written to disk.
func TestFamilyServicesSurviveTheFullBuildCatalogueValidatePipeline(t *testing.T) {
	repos := []repo{
		{Name: "function-go-templating", Owner: "crossplane-contrib", Description: "d", SourceURL: "s", LicenseSPDX: "Apache-2.0"},
	}
	tags := map[string][]string{"function-go-templating": {"v0.11.0"}}
	repoEntries := buildCatalogue(repos, tags)

	fam := family{
		Repo:     repo{Name: "provider-upjet-aws", Owner: "crossplane-contrib", Description: "aws family", SourceURL: "s", LicenseSPDX: "Apache-2.0"},
		Cloud:    "aws",
		Services: []string{"rds", "s3"},
	}
	serviceTags := map[string]string{"rds": "v2.7.0", "s3": "v2.7.0"}
	familyEntries := buildFamilyCatalogue(fam, serviceTags)

	entries := mergeCatalogue(repoEntries, familyEntries, nil)
	if err := catalogue.Validate(entries); err != nil {
		t.Fatalf("catalogue.Validate: %v", err)
	}

	var byName []string
	for _, e := range entries {
		byName = append(byName, e.Name)
	}
	for _, want := range []string{"function-go-templating", "provider-aws-rds", "provider-aws-s3"} {
		found := false
		for _, n := range byName {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("entries %v missing %q", byName, want)
		}
	}
}
