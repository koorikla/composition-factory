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
	header(d, blueprintSource(b))
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
	writeParameterProperties(d, 7, x.Parameters)
	if req := requiredParams(x.Parameters); len(req) > 0 {
		formatted := make([]string, len(req))
		for i, r := range req {
			formatted[i] = formatYAMLKey(r)
		}
		d.Line(6, "required: [%s]", strings.Join(formatted, ", "))
	}
	d.Comment("required lists only the parameters the blueprint marks Required.")
	d.Comment("A merely-dereferenced parameter is safe unforced: the Composition")
	d.Comment("guards every optional access with hasKey, never a bare dereference.")

	if len(x.Status) > 0 {
		d.Line(5, "status:")
		d.Line(6, "type: object")
		d.Line(6, "properties:")
		writeParameterProperties(d, 7, x.Status)
		if req := requiredParams(x.Status); len(req) > 0 {
			formatted := make([]string, len(req))
			for i, r := range req {
				formatted[i] = formatYAMLKey(r)
			}
			d.Line(6, "required: [%s]", strings.Join(formatted, ", "))
		}
	}
	return d.Bytes(), nil
}

func writeParameterProperties(d *Doc, ind int, params map[string]blueprint.Parameter) {
	names := make([]string, 0, len(params))
	for n := range params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := params[n]
		d.Line(ind, "%s:", formatYAMLKey(n))
		d.Line(ind+1, "type: %s", p.Type)
		if p.Description != "" {
			// User-authored free text: quote it. Unquoted, a ": " sequence is
			// an invalid mapping-value indicator (parse error) and a " #"
			// sequence silently truncates the rest of the string as a comment.
			d.Line(ind+1, "description: %s", quoteYAML(p.Description))
		}
		if p.Default != "" {
			d.Line(ind+1, "default: %s", defaultYAML(p.Type, p.Default))
		}
		if len(p.Enum) > 0 {
			d.Line(ind+1, "enum:")
			for _, e := range p.Enum {
				// Strings are quoted so YAML keywords/numbers are not
				// reinterpreted as bool/number/null on a type: string field.
				// Non-string types (integer, number, boolean) are emitted bare
				// so the Kubernetes API server accepts them as the declared type.
				d.Line(ind+1, "- %s", enumYAML(p.Type, e))
			}
		}
		if p.Type == "object" && len(p.Properties) == 0 {
			// The v1 free-form map. Byte-identical to what this emitter
			// wrote before typed members existed — a propertyless object
			// parameter keeps additionalProperties: string exactly.
			d.Line(ind+1, "additionalProperties:")
			d.Line(ind+2, "type: string")
		}
		if p.Type == "object" && len(p.Properties) > 0 {
			writeObjectMembers(d, ind+1, p)
		}
	}
}

// requiredParams returns the explicitly-required parameters, sorted.
func requiredParams(params map[string]blueprint.Parameter) []string {
	out := make([]string, 0, len(params))
	for n, p := range params {
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

// enumYAML formats an enum value for the OpenAPI v3 schema.
// Strings are always YAML-quoted so that values like "yes", "no", "1.0",
// or "" are not reinterpreted as bool/number/null on a type: string field.
// Non-string types (integer, number, boolean) are emitted bare so the
// Kubernetes API server accepts them as the declared schema type.
func enumYAML(paramType, value string) string {
	if paramType == "string" {
		return quoteYAML(value)
	}
	return value
}

// writeObjectMembers renders a typed object's member schema recursively:
// properties, per-member description/default/enum, a required list per
// level — and NO additionalProperties, because the members ARE the schema.
// An object member with properties of its own recurses (arbitrary depth,
// the openapi-editor shape); a propertyless object member keeps the v1
// free-form string map. Sorted throughout: determinism is a correctness
// requirement. ind is the column "properties:" itself lands on.
func writeObjectMembers(d *Doc, ind int, p blueprint.Parameter) {
	d.Line(ind, "properties:")
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
		d.Line(ind+1, "%s:", formatYAMLKey(m))
		d.Line(ind+2, "type: %s", mp.Type)
		if mp.Description != "" {
			// Same quoting rule as a top-level description: ": " and
			// " #" in free text change the document's meaning unquoted.
			d.Line(ind+2, "description: %s", quoteYAML(mp.Description))
		}
		if mp.Default != "" {
			d.Line(ind+2, "default: %s", defaultYAML(mp.Type, mp.Default))
		}
		if len(mp.Enum) > 0 {
			d.Line(ind+2, "enum:")
			for _, e := range mp.Enum {
				d.Line(ind+2, "- %s", enumYAML(mp.Type, e))
			}
		}
		if mp.Type == "object" && len(mp.Properties) == 0 {
			d.Line(ind+2, "additionalProperties:")
			d.Line(ind+3, "type: string")
		}
		if mp.Type == "object" && len(mp.Properties) > 0 {
			writeObjectMembers(d, ind+2, mp)
		}
	}
	if len(requiredMembers) > 0 {
		formatted := make([]string, len(requiredMembers))
		for i, r := range requiredMembers {
			formatted[i] = formatYAMLKey(r)
		}
		d.Line(ind, "required: [%s]", strings.Join(formatted, ", "))
	}
}
