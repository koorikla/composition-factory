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
	rhsEnv
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
	sourceType string   // Source type of param/status/env leaf: "string", "integer", "number", "boolean"
	envDefault string   // Declared default for rhsEnv if any
	hasEnvDef  bool     // Whether an environment default was declared
	isByte     bool     // Whether target is a byte/base64 target (Secret data or format: byte)
}

func isByteTarget(node *schema.Node, r blueprint.Resource, p string, isMap bool) bool {
	if node != nil && node.Format == "byte" {
		return true
	}
	if r.Kind == "Secret" {
		basePath, _, _ := blueprint.ParseFieldPath(p)
		if basePath == "data" || strings.HasPrefix(p, "data.") || strings.HasPrefix(p, "data[") || p == "data" {
			return true
		}
	}
	return false
}

// targetCustomNameField returns the custom name field declared on a resource if any.
func targetCustomNameField(targetDecl *blueprint.Resource) (blueprint.Field, bool) {
	if targetDecl == nil {
		return blueprint.Field{}, false
	}
	if f, ok := targetDecl.Fields["metadata.name"]; ok && !isFieldEmpty(f) {
		return f, true
	}
	if f, ok := targetDecl.Fields["name"]; ok && !isFieldEmpty(f) {
		return f, true
	}
	if targetDecl.Envelope != nil {
		if f, ok := targetDecl.Envelope["metadata.name"]; ok && !isFieldEmpty(f) {
			return f, true
		}
		if f, ok := targetDecl.Envelope["name"]; ok && !isFieldEmpty(f) {
			return f, true
		}
	}
	return blueprint.Field{}, false
}

func isFieldEmpty(f blueprint.Field) bool {
	return f.Value == "" && f.From == "" && f.Raw == "" && f.Template == ""
}

