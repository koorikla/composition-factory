// Package index builds a searchable index of the managed-resource kinds
// available across a set of cached provider schemas. It is deliberately
// tiny — full schemas are not embedded in it — so it can be loaded eagerly by
// the canvas, CLI and MCP server, while full per-kind schemas are fetched
// from the cache on demand.
package index

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// Kind is one indexed managed-resource kind: one version of one CRD from one
// provider. Upjet providers ship every managed resource twice — a
// cluster-scoped variant (e.g. sqs.aws.upbound.io) and a namespaced ".m."
// variant (e.g. sqs.aws.m.upbound.io) — so both appear as distinct entries
// with their own APIVersion and Scope.
type Kind struct {
	Kind       string `json:"kind"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	APIVersion string `json:"apiVersion"` // group/version
	Plural     string `json:"plural"`
	Scope      string `json:"scope"`    // Namespaced | Cluster
	Provider   string `json:"provider"` // the xpkg ref it came from
	Namespaced bool   `json:"namespaced"`
	Required   int    `json:"required"` // count of required forProvider leaves
	Fields     int    `json:"fields"`   // count of forProvider leaves
}

// Index is a searchable, sorted index of Kinds, along with the CRDs they
// were built from so a caller can resolve one back to its full schema
// without re-reading the cache.
type Index struct {
	kinds []Kind
	crds  map[string]schema.CRD // keyed by apiVersion + "/" + kind
}

// Build indexes every managed-resource CRD across byProvider, a map from
// xpkg provider ref to the CRDs cached for it. Non-managed CRDs (such as
// ProviderConfigs) are excluded.
//
// A CRD whose preferred version or apiVersion cannot be determined is
// malformed; Build skips it rather than failing the whole index, since one
// bad CRD in one provider should not prevent every other provider's kinds
// from being indexed. Build returns an error only when every managed CRD
// across every provider failed this way — compositionfactory has no logging
// framework, so a total failure has to surface as an error rather than
// silently producing an empty index. That error wraps the last underlying
// Preferred/APIVersion failure, so it is actionable rather than just a count.
func Build(byProvider map[string][]schema.CRD) (*Index, error) {
	// Provider keys come from a map, whose iteration order is randomized; sort
	// them so that Build (and therefore All, and which CRD wins a Lookup
	// collision — see Lookup) is byte-stable across rebuilds.
	providers := make([]string, 0, len(byProvider))
	for p := range byProvider {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var kinds []Kind
	crds := make(map[string]schema.CRD)
	var attempted, failed int
	var lastErr error

	for _, provider := range providers {
		for _, c := range byProvider[provider] {
			if !c.IsManaged() {
				continue
			}
			attempted++

			v, err := c.Preferred()
			if err != nil {
				failed++
				lastErr = err
				continue
			}
			apiVersion, err := c.APIVersion()
			if err != nil {
				failed++
				lastErr = err
				continue
			}

			// ForProvider legitimately returns (nil, nil) when a CRD has no
			// forProvider block at all (e.g. provider-kubernetes's
			// ObservedObjectCollection). An error here — for example a
			// version with no schema block whatsoever, as upjet ships for
			// some non-storage versions — also yields a nil node slice, so
			// it collapses to the same outcome: zero fields. The kind
			// itself is still real (its Preferred/APIVersion resolved
			// fine) and belongs in the index either way.
			nodes, _ := c.ForProvider()
			leaves := schema.Leaves(nodes, "")
			required := 0
			for _, l := range leaves {
				if l.Node.Required {
					required++
				}
			}

			kinds = append(kinds, Kind{
				Kind:       c.Kind,
				Group:      c.Group,
				Version:    v.Name,
				APIVersion: apiVersion,
				Plural:     c.Plural,
				Scope:      c.Scope,
				Provider:   provider,
				Namespaced: c.Namespaced(),
				Required:   required,
				Fields:     len(leaves),
			})
			// Last write wins: if two providers ship a CRD under the same
			// apiVersion+kind (see Lookup), the one from the
			// lexicographically greatest provider ref — processed last,
			// since providers is sorted above — ends up here.
			crds[apiVersion+"/"+c.Kind] = c
		}
	}

	if attempted > 0 && failed == attempted {
		return nil, fmt.Errorf("index: all %d managed CRD(s) failed to index: %w", attempted, lastErr)
	}

	sort.SliceStable(kinds, func(i, j int) bool {
		if kinds[i].APIVersion != kinds[j].APIVersion {
			return kinds[i].APIVersion < kinds[j].APIVersion
		}
		if kinds[i].Kind != kinds[j].Kind {
			return kinds[i].Kind < kinds[j].Kind
		}
		return kinds[i].Provider < kinds[j].Provider
	})

	return &Index{kinds: kinds, crds: crds}, nil
}

// All returns every indexed Kind sorted by (APIVersion, Kind). It is a copy
// of the index's internal slice, so the caller mutating the result cannot
// corrupt the index, and repeated calls are byte-identical.
func (i *Index) All() []Kind {
	out := make([]Kind, len(i.kinds))
	copy(out, i.kinds)
	return out
}

// Search returns the indexed Kinds whose Kind or Group contains q as a
// case-insensitive substring, preserving All()'s ordering. limit caps the
// number of results; limit <= 0 means no limit.
func (i *Index) Search(q string, limit int) []Kind {
	q = strings.ToLower(q)
	var out []Kind
	for _, k := range i.kinds {
		if strings.Contains(strings.ToLower(k.Kind), q) || strings.Contains(strings.ToLower(k.Group), q) {
			out = append(out, k)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out
}

// Lookup returns the CRD indexed under the given apiVersion (group/version)
// and kind, so a caller holding a Kind from All or Search can resolve its
// full schema without re-reading the cache.
//
// Duplicate-key note: the normal case of two entries sharing a Kind but not
// an apiVersion (the cluster/namespaced ".m." pairing every upjet provider
// ships) is unambiguous — each has its own key. The rare case of two
// different providers shipping the exact same apiVersion+kind is a genuine
// collision, and Lookup can only return one CRD for that key: it returns
// the one from the lexicographically greatest provider ref, since Build
// processes providers in sorted order and the last one processed wins.
func (i *Index) Lookup(apiVersion, kind string) (schema.CRD, bool) {
	c, ok := i.crds[apiVersion+"/"+kind]
	return c, ok
}
