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
			d.Line(8, "description: %s", p.Description)
		}
		if len(p.Enum) > 0 {
			d.Line(8, "enum:")
			for _, e := range p.Enum {
				d.Line(8, "- %s", e)
			}
		}
		if p.Type == "object" {
			d.Line(8, "additionalProperties:")
			d.Line(9, "type: string")
		}
	}
	if req := requiredParams(b); len(req) > 0 {
		d.Line(6, "required: [%s]", strings.Join(req, ", "))
	}
	d.Comment("Every parameter the template dereferences is required above, so a")
	d.Comment("missing value fails at the XR gate instead of rendering \"<no value>\".")
	return d.Bytes(), nil
}

// requiredParams unions the explicitly-required parameters with every parameter
// the templates actually read.
func requiredParams(b *blueprint.Blueprint) []string {
	seen := map[string]bool{}
	for n, p := range b.Spec.XRD.Parameters {
		if p.Required {
			seen[n] = true
		}
	}
	for _, n := range b.DereferencedParams() {
		if _, ok := b.Spec.XRD.Parameters[n]; ok {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