func resolveMetadataNameRef(r blueprint.Resource, what string, ref blueprint.FromRef, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool, targetType string, isMap bool, visited map[string]bool) (structuredRHS, string, string, error) {
	var s structuredRHS
	targetDecl := b.ResourceNamed(ref.Resource)
	if targetDecl == nil {
		return s, "", "", fmt.Errorf("resource %q %s: references unknown resource %q", r.Name, what, ref.Resource)
	}
	if targetDecl.ForEach != "" {
		return s, "", "", fmt.Errorf("resource %q %s: resource %q is looped (forEach: %s), so its name is indexed (%s-0, %s-1, ...) -- reference an unlooped resource",
			r.Name, what, ref.Resource, targetDecl.ForEach, ref.Resource, ref.Resource)
	}
	if visited != nil && visited[ref.Resource] {
		return s, "", "", fmt.Errorf("resource %q %s: cycle detected in metadata.name references at resource %q", r.Name, what, ref.Resource)
	}

	if targetField, ok := targetCustomNameField(targetDecl); ok {
		newVisited := make(map[string]bool, len(visited)+1)
		for k, v := range visited {
			newVisited[k] = v
		}
		if r.Name != "" {
			newVisited[r.Name] = true
		}

		targetNode := &schema.Node{Type: "string"}
		sTarget, rhsTarget, guardTarget, err := resolveFieldRHSWithVisited("metadata.name", targetField, *targetDecl, b, crds, wantNamespaced, targetNode, false, newVisited)
		if err != nil {
			return s, "", "", fmt.Errorf("resource %q %s: resolving target %q metadata.name: %w", r.Name, what, ref.Resource, err)
		}
		if isMap {
			sTarget.targetType = "string"
		} else if targetType != "" {
			sTarget.targetType = targetType
		}
		return sTarget, rhsTarget, guardTarget, nil
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
	rhs := fmt.Sprintf("{{ $xr }}-%s", ref.Resource)
	return s, rhs, "", nil
}

// resolveFieldRHS resolves a single blueprint field into its structured form and Go-template RHS/guard.
func resolveFieldRHS(p string, f blueprint.Field, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool, node *schema.Node, isMap bool) (structuredRHS, string, string, error) {
	return resolveFieldRHSWithVisited(p, f, r, b, crds, wantNamespaced, node, isMap, nil)
}

func resolveFieldRHSWithVisited(p string, f blueprint.Field, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool, node *schema.Node, isMap bool, visited map[string]bool) (structuredRHS, string, string, error) {
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
	s.isByte = isByteTarget(node, r, p, isMap)
	isByte := s.isByte

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
		rawNorm := blueprint.NormalizeRawGoTemplate(f.Raw)
		if targetType == "string" && isJSONComposite(f.Raw) {
			rhs = quoteYAML(rawNorm)
		} else {
			rhs = rawNorm
		}

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
				sMeta, rhsMeta, guardMeta, err := resolveMetadataNameRef(r, fmt.Sprintf("field %q", p), ref, b, crds, wantNamespaced, targetType, isMap, visited)
				if err != nil {
					return sMeta, "", "", err
				}
				sMeta.isByte = isByte
				if isByte {
					rhsMeta = fmt.Sprintf("{{ %s | b64enc | quote }}", sMeta.rawExpr)
				}
				return sMeta, rhsMeta, guardMeta, nil
			}

			g, expr, leafType, err := statusWire(ref, r, fmt.Sprintf("field %q", p), b, crds, wantNamespaced)
			if err != nil {
				return s, "", "", err
			}
			if !isFieldTypeCompatible(targetType, leafType, isMap) {
				return s, "", "", fmt.Errorf("resource %q field %q has type %q in the CRD schema, but status path %q has type %q — the wire would render a YAML scalar of the wrong type, which the API server rejects on apply", r.Name, p, targetType, strings.Join(ref.StatusPath, "."), leafType)
			}
			s.kind = rhsStatus
			s.resource = ref.Resource
			s.statusPath = strings.Join(ref.StatusPath, ".")
			s.optional = true
			s.guard = g
			s.rawExpr = expr
			s.targetType = targetType
			s.sourceType = leafType
			if isMap {
				s.targetType = "string"
			}
			intIntoIntOrString := isIntOrStringNode(node) && leafType == "integer"
			if intIntoIntOrString {
				s.targetType = "integer"
			}
			if isByte {
				rhs = fmt.Sprintf("{{ %s | b64enc | quote }}", expr)
			} else if (s.targetType == "string" || isMap) && !intIntoIntOrString {
				rhs = fmt.Sprintf("{{ %s | quote }}", expr)
			} else {
				rhs = "{{ " + expr + " }}"
			}
			guard = g
		} else if ref.Env != "" {
			envDecl, exists := b.Spec.Environment[ref.Env]
			if !exists {
				return s, "", "", blueprint.UnknownEnvKeyError(fmt.Sprintf("resource %q field %q", r.Name, p), ref.Env, b.Spec.Environment)
			}
			if branch {
				return s, "", "", fmt.Errorf("resource %q field %q is an object; a from: wire renders one scalar and cannot fill it — wire its individual children (e.g. %s.<key>), or set the whole node with raw:", r.Name, p, p)
			}
			if targetType == "array" {
				return s, "", "", fmt.Errorf("resource %q field %q is an array, and a from: wire cannot render a list in v1 — a scalar parameter renders one scalar, and array parameters are not supported. Use value: with comma-separated entries, or raw: for literal YAML", r.Name, p)
			}
			if targetType == "map" && !isMap {
				return s, "", "", fmt.Errorf("resource %q field %q is a map, and a from: wire cannot render one in v1. Set it with raw:", r.Name, p)
			}
			if !isFieldTypeCompatible(targetType, envDecl.Type, isMap) {
				return s, "", "", fmt.Errorf("resource %q field %q has type %q in the CRD schema, but environment key %q has type %q — the wire would render a YAML scalar of the wrong type, which the API server rejects on apply", r.Name, p, targetType, ref.Env, envDecl.Type)
			}

			s.kind = rhsEnv
			s.param = ref.Env
			s.paramSegs = []string{ref.Env}
			s.targetType = targetType
			s.sourceType = envDecl.Type
			if isMap {
				s.targetType = "string"
			}
			intIntoIntOrString := isIntOrStringNode(node) && envDecl.Type == "integer"
			if intIntoIntOrString {
				s.targetType = "integer"
			}

			if envDecl.Default != "" {
				s.hasEnvDef = true
				s.envDefault = envDecl.Default
				defVal := formatEnvDefault(envDecl)
				expr := fmt.Sprintf("default %s (index $env %q)", defVal, ref.Env)
				s.optional = false
				s.guard = ""
				s.rawExpr = expr
				if isByte {
					rhs = fmt.Sprintf("{{ %s | b64enc | quote }}", expr)
				} else if (s.targetType == "string" || isMap) && !intIntoIntOrString {
					rhs = fmt.Sprintf("{{ %s | quote }}", expr)
				} else {
					rhs = fmt.Sprintf("{{ %s }}", expr)
				}
				guard = ""
			} else {
				g := fmt.Sprintf("hasKey $env %q", ref.Env)
				s.optional = true
				s.guard = g
				s.rawExpr = fmt.Sprintf("$env.%s", ref.Env)
				if isByte {
					rhs = fmt.Sprintf("{{ $env.%s | b64enc | quote }}", ref.Env)
				} else if (s.targetType == "string" || isMap) && !intIntoIntOrString {
					rhs = fmt.Sprintf("{{ $env.%s | quote }}", ref.Env)
				} else {
					rhs = fmt.Sprintf("{{ $env.%s }}", ref.Env)
				}
				guard = g
			}
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
			s.sourceType = wireDecl.Type
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
			if isByte {
				rhs = fmt.Sprintf("{{ $spec.%s | b64enc | quote }}", refName)
			} else if (s.targetType == "string" || isMap) && !intIntoIntOrString {
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

// isJSONComposite reports whether s is a JSON object or array literal
// (e.g. `{"Version": ...}` or `["s3:GetObject"]`) rather than a Go-template
// expression (such as `{{ $xr }}`). When targeted at a string-typed schema
// field, emitting such literals unquoted causes YAML to treat them as inline
// mappings or sequences, failing CRD schema validation.
func isJSONComposite(s string) bool {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") && !strings.HasPrefix(trimmed, "{{") {
		return true
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return true
	}
	return false
}
