// Package catalogue holds the provider/function discovery catalogue: a
// static, CI-refreshed list of installable crossplane-contrib packages,
// embedded into the compositionfactory binary at compile time.
//
// It is static rather than queried live because there is no live query to
// make. Neither xpkg.upbound.io nor ghcr.io expose an anonymous /v2/_catalog
// listing (both return 401/403 to an anonymous token — verified directly
// against both while building this package; see docs/catalogue.md and
// docs/research/raw/schema-sourcing.md), so "what providers exist" cannot be
// answered per request the way "what tags does this one provider have" can.
// scripts/build-catalogue enumerates crossplane-contrib's repos and their
// published ghcr.io images ahead of time and writes the result to
// providers.json, which this package embeds; see that command's package doc
// for how and how often that happens.
//
// providers.json lives in this directory (rather than a top-level
// catalogue/ data directory read at runtime) because go:embed patterns
// cannot reach outside the embedding file's own directory tree — colocating
// the Go code that serves the data with the data itself is what makes
// go:embed usable here at all, not just tidy.
package catalogue

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed providers.json
var providersJSON []byte

// Provider is one entry in the discovery catalogue: a single
// crossplane-contrib provider-* or function-* repository, resolved to its
// latest stable ghcr.io image where one could be found.
type Provider struct {
	// Name is the repository name, e.g. "provider-aws" or
	// "function-go-templating" — also the ghcr.io/crossplane-contrib image
	// name when Ref is non-empty.
	Name string `json:"name"`
	// Ref is the full, installable image reference for the latest stable
	// (strict semver, non-prerelease) tag scripts/build-catalogue could
	// resolve on ghcr.io, e.g. "ghcr.io/crossplane-contrib/function-go-templating:v0.11.0".
	//
	// Empty, deliberately, rather than the entry being omitted, when no
	// stable tag could be resolved — an archived repo, one published under a
	// different registry/namespace entirely (most upjet provider families
	// ship as xpkg.upbound.io/upbound/provider-<service>, not a single
	// ghcr.io/crossplane-contrib/<repo> image), or one with no releases at
	// all. This project's catalogue policy is to label such repos, not hide
	// them — see scripts/build-catalogue's buildCatalogue.
	Ref string `json:"ref"`
	// Description is the repository's own GitHub description, verbatim.
	Description string `json:"description"`
	// Source is the repository's GitHub URL.
	Source string `json:"source"`
	// License is the repository's SPDX license identifier (e.g.
	// "Apache-2.0"), or the SPDX placeholder "NOASSERTION" when GitHub
	// reports no detected license.
	License string `json:"license"`
}

// Load parses the embedded providers.json into a slice of Provider. It
// fails only if providers.json itself is malformed — unreachable in
// practice once TestEmbeddedCatalogueIsValid (catalogue_test.go) passes in
// CI, but callers still check the error rather than assume it can't happen,
// per this project's own "no silent wrongness" standard.
func Load() ([]Provider, error) {
	var out []Provider
	if err := json.Unmarshal(providersJSON, &out); err != nil {
		return nil, fmt.Errorf("catalogue: parse embedded providers.json: %w", err)
	}
	return out, nil
}

// Validate reports whether entries is non-empty, strictly sorted by Name,
// and free of duplicate names. scripts/build-catalogue's writeCatalogue
// calls this before writing providers.json, so every caller of Load can
// rely on these invariants without re-checking them; catalogue_test.go
// checks that the embedded file itself still satisfies them.
func Validate(entries []Provider) error {
	if len(entries) == 0 {
		return fmt.Errorf("catalogue: empty")
	}
	for i, e := range entries {
		if e.Name == "" {
			return fmt.Errorf("catalogue: entry %d has an empty name", i)
		}
		if i > 0 && entries[i-1].Name >= e.Name {
			return fmt.Errorf("catalogue: not sorted (or duplicate name) at index %d: %q >= %q",
				i, entries[i-1].Name, e.Name)
		}
	}
	return nil
}
