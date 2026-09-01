package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// manifest is the --from-file input shape: a GitHub repo listing plus a
// ghcr.io tags-by-repo map, enough to run buildCatalogue without touching
// the network. It deliberately mirrors the raw shapes live mode itself
// consumes (githubRepo's JSON in github.go, tags/list's JSON in ghcr.go) —
// not a bespoke format only this tool understands — so that a manifest can
// be assembled from curl output plus a small extraction step. See
// docs/catalogue.md for the exact recipe used to build the manifest this
// repo's own catalogue/providers.json was generated from, in an environment
// where a compiled Go binary cannot reach the network at all but curl can.
type manifest struct {
	Repos []manifestRepo `json:"repos"`
	// Tags maps a repo name to every tag ghcr.io/v2/<owner>/<repo>/tags/list
	// reported for it — unfiltered; latestStableTag (build.go) does the
	// filtering. A repo absent from this map, or present with an empty
	// slice, is treated identically: no resolvable stable tag.
	Tags map[string][]string `json:"tags"`
}

// manifestRepo is one repo entry in a manifest file. Field names match
// GitHub's own REST API response (see githubRepo in github.go) wherever the
// two overlap, so a manifest's "repos" array can be built directly from
// `curl https://api.github.com/orgs/<org>/repos` output filtered down to
// matching names, with no field renaming step.
type manifestRepo struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	HTMLURL       string `json:"html_url"`
	LicenseSPDXID string `json:"license_spdx_id"`
}

// loadManifest reads and parses a --from-file manifest.
func loadManifest(path string) (*manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// repos converts a manifest's repo entries into this package's internal
// repo shape, tagging every one with owner so buildRef has what it needs.
// owner is one value for the whole manifest, mirroring live mode
// (fetchGitHubRepos and fetchAllGhcrTags are both called for one org at a
// time — see main.go's run).
func (m *manifest) repos(owner string) []repo {
	out := make([]repo, 0, len(m.Repos))
	for _, r := range m.Repos {
		out = append(out, repo{
			Name:        r.Name,
			Owner:       owner,
			Description: r.Description,
			SourceURL:   r.HTMLURL,
			LicenseSPDX: r.LicenseSPDXID,
		})
	}
	return out
}
