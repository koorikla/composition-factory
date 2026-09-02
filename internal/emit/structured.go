package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// rhsKind describes the origin and nature of a field's right-hand side.
type rhsKind int

const (
	rhsUnset rhsKind = iota
	rhsLiteral
	rhsRaw
	rhsTemplate
	rhsParam
	rhsStatus
)

// structuredRHS represents a typed, backend-independent representation of a field,
// annotation, or envelope assignment.
type structuredRHS struct {
	kind       rhsKind
	value      string   // For Literal, Raw, or Template name
	param      string   // e.g. "region", "net.cidr"
	paramSegs  []string // e.g. ["net", "cidr"]
	resource   string   // Source resource name for status ref
	statusPath string   // e.g. "atProvider.arn"
	optional   bool     // Whether this field is optional/conditional
	guard      string   // Go-template guard expression
	rawExpr    string   // Go-template dereference expression without {{ }}
}

// resolveFieldRHS resolves a single blueprint field into its structured form and Go-template RHS/guard.
func resolveFieldRHS(p string, f blueprint.Field, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) (structuredRHS, string, string, error) {
	var s structuredRHS
	var rhs, guard string

	switch {
	case f.Value != "":
		s.kind = rhsLiteral
		s.value = f.Value
		rhs = quoteYAML(f.Value)
	case f.Raw != "":
		s.kind = rhsRaw
		s.value = f.Raw
		rhs = f.Raw
	case f.Template != "":
		if _, ok := b.Spec.Templates[f.Template]; !ok {
			return s, "", "", fmt.Errorf("resource %q field %q: unknown template %q", r.Name, p, f.Template)
		}
		s.kind = rhsTemplate
		s.value = f.Template
		rhs = templateCallRHS(f.Template, r.Name, p)
	case f.From != "":
		ref, err := blueprint.ParseFrom(f.From)
		if err != nil {
			return s, "", "", fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
		}
		if ref.Resource != "" {
			g, expr, err := statusWire(ref, r, fmt.Sprintf("field %q", p), b, crds, wantNamespaced)
			if err != nil {
				return s, "", "", err
			}
			s.kind = rhsStatus
			s.resource = ref.Resource
			s.statusPath = strings.Join(ref.StatusPath, ".")
			s.optional = true
			s.guard = g
			s.rawExpr = expr
			rhs = "{{ " + expr + " }}"
			guard = g
		} else {
			param, member, _ := blueprint.ParamRef(f.From)
			chainRef := param
			if member != "" {
				chainRef = param + "." + member
			}
			segs, chain, err := blueprint.ParamChain(b.Spec.XRD,
				fmt.Sprintf("resource %q field %q", r.Name, p), chainRef)
			if err != nil {
				return s, "", "", err
			}
			g := chainGuard(segs, chain)
			s.kind = rhsParam
			s.param = chainRef
			s.paramSegs = segs
			s.optional = g != ""
			s.guard = g
			s.rawExpr = fmt.Sprintf("$spec.%s", strings.Join(segs, "."))
			rhs = fmt.Sprintf("{{ $spec.%s }}", strings.Join(segs, "."))
			guard = g
		}
	}
	return s, rhs, guard, nil
}
