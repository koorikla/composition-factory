package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/koorikla/compositionfactory/catalogue"
)

// ghcrRegistry is the registry host every Ref this generator builds points
// at. Hard-coded here (as opposed to owner, which is a flag — see main.go)
// because a Ref always names the same host the tags were actually resolved
// against; there would be nothing to gain from parameterizing it separately.
const ghcrRegistry = "ghcr.io"

// repo is one crossplane-contrib GitHub repository, reduced to the fields
// this generator needs. It is the common shape both live mode
// (fetchGitHubRepos, in github.go) and --from-file mode (manifest.repos, in
// manifest.go) normalize into before buildCatalogue ever sees either one —
// buildCatalogue itself has no idea which source a repo came from.
type repo struct {
	Name        string
	Owner       string // GitHub org / ghcr.io namespace, e.g. "crossplane-contrib"
	Description string
	SourceURL   string
	LicenseSPDX string // "" when GitHub reports no detected license
}

// stableTagPattern matches a strict, unadorned semver release tag —
// "v1.2.3" — and nothing else. Two shapes that a looser "starts with v and
// has two dots" check would wrongly accept are excluded on purpose:
//
//   - pre-releases ("v1.2.3-rc1"), which are not what "stable" means;
//   - Go pseudo-versions ("v0.0.0-20251028114116-30cc3a089783"), which upjet
//     and several crossplane-contrib repos push to ghcr.io for every commit
//     to main. These outnumber real releases by ~10:1 in the tag lists this
//     generator observed live against ghcr.io/crossplane-contrib
//     (function-go-templating alone had over 90 of them against 3 real
//     releases) and are floating per-commit builds, not something a
//     catalogue should ever recommend installing.
var stableTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// semver is a parsed stableTagPattern match, kept only long enough to order
// tags against each other.
type semver struct {
	major, minor, patch int
	tag                 string
}

func (s semver) less(o semver) bool {
	if s.major != o.major {
		return s.major < o.major
	}
	if s.minor != o.minor {
		return s.minor < o.minor
	}
	return s.patch < o.patch
}

// latestStableTag returns the highest strict-semver tag in tags (see
// stableTagPattern), or "" if none of them are one — a repo that only ships
// pseudo-versions, or whose ghcr.io tag list could not be resolved at all
// (fetchAllGhcrTags records that as a nil/empty slice rather than an error;
// see its own doc comment), lands here. Input order does not matter; the
// result does not depend on the registry's own (observed to vary) tag-list
// order.
func latestStableTag(tags []string) string {
	var parsed []semver
	for _, t := range tags {
		m := stableTagPattern.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		parsed = append(parsed, semver{major, minor, patch, t})
	}
	if len(parsed) == 0 {
		return ""
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].less(parsed[j]) })
	return parsed[len(parsed)-1].tag
}

// buildRef renders the installable image reference for r given the latest
// stable tag already resolved for it (see latestStableTag). "" in, "" out —
// see buildCatalogue's doc comment for why an unresolved tag is not
// papered over with a floating tag like ":latest".
func buildRef(r repo, tag string) string {
	if tag == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s:%s", ghcrRegistry, r.Owner, r.Name, tag)
}

// licenseOr defaults an empty SPDX id to the SPDX placeholder for "no
// license asserted", so catalogue.Provider.License is never a silently
// empty string a caller could mistake for "public domain" or "checked and
// found to have none".
func licenseOr(spdx string) string {
	if spdx == "" {
		return "NOASSERTION"
	}
	return spdx
}

// buildCatalogue turns repos and their ghcr.io tag lists (tagsByRepo, keyed
// by repo name — see fetchAllGhcrTags and manifest.Tags) into the
// deterministic, Name-sorted []catalogue.Provider this whole command exists
// to produce.
//
// Every element of repos becomes exactly one entry: this is the project's
// "label, don't hide" catalogue policy in code. A repo this generator could
// not resolve a stable ghcr.io tag for — archived, published under a
// different registry/namespace (most upjet provider families ship as
// xpkg.upbound.io/upbound/provider-<service>, not one
// ghcr.io/crossplane-contrib/<repo> image — see docs/catalogue.md), or
// genuinely tagless — still appears, with Ref == "" labelling that fact,
// rather than silently vanishing from the list. A caller that only wants
// installable entries filters on Ref != "" itself.
func buildCatalogue(repos []repo, tagsByRepo map[string][]string) []catalogue.Provider {
	out := make([]catalogue.Provider, 0, len(repos))
	for _, r := range repos {
		tag := latestStableTag(tagsByRepo[r.Name])
		out = append(out, catalogue.Provider{
			Name:        r.Name,
			Ref:         buildRef(r, tag),
			Description: r.Description,
			Source:      r.SourceURL,
			License:     licenseOr(r.LicenseSPDX),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// writeCatalogue marshals entries as this project's one deterministic JSON
// format — 2-space indent, HTML-unescaped (so a GitHub URL's "&" never
// round-trips as "&"), one trailing newline (json.Encoder.Encode
// always appends exactly one) — validates it (catalogue.Validate) before
// ever touching disk, and writes it to path, creating path's parent
// directory if it does not already exist.
func writeCatalogue(path string, entries []catalogue.Provider) error {
	if err := catalogue.Validate(entries); err != nil {
		return fmt.Errorf("build-catalogue: refusing to write an invalid catalogue: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("build-catalogue: encode: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("build-catalogue: mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("build-catalogue: write %s: %w", path, err)
	}
	return nil
}
