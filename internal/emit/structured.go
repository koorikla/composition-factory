package emit

import (
	"fmt"
	"math"
	"strconv"
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
	rhsMetadata
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
	targetType string   // "string", "integer", "number", "boolean", "array", "map"
}

// resolveFieldRHS resolves a single blueprint field into its structured form and Go-template RHS/guard.
func resolveFieldRHS(p string, f blueprint.Field, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool, node *schema.Node, isMap bool) (structuredRHS, string, string, error) {
	var s structuredRHS
	var rhs, guard string

	targetType := ""
	branch := false
	if node != nil {
		targetType = node.Type
		branch = len(node.Children) > 0 || node.Type == "object"
	}
	if isMap {
		targetType = "string"
	}
	s.targetType = targetType

	switch {
	case f.Value != "":
		s.kind = rhsLiteral
		if branch {
			if targetType == "array" {
				return s, "", "", fmt.Errorf("resource %q field %q is an array of objects; value's comma-separated form renders scalar entries only — set the whole array with raw:", r.Name, p)
			}
			return s, "", "", fmt.Errorf("resource %q field %q is an object; value cannot render a composite — set its individual children (e.g. %s.<key>), or set the whole node with raw:", r.Name, p, p)
		}
		if isMap {
			s.value = f.Value
			s.targetType = "string"
			rhs = quoteYAML(f.Value)
			return s, rhs, guard, nil
		}
		// IntOrString normalizes to type string in the tree, so the string
		// case below would quote it — but an integer literal here is a port
		// NUMBER, and quoting makes the API server read it as a port NAME.
		// See isIntOrStringNode.
		if isIntOrStringNode(node) && isIntegerLiteral(f.Value) {
			s.value = f.Value
			s.targetType = "integer"
			return s, f.Value, guard, nil
		}

		switch targetType {
		case "string":
			s.value = f.Value
			s.targetType = "string"
			rhs = quoteYAML(f.Value)
		case "integer":
			i, err := strconv.ParseInt(f.Value, 10, 64)
			if err != nil {
				return s, "", "", fmt.Errorf("resource %q field %q: value %q is not a valid integer: %w", r.Name, p, f.Value, err)
			}
			s.value = strconv.FormatInt(i, 10)
			s.targetType = "integer"
			rhs = strconv.FormatInt(i, 10)
		case "number":
			val, err := strconv.ParseFloat(f.Value, 64)
			if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
				return s, "", "", fmt.Errorf("resource %q field %q: value %q is not a valid number (NaN and Inf are refused)", r.Name, p, f.Value)
			}
			s.value = f.Value
			s.targetType = "number"
			rhs = f.Value
		case "boolean":
			val := strings.ToLower(f.Value)
			switch val {
			case "true", "false":
				s.value = val
				s.targetType = "boolean"
				rhs = val
			default:
				return s, "", "", fmt.Errorf("resource %q field %q: value %q is not a valid boolean (use true or false)", r.Name, p, f.Value)
			}
		case "array":
			entries := strings.Split(f.Value, ",")
			quoted := make([]string, 0, len(entries))
			for _, e := range entries {
				sVal := strings.TrimSpace(e)
				if sVal == "" {
					return s, "", "", fmt.Errorf("resource %q field %q: value %q contains an empty entry (check for trailing/double commas)", r.Name, p, f.Value)
				}
				quoted = append(quoted, quoteYAML(sVal))
			}
			s.value = f.Value
			s.targetType = "array"
			rhs = "[" + strings.Join(quoted, ", ") + "]"
		default:
			// Untyped in schema: keep as quoted string
			s.value = f.Value
			s.targetType = "string"
			rhs = quoteYAML(f.Value)
		}

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
			if branch {
				return s, "", "", fmt.Errorf("resource %q field %q is an object; a from: wire renders one scalar and cannot fill it — wire its individual children (e.g. %s.<key>), or set the whole node with raw:", r.Name, p, p)
			}
			if targetType == "array" {
				return s, "", "", fmt.Errorf("resource %q field %q is an array, and a from: wire cannot render a list in v1 — a scalar parameter renders one scalar, and array parameters are not supported. Use value: with comma-separated entries, or raw: for literal YAML", r.Name, p)
			}
			if targetType == "map" && !isMap {
				return s, "", "", fmt.Errorf("resource %q field %q is a map, and a from: wire cannot render one in v1. Set it with raw:", r.Name, p)
			}

			if ref.IsMetadataName() {
				targetDecl := b.ResourceNamed(ref.Resource)
				if targetDecl == nil {
					return s, "", "", fmt.Errorf("resource %q field %q: references unknown resource %q", r.Name, p, ref.Resource)
				}
				s.kind = rhsMetadata
				s.resource = ref.Resource
				s.statusPath = "metadata.name"
				s.optional = false
				s.guard = ""
				s.targetType = targetType
				if isMap {
					s.targetType = "string"
				}
				s.rawExpr = fmt.Sprintf("$xr-%s", ref.Resource)
				rhs = fmt.Sprintf("{{ $xr }}-%s", ref.Resource)
				return s, rhs, "", nil
			}

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
			s.targetType = targetType
			if isMap {
				s.targetType = "string"
			}
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
			wireDecl := chain[len(chain)-1]
			refName := strings.Join(segs, ".")

			if branch {
				return s, "", "", fmt.Errorf("resource %q field %q is an object; a from: wire renders one scalar and cannot fill it — wire its individual children (e.g. %s.<key>), or set the whole node with raw:", r.Name, p, p)
			}
			if targetType == "array" {
				return s, "", "", fmt.Errorf("resource %q field %q is an array, and a from: wire cannot render a list in v1 — a scalar parameter renders one scalar, and array parameters are not supported. Use value: with comma-separated entries, or raw: for literal YAML", r.Name, p)
			}
			if targetType == "map" && !isMap {
				if wireDecl.Type == "object" {
					g := chainGuard(segs, chain)
					s.kind = rhsParam
					s.param = chainRef
					s.paramSegs = segs
					s.optional = g != ""
					s.guard = g
					s.rawExpr = fmt.Sprintf("$spec.%s", refName)
					s.targetType = "object"
					return s, "", g, nil
				}
				return s, "", "", fmt.Errorf("resource %q field %q is a map, and a from: wire cannot render one in v1. Set it with raw:", r.Name, p)
			}

			if !isFieldTypeCompatible(targetType, wireDecl.Type, isMap) {
				return s, "", "", fmt.Errorf("resource %q field %q has type %q in the CRD schema, but parameter %q has type %q — the wire would render a YAML scalar of the wrong type, which the API server rejects on apply", r.Name, p, targetType, refName, wireDecl.Type)
			}

			g := chainGuard(segs, chain)
			s.kind = rhsParam
			s.param = chainRef
			s.paramSegs = segs
			s.optional = g != ""
			s.guard = g
			s.rawExpr = fmt.Sprintf("$spec.%s", refName)
			s.targetType = targetType
			if isMap {
				s.targetType = "string"
			}
			// An IntOrString target reaches here as targetType "string" (the
			// tree's normalization), so the plain string rule would quote it.
			// Quote only when the wire really is a string: a quoted IntOrString
			// is a port NAME to the API server, and a numeric name is refused.
			intIntoIntOrString := isIntOrStringNode(node) && wireDecl.Type == "integer"
			if intIntoIntOrString {
				s.targetType = "integer"
			}
			if (s.targetType == "string" || isMap) && !intIntoIntOrString {
				rhs = fmt.Sprintf("{{ $spec.%s | quote }}", refName)
			} else {
				rhs = fmt.Sprintf("{{ $spec.%s }}", refName)
			}
			guard = g
		}
	}
	return s, rhs, guard, nil
}

