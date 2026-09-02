package emit

import (
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// RHSKind describes the origin and nature of a field's right-hand side.
type RHSKind int

const (
	RHSLiteral RHSKind = iota
	RHSRaw
	RHSTemplate
	RHSParam
	RHSStatus
)

// StructuredRHS represents a typed, backend-independent representation of a field,
// annotation, or envelope assignment.
type StructuredRHS struct {
	Kind       RHSKind
	Value      string   // For Literal, Raw, or Template name
	Param      string   // e.g. "region", "net.cidr"
	ParamSegs  []string // e.g. ["net", "cidr"]
	Resource   string   // Source resource name for status ref
	StatusPath string   // e.g. "atProvider.arn"
	Optional   bool     // Whether this field is optional/conditional
	Guard      string   // Go-template guard expression
	RawExpr    string   // Go-template dereference expression without {{ }}
}

// resolveFieldRHS resolves a single blueprint field into its structured form and Go-template RHS/guard.
func resolveFieldRHS(p string, f blueprint.Field, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) (StructuredRHS, string, string, error) {
	var s StructuredRHS
	var rhs, guard string

	switch {
	case f.Value != "":
		s.Kind = RHSLiteral
		s.Value = f.Value
		rhs = quoteYAML(f.Value)
	case f.Raw != "":
		s.Kind = RHSRaw
		s.Value = f.Raw
		rhs = f.Raw
	case f.Template != "":
		if _, ok := b.Spec.Templates[f.Template]; !ok {
			return s, "", "", fmt.Errorf("resource %q field %q: unknown template %q", r.Name, p, f.Template)
		}
		s.Kind = RHSTemplate
		s.Value = f.Template
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
			s.Kind = RHSStatus
			s.Resource = ref.Resource
			s.StatusPath = strings.Join(ref.StatusPath, ".")
			s.Optional = true
			s.Guard = g
			s.RawExpr = expr
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
			s.Kind = RHSParam
			s.Param = chainRef
			s.ParamSegs = segs
			s.Optional = g != ""
			s.Guard = g
			s.RawExpr = fmt.Sprintf("$spec.%s", strings.Join(segs, "."))
			rhs = fmt.Sprintf("{{ $spec.%s }}", strings.Join(segs, "."))
			guard = g
		}
	}
	return s, rhs, guard, nil
}
