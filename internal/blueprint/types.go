// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

import "strings"

// Blueprint is the root document.
type Blueprint struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name string `json:"name"`
}

type Spec struct {
	Sources   []Source   `json:"sources"`
	XRD       XRD        `json:"xrd"`
	Resources []Resource `json:"resources"`
}

// Source is one schema source. M1 supports provider packages only.
type Source struct {
	Provider string `json:"provider"`
}

// XRD describes the composite API to generate.
type XRD struct {
	Group      string               `json:"group"`
	Kind       string               `json:"kind"`
	Plural     string               `json:"plural"`
	Version    string               `json:"version"`
	Scope      string               `json:"scope"`
	Parameters map[string]Parameter `json:"parameters"`
}

// Parameter is one spec field of the composite API. It is single-source: this
// declaration produces both the XRD schema and the template default.
type Parameter struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
}

// Resource is one composed resource.
//
// ForEach, when set, repeats the resource's whole rendered document N times,
// with N read at render time from an integer XRD parameter. The value grammar
// is exactly "params.<name>" — the same reference shape as Field.From. The
// referenced parameter must be an integer and must be either required or
// carry a default (see Validate): the Composition dereferences the loop
// bound unguarded, and under options: ["missingkey=error"] an absent key is
// a hard render failure, so only the XRD's required gate or its schema
// default makes the dereference safe.
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	ForEach  string           `json:"forEach"`
	Fields   map[string]Field `json:"fields"`
}

// Field sets one path on a composed resource. Exactly one of From, Value or Raw
// must be set.
//
// From accepts two reference grammars:
//
//	params.<name>                       — an XRD parameter of the composite
//	resources.<name>.status.<path>      — a status value observed on another
//	                                      composed resource in this blueprint
//
// A status reference is a cross-resource wire: the value exists only after
// the referenced resource has been observed at least once, so the emitter
// renders it behind a hasKey guard chain over $.observed.resources and the
// field is omitted cleanly until then — Crossplane fills it in on a later
// reconcile. The referenced resource must not itself be looped (forEach):
// a looped resource's composed names are indexed (<name>-0, <name>-1, ...),
// so the un-indexed key the reference names never appears in the observed
// map. Validate enforces both, plus that <path> resolves to a scalar leaf in
// the referenced kind's CRD status schema (checked in internal/emit, which
// holds the CRDs).
type Field struct {
	From  string `json:"from"`
	Value string `json:"value"`
	Raw   string `json:"raw"`
}

// statusRefPrefix marks a Field.From cross-resource status reference.
const statusRefPrefix = "resources."

// StatusRef splits a well-formed cross-resource reference
// resources.<name>.status.<path> into its resource name and status-relative
// path. ok is false when from is not shaped that way at all — either not a
// resources. reference (a params. one, say) or a resources. reference whose
// grammar is broken (no .status. separator, or an empty name or path).
// Validate is the layer that turns the broken-grammar case into a specific
// error; every other caller runs after Validate and can treat !ok as
// "not a status reference".
func StatusRef(from string) (resource, path string, ok bool) {
	rest, found := strings.CutPrefix(from, statusRefPrefix)
	if !found {
		return "", "", false
	}
	resource, path, found = strings.Cut(rest, ".status.")
	if !found || resource == "" || path == "" {
		return "", "", false
	}
	return resource, path, true
}