// isIntOrStringNode reports whether a schema leaf is a Kubernetes IntOrString.
// BuildTree gives such a leaf type "string" — the one spelling legal for both
// halves — but the format survives resolution, and it is the only thing that
// separates IntOrString from a genuine string. The distinction matters on the
// wire: the API server reads a QUOTED IntOrString as a name (a Service
// targetPort of "8080" is rejected with "must contain at least one letter"),
// so a numeric source has to render as a bare scalar. Quantity carries no
// format and stays quoted, which is correct for it.
func isIntOrStringNode(node *schema.Node) bool {
	return node != nil && node.Format == "int-or-string"
}

func isIntegerLiteral(v string) bool {
	_, err := strconv.ParseInt(v, 10, 64)
	return err == nil
}

func isFieldTypeCompatible(targetType, paramType string, isMap bool) bool {
	if isMap {
		return paramType == "string" || paramType == "integer" || paramType == "number" || paramType == "boolean"
	}
	switch targetType {
	case "string":
		return paramType == "string" || paramType == "integer" || paramType == "number" || paramType == "boolean"
	case "integer":
		return paramType == "integer"
	case "number":
		return paramType == "number" || paramType == "integer"
	case "boolean":
		return paramType == "boolean"
	case "":
		return true
	}
	return false
}
