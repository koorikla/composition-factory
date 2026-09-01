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
	// Families carries the upjet provider-family monorepos' per-service
	// package data (see family.go) — absent or empty when the manifest
	// predates that feature or the org simply has none, in which case
	// families(...) below returns nil and the catalogue is exactly what it
	// would have been without family.go at all.
	Families []manifestFamily `json:"families"`
}

// manifestFamily is one upjet family monorepo's entry in a manifest file —
// the --from-file equivalent of what discoverFamilies + resolveFamilyTags
// resolve over the network in live mode (see family.go). Field names for
// the repo-level metadata match manifestRepo's for the same reason
// manifestRepo's match githubRepo's: a manifest can be assembled directly
// from `curl https://api.github.com/repos/<owner>/<repo>` output, with no
// field renaming step.
type manifestFamily struct {
	// Repo is the family monorepo's GitHub repository name, e.g.
	// "provider-upjet-aws".
	Repo          string `json:"repo"`
	Description   string `json:"description"`
	HTMLURL       string `json:"html_url"`
	LicenseSPDXID string `json:"license_spdx_id"`
	// Services is the cmd/provider/<service> directory names this family
	// publishes as per-service packages — the contents-API equivalent of
	// what `curl https://api.github.com/repos/<owner>/<repo>/contents/cmd/provider`
	// reports, filtered down to directory names other than "monolith" and
	// "config" (see nonServiceSubpackages).
	Services []string `json:"services"`
	// Tags maps a full ghcr.io package name — the family's own
	// "provider-family-<cloud>" plus, ordinarily, a handful of individual
	// "provider-<cloud>-<service>" packages sampled to confirm the family
	// shares one version (see resolveFamilyTags) — to that package's
	// unfiltered ghcr.io/v2/.../tags/list output. A service named in
	// Services but absent here falls back to the family package's own tag;
	// see resolveFamilyServiceTags.
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

// families converts a manifest's family entries into this package's
// internal family shape, the --from-file counterpart to discoverFamilies
// (family.go). owner is one value for the whole manifest, mirroring repos
// above.
func (m *manifest) families(owner string) []family {
	out := make([]family, 0, len(m.Families))
	for _, f := range m.Families {
		out = append(out, family{
			Repo: repo{
				Name:        f.Repo,
				Owner:       owner,
				Description: f.Description,
				SourceURL:   f.HTMLURL,
				LicenseSPDX: f.LicenseSPDXID,
			},
			Cloud:    familyCloud(f.Repo),
			Services: f.Services,
		})
	}
	return out
}

// familyTags returns the manifest's raw per-package tag data for one family
// repo (see manifestFamily.Tags), in the same shape resolveFamilyTags
// produces from the network — the input resolveFamilyServiceTags expects.
func (m *manifest) familyTags(repoName string) map[string][]string {
	for _, f := range m.Families {
		if f.Repo == repoName {
			return f.Tags
		}
	}
	return nil
}
