package main

// family.go closes this catalogue's specific coverage gap: searching it for
// "provider-aws-rds" (or provider-aws-s3, provider-gcp-storage, ...) found
// nothing, even though this project's own testdata and README install
// exactly ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0.
//
// The root cause is that github.go/build.go enumerate crossplane-contrib
// GitHub *repositories* and, for each one, try to resolve one
// ghcr.io/crossplane-contrib/<repo> image — which is right for the ordinary
// case (one repo, one image) but wrong for the four upjet "family" monorepos
// crossplane-contrib has today (provider-upjet-aws, provider-upjet-gcp,
// provider-upjet-azure, provider-upjet-alibabacloud, verified live while
// writing this file — see discoverFamilies' doc comment for how the other
// nine provider-upjet-* repos are told apart from these four). Each family
// monorepo publishes one per-service image per cmd/provider/<service>
// subdirectory instead of a single monolithic image:
// ghcr.io/crossplane-contrib/provider-<cloud>-<service>, e.g.
// provider-aws-rds, provider-gcp-storage, provider-azure-storage,
// provider-alibabacloud-ecs. None of those images has a GitHub repository of
// its own, so the plain repo-based enumeration can never find them — see
// .github/workflows/publish-provider-packages.yaml (in each family repo) and
// the reusable crossplane-contrib/provider-workflows repo it calls
// (.github/workflows/publish-provider-family.yml,
// .github/actions/get-provider-subpackages/action.yml), all fetched live and
// cross-checked against provider-upjet-aws's own Makefile
// (SUBPACKAGES/cmd/provider/*, and the providerconfig-e2e target's
// AWS_EC2_PACKAGE_IMAGE/AWS_RDS_PACKAGE_IMAGE/AWS_FAMILY_PACKAGE_IMAGE
// values) while building this file.
//
// See also docs/research/raw/schema-sourcing.md, which independently
// verified provider-family-aws vs. per-service packages like
// provider-aws-sqs from the xpkg-pulling side of this same family structure.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/koorikla/compositionfactory/catalogue"
)

const (
	// upjetFamilyPrefix names a candidate family monorepo. Every repo this
	// generator should even consider probing for per-service subpackages
	// starts with this — see discoverFamilies.
	upjetFamilyPrefix = "provider-upjet-"

	// familySampleSize bounds how many of a family's own services
	// resolveFamilyTags resolves individually before trusting the family
	// package's own tag for the rest. See resolveFamilyTags' doc comment for
	// why this is safe and how it falls back when it isn't.
	familySampleSize = 5
)

// nonServiceSubpackages are cmd/provider/ subdirectories that exist in every
// family repo but are not themselves a per-service package:
//
//   - "monolith" builds the single all-services-in-one-image provider. It is
//     not part of this catalogue's per-service listing — the family repo's
//     own entry (built from github.go/build.go, almost always Ref == "" since
//     the monolith itself is not published to ghcr.io/crossplane-contrib
//     either) stands in for it.
//   - "config" builds the family's ProviderConfig-only package. Every one of
//     the four families that has one names it "provider-family-<cloud>", not
//     "provider-<cloud>-config" — verified live against provider-upjet-aws's
//     own Makefile: the providerconfig-e2e target builds
//     SUBPACKAGES="ec2 rds kafka config" and then refers to the results as
//     AWS_EC2_PACKAGE_IMAGE=".../provider-aws-ec2:$(VERSION)",
//     AWS_RDS_PACKAGE_IMAGE=".../provider-aws-rds:$(VERSION)", but
//     AWS_FAMILY_PACKAGE_IMAGE=".../provider-family-aws:$(VERSION)" for the
//     "config" one. resolveFamilyTags resolves that package under its own
//     name (see familyPackageName) to seed the shared-tag assumption, but it
//     is not synthesized as a same-shaped provider-<cloud>-config entry.
var nonServiceSubpackages = map[string]bool{
	"monolith": true,
	"config":   true,
}

