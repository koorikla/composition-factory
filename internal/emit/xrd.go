package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// XRD renders the CompositeResourceDefinition for b.
func XRD(b *blueprint.Blueprint) ([]byte, error) {
	x := b.Spec.XRD
	if x.Scope == "LegacyCluster" {
		return nil, fmt.Errorf("scope LegacyCluster is not valid in apiextensions.crossplane.io/v2")
	}
	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Line(0, "apiVersion: apiextensions.crossplane.io/v2")
	d.Line(0, "kind: CompositeResourceDefinition")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s.%s", x.Plural, x.Group)
	d.Line(0, "spec:")
	d.Line(1, "group: %s", x.Group)
	d.Line(1, "names:")
	d.Line(2, "kind: %s", x.Kind)
	d.Line(2, "plural: %s", x.Plural)
	// Always explicit: the API server defaults an omitted scope to Namespaced
	// while `crossplane xrd convert` defaults it to LegacyCluster.
	d.Line(1, "scope: %s", x.Scope)
	d.Line(1, "versions:")
	d.Line(1, "- name: %s", x.Version)
	d.Line(2, "served: true")
	d.Line(2, "referenceable: true")
	d.Line(2, "schema:")
	d.Line(3, "openAPIV3Schema:")
	d.Line(4, "type: object")
	d.Line(4, "properties:")
	d.Line(5, "spec:")
	d.Line(6, "type: object")
	d.Line(6, "properties:")

	names := make([]string, 0, len(x.Parameters))
	for n := range x.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := x.Parameters[n]
		d.Line(7, "%s:", n)
		d.Line(8, "type: %s", p.Type)
		if p.Description != "" {
			// User-authored free text: quote it. Unquoted, a ": " sequence is
			// an invalid mapping-value indicator (parse error) and a " #"
			// sequence silently truncates the rest of the string as a comment.
			d.Line(8, "description: %s", quoteYAML(p.Description))
		}
		if p.Default != "" {
			d.Line(8, "default: %s", defaultYAML(p.Type, p.Default))
		}
		if len(p.Enum) > 0 {
			d.Line(8, "enum:")
			for _, e := range p.Enum {
				// User-authored value: quote it. Unquoted, values like "yes",
				// "no", "1.0" or "" are YAML keywords/numbers and would be
				// silently reinterpreted as bool/number/null on a
				// type: string field, corrupting the enum.
				d.Line(8, "- %s", quoteYAML(e))
			}
		}
		if p.Type == "object" && len(p.Properties) == 0 {
			// The v1 free-form map. Byte-identical to what this emitter
			// wrote before typed members existed — a propertyless object
			// parameter keeps additionalProperties: string exactly.
			d.Line(8, "additionalProperties:")
			d.Line(9, "type: string")
		}
		if p.Type == "object" && len(p.Properties) > 0 {
			// Typed members render as a real nested schema: properties,
			// per-member description/default/enum, a required list — and NO
			// additionalProperties, because the members ARE the schema.
			// Sorted throughout: determinism is a correctness requirement.
			d.Line(8, "properties:")
			members := make([]string, 0, len(p.Properties))
			for m := range p.Properties {
				members = append(members, m)
			}
			sort.Strings(members)
			var requiredMembers []string
			for _, m := range members {
				mp := p.Properties[m]
				if mp.Required {
					requiredMembers = append(requiredMembers, m)
				}
				d.Line(9, "%s:", m)
				d.Line(10, "type: %s", mp.Type)
				if mp.Description != "" {
					// Same quoting rule as a top-level description: ": " and
					// " #" in free text change the document's meaning unquoted.
					d.Line(10, "description: %s", quoteYAML(mp.Description))
				}
				if mp.Default != "" {
					d.Line(10, "default: %s", defaultYAML(mp.Type, mp.Default))
				}
				if len(mp.Enum) > 0 {
					d.Line(10, "enum:")
					for _, e := range mp.Enum {
						d.Line(10, "- %s", quoteYAML(e))
					}
				}
			}
			if len(requiredMembers) > 0 {
				d.Line(8, "required: [%s]", strings.Join(requiredMembers, ", "))
			}
		}
	}
	if req := requiredParams(x); len(req) > 0 {
		d.Line(6, "required: [%s]", strings.Join(req, ", "))
	}
	d.Comment("required lists only the parameters the blueprint marks Required.")
	d.Comment("A merely-dereferenced parameter is safe unforced: the Composition")
	d.Comment("guards every optional access with hasKey, never a bare dereference.")
	return d.Bytes(), nil
}

// requiredParams returns the explicitly-required parameters, sorted.
//
// This deliberately does NOT also include every parameter some template
// dereferences. An earlier version of this emitter unioned the two: the idea
// was that a Go template dereferencing a missing XR field renders the
// literal string "<no value>" into a live managed resource, and since that
// string is legal YAML the whole validate -> render -> validate pipeline
// still exits 0 -- so forcing every dereferenced parameter to be required
// looked like the mitigation.
//
// That union is now both unnecessary and actively harmful. The Composition
// emitter (internal/emit/composition.go) no longer takes a bare dereference
// on trust: it gives a required parameter direct, unguarded template access
// (safe, because the XRD gate makes its presence unconditional on any valid
// XR) and gates every access to a non-required parameter behind hasKey
// (safe, because the guarded branch only renders when the key provably
// exists). Both paths are independently immune to "<no value>" -- the
// mitigation this field's Required-only-ness now needs to protect is nothing.
// Unioning in the dereferenced set here would not close any remaining gap;
// it would only strip optionality from every parameter a template happens to
// read, which is precisely what "required" is supposed to let a blueprint
// author NOT do. If you're reading this because you're tempted to restore
// the union believing it closes a hole: it doesn't, not anymore -- read
// composition.go's hasKey handling first.
func requiredParams(x blueprint.XRD) []string {
	out := make([]string, 0, len(x.Parameters))
	for n, p := range x.Parameters {
		if p.Required {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// defaultYAML renders p.Default for a parameter of the given declared type.
// A string default is user-controlled free text exactly like description, so
// it is quoted the same way, for the same reasons (an unquoted ": '" breaks
// the document, an unquoted "yes"/"1.0"-shaped value is silently
// reinterpreted). Every other type this project accepts (integer, number,
// boolean) must instead be unquoted: a quoted "512" or "true" in an
// OpenAPIV3Schema default is a string, which is a type mismatch against the
// property's declared `type: integer`/`number`/`boolean` and gets the whole
// XRD rejected by the API server at apply time.
//
// object and array are deliberately not special-cased: this function treats
// them the same as the numeric/boolean types (emitted unquoted, verbatim).
// See task-8b-report.md for why that is a known, intentionally-unclosed gap
// here rather than a guess at a data format this package does not otherwise
// parse or validate.
func defaultYAML(paramType, value string) string {
	if paramType == "string" {
		return quoteYAML(value)
	}
	return value
}
