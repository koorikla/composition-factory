// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

import (
	"fmt"
	"regexp"
	"strings"
)

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
	Sources []Source `json:"sources"`
	XRD     XRD      `json:"xrd"`
	// Templates are user-defined Go templates, name -> body. Each is emitted
	// as a {{- define "<name>" }} block heading the Composition's template
	// and is callable from a field via template: <name> (or applied by a
	// convention). Bodies are validated by parsing them under the real
	// engine's contract — text/template with missingkey=error and
	// function-go-templating's function set (sprig minus env/expandenv, plus
	// its own additions) — so a body that cannot parse at render time is
	// refused at the source. A body renders with the minimal context the
	// field call passes: .spec (the composite's spec), .xr (the composite's
	// metadata.name), .resource (the composed resource's name) and .field
	// (the field path being set). Dereferences of optional .spec keys inside
	// a body are the author's contract: guard them with hasKey, exactly as
	// the generator does.
	Templates map[string]string `json:"templates"`
	// Conventions apply a template to every matching field a resource does
	// NOT set explicitly. Match is a case-sensitive suffix of a top-level
	// forProvider field name (e.g. "tags" matches tags; "Name" matches
	// queueName); the first matching convention in list order wins for each
	// field, and an explicit field always wins over any convention — that is
	// the override mechanism.
	Conventions []Convention `json:"conventions"`
	Resources   []Resource   `json:"resources"`
}

// Convention binds a template to every top-level forProvider leaf whose name
// ends with Match, on every resource that does not set that field itself.
type Convention struct {
	Match    string `json:"match"`
	Template string `json:"template"`
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
// When, when set, wraps the resource's whole rendered document — outside any
// forEach range, so a false condition skips every iteration — in a template
// conditional. The grammar is minimal and exact (see ParseWhen):
//
//	params.<name>                  — a bare boolean parameter
//	params.<name> == "<literal>"   — string equality
//	params.<name> != "<literal>"   — string inequality
//
// The referenced parameter must be required or carry a default: the emitted
// condition dereferences it unguarded ({{- if $spec.<name> }}), and under
// options: ["missingkey=error"] an absent key is a hard render failure — the
// same rule, for the same reason, as ForEach's loop bound. The bare form
// requires a boolean parameter; the comparison forms require a string one,
// and when that parameter declares an enum the literal must be one of its
// values (a literal outside the enum makes the condition constant, silently
// dead — every gate green, a resource that can never (or always) exist).
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	ForEach  string           `json:"forEach"`
	When     string           `json:"when"`
	Fields   map[string]Field `json:"fields"`
}

// when grammar, compiled once. The literal character class excludes '"' and
// '\\' so the emitted Go-syntax quoting (%q) is always the literal wrapped
// in plain quotes — byte-deterministic, no escape sequences to reason about.
var (
	whenBareRE = regexp.MustCompile(`^params\.([a-zA-Z][a-zA-Z0-9]*)$`)
	whenCmpRE  = regexp.MustCompile(`^params\.([a-zA-Z][a-zA-Z0-9]*) (==|!=) "([^"\\]*)"$`)
)

// ParseWhen splits a when expression into its parameter name, operator and
// literal. op is "" for the bare boolean form, "==" or "!=" for the
// comparison forms (with literal carrying the compared string, which may be
// empty: params.x == "" is legal). The grammar is exact — one space around
// the operator, double quotes around the literal — so that a when expression
// has exactly one written form and the emitted Composition is
// byte-deterministic.
func ParseWhen(expr string) (param, op, literal string, err error) {
	if m := whenBareRE.FindStringSubmatch(expr); m != nil {
		return m[1], "", "", nil
	}
	if m := whenCmpRE.FindStringSubmatch(expr); m != nil {
		return m[1], m[2], m[3], nil
	}
	return "", "", "", fmt.Errorf("when must be params.<name> (a boolean parameter), "+
		`params.<name> == "<literal>" or params.<name> != "<literal>" — exactly one space around `+
		"the operator, double quotes around the literal, no backslashes or embedded quotes (got %q)", expr)
}

// Field sets one path on a composed resource. Exactly one of From, Value,
// Raw or Template must be set. Template names an entry in spec.templates and
// renders as an include call whose output becomes the field's YAML value —
// a scalar on one line, or an indented block for a multi-line body (the
// generator owns the nindent).
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
	From     string `json:"from"`
	Value    string `json:"value"`
	Raw      string `json:"raw"`
	Template string `json:"template"`
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
