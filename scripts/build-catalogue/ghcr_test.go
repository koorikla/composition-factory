package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// ghcrFakeServer serves the two-request anonymous flow (token, tags/list)
// for a fixed set of owner/repo -> tags. A repo not in tagsByRepo denies the
// token request with 403, mirroring what this generator actually observed
// live against ghcr.io for repos with no package under crossplane-contrib
// (see ghcr.go's doc comments).
func ghcrFakeServer(t *testing.T, tagsByRepo map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			scope := r.URL.Query().Get("scope")
			// scope is "repository:<owner>/<name>:pull"; find the repo name
			// by checking which key in tagsByRepo it ends with.
			for name := range tagsByRepo {
				if strings.HasSuffix(scope, name+":pull") {
					fmt.Fprintf(w, `{"token":"fake-token-for-%s"}`, name)
					return
				}
			}
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"errors":[{"code":"DENIED","message":"requested access to the resource is denied"}]}`)
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			auth := r.Header.Get("Authorization")
			for name, tags := range tagsByRepo {
				if auth == "Bearer fake-token-for-"+name {
					b, _ := json.Marshal(struct {
						Tags []string `json:"tags"`
					}{Tags: tags})
					w.Header().Set("Content-Type", "application/json")
					w.Write(b)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"errors":[{"code":"NAME_UNKNOWN"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchGhcrTagsHappyPath(t *testing.T) {
	srv := ghcrFakeServer(t, map[string][]string{
		"function-go-templating": {"v0.9.2", "v0.10.0", "v0.11.0"},
	})
	defer srv.Close()

	got, err := fetchGhcrTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", "function-go-templating")
	if err != nil {
		t.Fatalf("fetchGhcrTags: %v", err)
	}
	want := []string{"v0.9.2", "v0.10.0", "v0.11.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

// TestFetchGhcrTagsErrorsWhenTokenDenied mirrors what was actually observed
// live: ~63 of 108 crossplane-contrib provider-*/function-* repos have no
// resolvable ghcr.io package, and the token request itself is denied for
// them (403 DENIED), before any tags/list request is even attempted.
func TestFetchGhcrTagsErrorsWhenTokenDenied(t *testing.T) {
	srv := ghcrFakeServer(t, map[string][]string{
		"function-go-templating": {"v0.11.0"},
	})
	defer srv.Close()

	if _, err := fetchGhcrTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", "provider-upjet-aws"); err == nil {
		t.Fatal("fetchGhcrTags for a repo with no ghcr package = nil error, want one")
	}
}

// TestFetchAllGhcrTagsIsPartialFailureTolerant is fetchAllGhcrTags' own
// policy test: one repo resolving and one repo failing must not affect each
// other — the failing one gets a nil/empty tag list and a warning, the
// succeeding one gets its real tags, and both keys are present in the
// result map (so buildCatalogue can label the failed one instead of it
// silently vanishing — see build.go's TestBuildCatalogueLabels... for the
// downstream half of this contract).
func TestFetchAllGhcrTagsIsPartialFailureTolerant(t *testing.T) {
	srv := ghcrFakeServer(t, map[string][]string{
		"function-go-templating": {"v0.11.0"},
	})
	defer srv.Close()

	repos := []repo{
		{Name: "function-go-templating", Owner: "crossplane-contrib"},
		{Name: "provider-upjet-aws", Owner: "crossplane-contrib"},
	}

	var mu sync.Mutex
	var warnings []string
	warn := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	got := fetchAllGhcrTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", repos, warn)

	if len(got["function-go-templating"]) != 1 || got["function-go-templating"][0] != "v0.11.0" {
		t.Errorf("function-go-templating tags = %v, want [v0.11.0]", got["function-go-templating"])
	}
	if tags, ok := got["provider-upjet-aws"]; !ok {
		t.Error("provider-upjet-aws missing from the result map entirely — a failed repo must still get a (empty) entry")
	} else if len(tags) != 0 {
		t.Errorf("provider-upjet-aws tags = %v, want empty", tags)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want exactly 1 (for the failed repo)", warnings)
	}
}

func TestFetchAllGhcrTagsToleratesNilWarnFunc(t *testing.T) {
	srv := ghcrFakeServer(t, map[string][]string{})
	defer srv.Close()

	repos := []repo{{Name: "provider-nothing-here", Owner: "crossplane-contrib"}}
	got := fetchAllGhcrTags(context.Background(), srv.Client(), srv.URL, "crossplane-contrib", repos, nil)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
}