// familyCloud derives the vendor identifier a family's per-service images
// are named after (e.g. "aws", so cmd/provider/rds becomes
// "provider-aws-rds") from its repo name. Verified against all four families
// that actually enumerate per-service subpackages: each one's own Makefile
// sets PROVIDER_NAME to exactly this suffix — provider-upjet-aws has
// `PROVIDER_NAME := aws`, provider-upjet-gcp `PROVIDER_NAME := gcp`,
// provider-upjet-azure `PROVIDER_NAME := azure`, provider-upjet-alibabacloud
// `export PROVIDER_NAME := alibabacloud` — so deriving it from the repo name
// needs no extra request, live or offline.
func familyCloud(repoName string) string {
	return strings.TrimPrefix(repoName, upjetFamilyPrefix)
}

// familyPackageName is fam's own ProviderConfig-only package name (see
// nonServiceSubpackages' "config" case), e.g. "provider-family-aws".
func familyPackageName(cloud string) string {
	return "provider-family-" + cloud
}

// servicePackageName is the ghcr.io/crossplane-contrib image name for one
// service in a family, e.g. familyCloud("provider-upjet-aws")="aws" and
// svc="rds" -> "provider-aws-rds".
func servicePackageName(cloud, svc string) string {
	return "provider-" + cloud + "-" + svc
}

// family is one upjet monorepo this generator found to publish per-service
// packages, plus the list of services it publishes.
type family struct {
	// Repo is the family monorepo's own GitHub metadata (name, description,
	// URL, license) — reused verbatim for every service synthesized from it;
	// see buildFamilyCatalogue's doc comment for why no separate per-service
	// description is fetched.
	Repo repo
	// Cloud is familyCloud(Repo.Name), kept alongside it so callers do not
	// recompute it.
	Cloud string
	// Services is the sorted list of cmd/provider/<service> directory names,
	// excluding nonServiceSubpackages.
	Services []string
}

// fetchDirNames lists the subdirectory names GitHub's contents API reports
// for path within owner/repoName's default branch
// (https://docs.github.com/en/rest/repos/contents#get-repository-content),
// against baseURL (the real one is https://api.github.com; tests point this
// at an httptest server, matching every other function in this package).
// Only entries of type "dir" are returned — files (e.g. cmd/provider/main.go
// in a repo with no per-service split at all) are silently skipped rather
// than erroring, since a monolithic provider-upjet-* repo answering with
// zero directories here is an expected, ordinary outcome (see
// discoverFamilies), not a fetch failure.
//
// "main" is hard-coded as the ref because it is what all four families this
// generator found actually use as their default branch — verified live
// while writing this file.
func fetchDirNames(ctx context.Context, client *http.Client, baseURL, owner, repoName, path string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=main", baseURL, owner, repoName, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

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

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parse response for %s: %w", url, err)
	}

	var dirs []string
	for _, e := range entries {
		if e.Type == "dir" {
			dirs = append(dirs, e.Name)
		}
	}
	return dirs, nil
}

