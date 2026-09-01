package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ghcrToken requests an anonymous, pull-scoped bearer token for one
// repository from baseURL's token endpoint (the real one is
// https://ghcr.io/token; tests point this at an httptest server). ghcr.io
// grants these with no credentials at all — verified directly while
// building this catalogue, the same anonymous-bearer-handshake shape
// internal/xpkg documents for xpkg.upbound.io. A repo whose package does not
// exist under owner/name, or is private, is denied at this step rather than
// at tags/list, which is why token acquisition is its own function callers
// can fail on before ever issuing a tags request.
func ghcrToken(ctx context.Context, client *http.Client, baseURL, owner, name string) (string, error) {
	url := fmt.Sprintf("%s/token?service=ghcr.io&scope=repository:%s/%s:pull", baseURL, owner, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build token request for %s/%s: %w", owner, name, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response for %s/%s: %w", owner, name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request for %s/%s: status %d: %s", owner, name, resp.StatusCode, string(body))
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse token response for %s/%s: %w", owner, name, err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("empty token in response for %s/%s", owner, name)
	}
	return out.Token, nil
}

// fetchGhcrTags resolves every tag ghcr.io reports for owner/name against
// baseURL, via ghcrToken followed by a GET .../tags/list — the same
// two-request anonymous flow verified live in §4 of
// docs/research/raw/schema-sourcing.md, against xpkg.upbound.io there and
// against ghcr.io here.
func fetchGhcrTags(ctx context.Context, client *http.Client, baseURL, owner, name string) ([]string, error) {
	token, err := ghcrToken(ctx, client, baseURL, owner, name)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/%s/tags/list", baseURL, owner, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response for %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body))
	}

	var out struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse tags response for %s: %w", url, err)
	}
	return out.Tags, nil
}

// ghcrConcurrency bounds how many repos' tag lists this generator resolves
// at once — the research corpus's own politeness recommendation for an
// anonymous client against a shared registry (cap at 8-16 concurrent; see
// docs/research/raw/schema-sourcing.md §7).
const ghcrConcurrency = 8

// warnFunc receives one best-effort diagnostic; nil means "don't bother
// reporting these" (used by tests that don't want to assert on stderr
// text).
type warnFunc func(format string, args ...any)

// fetchAllGhcrTags resolves fetchGhcrTags for every repo in repos against
// baseURL/owner, bounded to ghcrConcurrency requests in flight at once.
//
// A single repo's failure is not fatal to the run: it is reported to warn
// (if non-nil) and recorded as a nil tag list. That is a real, common
// outcome — of the ~108 crossplane-contrib provider-*/function-* repos this
// generator found live, 63 had no resolvable ghcr.io/crossplane-contrib
// package at all (most of the large upjet provider families publish
// per-service images under xpkg.upbound.io/upbound/ instead; some archived
// repos' packages are simply gone) — and buildCatalogue's label-don't-hide
// policy turns a nil tag list into a Provider entry with Ref == "" rather
// than dropping the repo from the catalogue.
func fetchAllGhcrTags(ctx context.Context, client *http.Client, baseURL, owner string, repos []repo, warn warnFunc) map[string][]string {
	type result struct {
		name string
		tags []string
	}
	results := make(chan result, len(repos))
	sem := make(chan struct{}, ghcrConcurrency)
	var wg sync.WaitGroup

	for _, r := range repos {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tags, err := fetchGhcrTags(ctx, client, baseURL, owner, name)
			if err != nil {
				if warn != nil {
					warn("build-catalogue: %s: no ghcr.io tags resolved: %v", name, err)
				}
				tags = nil
			}
			results <- result{name: name, tags: tags}
		}(r.Name)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make(map[string][]string, len(repos))
	for res := range results {
		out[res.name] = res.tags
	}
	return out
}
