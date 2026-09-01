package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// githubRepo is the subset of GitHub's repo-list API response this
// generator reads:
// https://docs.github.com/en/rest/repos/repos#list-organization-repositories
type githubRepo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	HTMLURL     string `json:"html_url"`
	Fork        bool   `json:"fork"`
	License     *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
}

// githubPerPage is GitHub's own maximum page size, so enumerating an org's
// repos costs the fewest possible requests.
const githubPerPage = 100

// fetchGitHubRepos enumerates every repository in org via GitHub's
// unauthenticated REST API against baseURL (the real API is
// https://api.github.com; tests point this at an httptest server),
// paginating until a page returns fewer than githubPerPage repos.
//
// Rate-limit-graceful: an unauthenticated caller gets 60 requests/hour
// (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).
// crossplane-contrib has ~140 repos, so a full run costs 2 requests —
// comfortably inside that budget even run every week (see
// .github/workflows/catalogue.yml). If GitHub still answers 403 with
// X-RateLimit-Remaining: 0 partway through a run, that is not treated as
// fully fatal: the repos already collected are returned alongside a non-nil
// error describing the cutoff, so a caller can choose a partial catalogue
// (better than none, and this project's own catalogue policy is to label
// gaps rather than produce nothing — see buildCatalogue) over aborting the
// whole run. A caller that wants a partial result to hard-fail checks the
// returned error itself; run (main.go) does exactly that when the FIRST
// page fails (zero repos collected), and warns-but-continues otherwise.
func fetchGitHubRepos(ctx context.Context, client *http.Client, baseURL, org string) ([]githubRepo, error) {
	var all []githubRepo
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/orgs/%s/repos?per_page=%d&page=%d&type=public", baseURL, org, githubPerPage, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return all, fmt.Errorf("build request for %s: %w", url, err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			return all, fmt.Errorf("GET %s: %w", url, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return all, fmt.Errorf("github: rate limit exhausted after %d repo(s) across %d page(s); resets at unix time %s",
				len(all), page-1, resp.Header.Get("X-RateLimit-Reset"))
		}
		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body))
		}
		if readErr != nil {
			return all, fmt.Errorf("read response body for %s: %w", url, readErr)
		}

		var pageRepos []githubRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return all, fmt.Errorf("parse response for %s: %w", url, err)
		}
		all = append(all, pageRepos...)
		if len(pageRepos) < githubPerPage {
			return all, nil
		}
	}
}

// filterCatalogueRepos keeps exactly the repos this catalogue is about:
// non-fork provider-* and function-* repositories. Forks are excluded — a
// fork (crossplane-contrib/provider-checkly is one, verified live) is a
// duplicate of a repo maintained elsewhere, not a distinct package this
// catalogue should offer alongside the original.
func filterCatalogueRepos(all []githubRepo) []githubRepo {
	var out []githubRepo
	for _, r := range all {
		if r.Fork {
			continue
		}
		if strings.HasPrefix(r.Name, "provider-") || strings.HasPrefix(r.Name, "function-") {
			out = append(out, r)
		}
	}
	return out
}

// toRepos converts filtered githubRepo entries (see filterCatalogueRepos)
// into this package's internal repo shape.
func toRepos(owner string, all []githubRepo) []repo {
	out := make([]repo, 0, len(all))
	for _, r := range all {
		spdx := ""
		if r.License != nil {
			spdx = r.License.SPDXID
		}
		out = append(out, repo{
			Name:        r.Name,
			Owner:       owner,
			Description: r.Description,
			SourceURL:   r.HTMLURL,
			LicenseSPDX: spdx,
		})
	}
	return out
}