// discoverFamilies finds every repo in repos whose cmd/provider/ directory
// contains at least one real per-service subpackage (i.e. a directory other
// than the ones nonServiceSubpackages names). It only probes repos named
// provider-upjet-* to begin with, keeping the extra GitHub API cost small
// and bounded regardless of how many total repos the org has: crossplane-contrib
// has 13 such repos today (verified live), so this costs at most 13 extra
// requests against the same 60-requests/hour unauthenticated budget
// fetchGitHubRepos' doc comment already accounts for.
//
// Of those 13, only 4 actually enumerate per-service subpackages today
// (provider-upjet-aws, provider-upjet-gcp, provider-upjet-azure,
// provider-upjet-alibabacloud, verified live: 178, 82, 94 and 22
// cmd/provider/ directories respectively); the other 9
// (provider-upjet-azuread, -cloudflare, -digitalocean, -ec, -edgeadc,
// -github, -kafka, -mysql, -zitadel) have a bare cmd/provider/main.go and no
// subdirectories at all — a single-service provider that happens to share
// the provider-upjet-* naming convention, not a family. Those are correctly
// left to the plain repo-derived entry github.go/build.go already produce
// for them; this function reports them by simply not including them in its
// result, not as an error.
//
// A repo whose cmd/provider listing itself could not be fetched (network
// error, repo restructured, ref renamed) is reported to warn and skipped —
// the same partial-failure-tolerant policy fetchAllGhcrTags (ghcr.go) uses,
// so one family's fetch failure never aborts the whole run.
func discoverFamilies(ctx context.Context, client *http.Client, baseURL, owner string, repos []repo, warn warnFunc) []family {
	var candidates []repo
	for _, r := range repos {
		if strings.HasPrefix(r.Name, upjetFamilyPrefix) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	type result struct {
		fam family
		ok  bool
	}
	results := make(chan result, len(candidates))
	sem := make(chan struct{}, ghcrConcurrency)
	var wg sync.WaitGroup

	for _, r := range candidates {
		wg.Add(1)
		go func(r repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dirs, err := fetchDirNames(ctx, client, baseURL, owner, r.Name, "cmd/provider")
			if err != nil {
				if warn != nil {
					warn("build-catalogue: %s: could not list cmd/provider: %v", r.Name, err)
				}
				results <- result{}
				return
			}
			var services []string
			for _, d := range dirs {
				if !nonServiceSubpackages[d] {
					services = append(services, d)
				}
			}
			if len(services) == 0 {
				// A provider-upjet-* repo with no per-service split — not a
				// family (see this function's own doc comment). Nothing to add.
				results <- result{}
				return
			}
			sort.Strings(services)
			results <- result{fam: family{Repo: r, Cloud: familyCloud(r.Name), Services: services}, ok: true}
		}(r)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var families []family
	for res := range results {
		if res.ok {
			families = append(families, res.fam)
		}
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Repo.Name < families[j].Repo.Name })
	return families
}

// fetchTagsForPackages resolves fetchGhcrTags for every package name in
// pkgs, bounded to ghcrConcurrency requests in flight at once — the same
// shape and politeness bound as fetchAllGhcrTags (ghcr.go), generalized from
// "one call per repo name" to "one call per arbitrary ghcr.io package name"
// since a family's package names (provider-family-aws, provider-aws-rds, …)
// are not GitHub repo names at all. A package whose tags could not be
// resolved is reported to warn (if non-nil) and recorded as a nil slice,
// same partial-failure-tolerant policy as fetchAllGhcrTags.
func fetchTagsForPackages(ctx context.Context, client *http.Client, baseURL, owner string, pkgs []string, warn warnFunc) map[string][]string {
	type result struct {
		pkg  string
		tags []string
	}
	results := make(chan result, len(pkgs))
	sem := make(chan struct{}, ghcrConcurrency)
	var wg sync.WaitGroup

	for _, pkg := range pkgs {
		wg.Add(1)
		go func(pkg string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tags, err := fetchGhcrTags(ctx, client, baseURL, owner, pkg)
			if err != nil {
				if warn != nil {
					warn("build-catalogue: %s: no ghcr.io tags resolved: %v", pkg, err)
				}
				tags = nil
			}
			results <- result{pkg: pkg, tags: tags}
		}(pkg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make(map[string][]string, len(pkgs))
	for res := range results {
		out[res.pkg] = res.tags
	}
	return out
}

// resolveFamilyTags resolves as much of fam's per-service tag data as it
// needs to over the network, and returns it in the same shape manifest mode
// stores it in offline (see manifestFamily.Tags): raw, unfiltered
// ghcr.io/v2/.../tags/list output keyed by full package name. Pass the
// result to resolveFamilyServiceTags to turn it into one resolved tag per
// service.
//
// It always resolves fam's own "provider-family-<cloud>" package (one
// ghcr.io round trip) plus up to familySampleSize of fam's services. If
// every sampled service's own latest-stable tag matches the family
// package's, it stops there and trusts that tag for every other service too
// — an upjet family publishes every one of its images from a single
// release, so all its subpackages normally share one version. This was
// VERIFIED live while writing this file: provider-family-aws,
// provider-aws-rds, provider-aws-s3 and provider-aws-sqs all resolved to
// v2.7.0; provider-family-gcp, provider-gcp-storage, provider-gcp-compute
// and provider-gcp-bigquery all resolved to v3.0.0; provider-family-azure,
// provider-azure-storage, provider-azure-compute and provider-azure-network
// all resolved to v2.7.0; provider-family-alibabacloud,
// provider-alibabacloud-ecs, provider-alibabacloud-oss and
// provider-alibabacloud-polardb all resolved to v1.3.0.
//
// If the sample disagrees with the family tag for even one service (a
// service released independently, retired, or not yet caught up), this
// falls back to resolving every remaining service in the family
// individually, so no wrong version is ever silently assumed — the sample
// is a politeness optimization (the alternative, one tags/list pair per
// service on every run, would cost ~370 more request pairs today across the
// four known families, against a registry this generator is explicitly
// meant to poll politely — see ghcrConcurrency's doc comment), not a
// correctness shortcut.
func resolveFamilyTags(ctx context.Context, client *http.Client, baseURL, owner string, fam family, warn warnFunc) map[string][]string {
	familyPkg := familyPackageName(fam.Cloud)

	sample := fam.Services
	if len(sample) > familySampleSize {
		sample = sample[:familySampleSize]
	}

	pkgs := make([]string, 0, len(sample)+1)
	pkgs = append(pkgs, familyPkg)
	for _, svc := range sample {
		pkgs = append(pkgs, servicePackageName(fam.Cloud, svc))
	}

	rawTags := fetchTagsForPackages(ctx, client, baseURL, owner, pkgs, warn)

	familyTag := latestStableTag(rawTags[familyPkg])
	sampleMatches := familyTag != ""
	for _, svc := range sample {
		if latestStableTag(rawTags[servicePackageName(fam.Cloud, svc)]) != familyTag {
			sampleMatches = false
			break
		}
	}
	if sampleMatches {
		return rawTags
	}

	if warn != nil {
		warn("build-catalogue: %s: sampled service tags did not all match the family tag %q; resolving every service individually", fam.Repo.Name, familyTag)
	}

	var remaining []string
	for _, svc := range fam.Services {
		pkg := servicePackageName(fam.Cloud, svc)
		if _, done := rawTags[pkg]; !done {
			remaining = append(remaining, pkg)
		}
	}
	rest := fetchTagsForPackages(ctx, client, baseURL, owner, remaining, warn)
	for pkg, tags := range rest {
		rawTags[pkg] = tags
	}
	return rawTags
}

// resolveFamilyServiceTags derives one resolved tag per service in fam from
// rawTags — raw, unfiltered ghcr.io tags/list output keyed by full package
// name (see resolveFamilyTags and manifestFamily.Tags, which produce this
// shape from the network and from a manifest file respectively). A service
// whose own package name is present in rawTags gets its own
// latestStableTag; every other service falls back to the family package's
// (provider-family-<cloud>) latestStableTag — see resolveFamilyTags' doc
// comment for why that fallback is safe. This function is pure and
// deterministic so it can be tested against fixtures without a network
// round trip either way — the same split live mode's fetchAllGhcrTags/
// buildCatalogue and offline mode's loadManifest/buildCatalogue already use.
func resolveFamilyServiceTags(fam family, rawTags map[string][]string) map[string]string {
	familyTag := latestStableTag(rawTags[familyPackageName(fam.Cloud)])

	out := make(map[string]string, len(fam.Services))
	for _, svc := range fam.Services {
		pkg := servicePackageName(fam.Cloud, svc)
		if raw, ok := rawTags[pkg]; ok {
			out[svc] = latestStableTag(raw)
			continue
		}
		out[svc] = familyTag
	}
	return out
}

// buildFamilyCatalogue turns one family's services and their resolved tags
// (see resolveFamilyServiceTags) into catalogue entries, one per service,
// named the same way the family itself names the ghcr.io image
// (provider-<cloud>-<service>) so Name and the image basename inside Ref
// always agree, exactly like every repo-derived entry already does.
//
// Description and License are the family repo's own GitHub description and
// license, reused verbatim for every service in it, with a short per-service
// suffix identifying which service this entry is — not a separate
// per-service description fetched over the network. A true per-service
// description would need one more GitHub API request per service (~370
// across the four known families today), on top of the ~140 requests
// enumerating the org's repos plus the ~13 requests discoverFamilies makes
// already cost against GitHub's 60-requests/hour unauthenticated budget (see
// fetchGitHubRepos' and discoverFamilies' doc comments) — not affordable for
// a value this generator does not strictly need. This is "description from
// the service where available" applied literally: what is available without
// spending that budget is the family's own description, refined per
// service.
//
// A service whose tag could not be resolved at all still gets an entry,
// with Ref == "" — the same label-don't-hide policy buildCatalogue (build.go)
// applies to repos.
func buildFamilyCatalogue(fam family, serviceTags map[string]string) []catalogue.Provider {
	out := make([]catalogue.Provider, 0, len(fam.Services))
	for _, svc := range fam.Services {
		name := servicePackageName(fam.Cloud, svc)
		tag := serviceTags[svc]
		var ref string
		if tag != "" {
			ref = fmt.Sprintf("%s/%s/%s:%s", ghcrRegistry, fam.Repo.Owner, name, tag)
		}
		desc := fam.Repo.Description
		if desc != "" {
			desc = fmt.Sprintf("%s — %s service package", desc, svc)
		} else {
			desc = fmt.Sprintf("%s service package from %s", svc, fam.Repo.Name)
		}
		out = append(out, catalogue.Provider{
			Name:        name,
			Ref:         ref,
			Description: desc,
			Source:      fam.Repo.SourceURL,
			License:     licenseOr(fam.Repo.LicenseSPDX),
		})
	}
	return out
}

// mergeCatalogue combines repo-derived and family-derived entries into one
// deterministic, Name-sorted, duplicate-free catalogue slice — the shape
// catalogue.Validate requires and writeCatalogue enforces before writing
// anything to disk. Name and Ref agree 1:1 for every entry this generator
// produces (Ref's image basename is always exactly Name — see buildRef and
// buildFamilyCatalogue), so deduplicating by Name here is the same thing as
// this generator's "dedupe by ref" policy.
//
// No collision between the two sources is known to exist today (no
// crossplane-contrib GitHub repo happens to be named
// "provider-<cloud>-<service>" for a cloud/service pair an upjet family also
// publishes), but one is handled deterministically rather than left to
// whichever slice happened to be appended last: repo-derived entries always
// win, and a dropped family entry is reported to warn so a genuine collision
// does not silently disappear.
func mergeCatalogue(repoEntries, familyEntries []catalogue.Provider, warn warnFunc) []catalogue.Provider {
	seen := make(map[string]bool, len(repoEntries)+len(familyEntries))
	out := make([]catalogue.Provider, 0, len(repoEntries)+len(familyEntries))

	for _, e := range repoEntries {
		seen[e.Name] = true
		out = append(out, e)
	}
	for _, e := range familyEntries {
		if seen[e.Name] {
			if warn != nil {
				warn("build-catalogue: %s: family-derived entry collides with a repo-derived entry of the same name; keeping the repo-derived one", e.Name)
			}
			continue
		}
		seen[e.Name] = true
		out = append(out, e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
