// This file renders a resource's Crossplane-native spec envelope: the
// computed defaults (today, providerConfigRef derived from providerName)
// merged with the blueprint's own envelope entries, validated against the
// resolved CRD variant's ACTUAL envelope schema (schema.CRD.Envelope — never
// a hard-coded field list, because the namespaced .m. and cluster-scoped
// variants differ structurally: the .m. envelope has no deletionPolicy, its
// providerConfigRef is {kind, name} not {name, policy}, and its
// writeConnectionSecretToRef carries name only, no namespace).
package emit

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// checkEnvelopePaths resolves every envelope path for r against the resolved
// CRD's envelope schema (spec.properties minus forProvider/initProvider),
// erroring on any path the schema does not define — the same silent-pruning
// defence checkFieldPaths gives forProvider paths, against the other half of
// the spec. It returns a path -> node map covering every branch and leaf, so
// planEnvelope can type-check each entry against the node it targets.
//
// A resource with no envelope entries returns (nil, nil) without touching
// the CRD at all: an envelope-free blueprint must keep generating exactly as
// it does today, even against a CRD whose envelope schema is missing or odd.
func checkEnvelopePaths(r blueprint.Resource, crd schema.CRD) (map[string]*schema.Node, error) {
	if len(r.Envelope) == 0 {
		return nil, nil
	}
	nodes, err := crd.Envelope()
	if err != nil {
		return nil, fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
	}

	known := make(map[string]*schema.Node)
	walkEnvelopeNodes(nodes, "", known)

	leaves := schema.Leaves(nodes, "")
	suggestions := make([]string, 0, len(leaves))
	for _, l := range leaves {
		suggestions = append(suggestions, l.Path)
	}

	paths := make([]string, 0, len(r.Envelope))
	for p := range r.Envelope {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic: the same blueprint names the same path first
	for _, p := range paths {
		if known[p] != nil {
			continue
		}
		first, _, _ := strings.Cut(p, ".")
		// Defense in depth: Generate always validates first, and Validate
		// already refuses providerConfigRef envelope entries — but
		// Composition is exported, so the rule is enforced here too rather
		// than depending on every caller's discipline.
		if first == "providerConfigRef" {
			return nil, fmt.Errorf("resource %q: envelope %q: providerConfigRef cannot be set via the "+
				"envelope — it is derived from the required providerName parameter, the single source "+
				"of truth for which ProviderConfig a composed resource binds to", r.Name, p)
		}
		// deletionPolicy is the known structural difference between the two
		// variants, so its absence gets a message naming that difference
		// instead of a generic unknown-path one: the namespaced .m. envelope
		// genuinely does not carry it (the cluster-scoped variant does), and
		// an emitted deletionPolicy would be silently pruned on apply.
		if first == "deletionPolicy" && crd.Namespaced() {
			return nil, fmt.Errorf("resource %q: envelope %q: the namespaced (.m.) %s variant's envelope "+
				"has no deletionPolicy — that field exists only on the cluster-scoped variant, and the "+
				"API server would silently prune it from a namespaced resource on apply. Remove the "+
				"entry (namespaced managed resources are cleaned up through their XR)", r.Name, p, crd.Kind)
		}
		if s := closestPath(p, suggestions); s != "" {
			return nil, fmt.Errorf("resource %q: envelope %q is not in %s's spec envelope; did you mean %q? "+
				"(an unknown field is silently pruned by the API server on apply, so it must be "+
				"caught here)", r.Name, p, crd.Kind, s)
		}
		return nil, fmt.Errorf("resource %q: envelope %q is not in %s's spec envelope "+
			"(an unknown field is silently pruned by the API server on apply, so it must be "+
			"caught here)", r.Name, p, crd.Kind)
	}
	return known, nil
}

// walkEnvelopeNodes records every node — branch and leaf — under its dotted
// path. Array-of-object children get the same "[0]" element addressing
// schema.Leaves uses; blueprint validation rejects bracketed envelope
// segments, so those paths exist only for parity, never to be matched.
func walkEnvelopeNodes(nodes []*schema.Node, prefix string, out map[string]*schema.Node) {
	for _, n := range nodes {
		path := n.Name
		if prefix != "" {
			path = prefix + "." + n.Name
		}
		out[path] = n
		if len(n.Children) == 0 {
			continue
		}
		childPrefix := path
		if n.Type == "array" {
			childPrefix += "[0]"
		}
		walkEnvelopeNodes(n.Children, childPrefix, out)
	}
}

// envField is one envelope entry resolved to a renderable form: the dotted
// path split into segments, the right-hand side, and — for a wired optional
// parameter — the hasKey guard it needs (same semantics as forProviderField).
type envField struct {
	path     []string
	rhs      string
	optional bool
	param    string
}

// planEnvelope resolves r.Envelope into a deterministic, path-sorted plan,
// type-checking every entry against the schema node it targets (nodes comes
// from checkEnvelopePaths, so every path is known to resolve).
//
// The type rules, per node:
//
//   - branch (an object with properties, or an array of objects): raw only.
//     It is the whole-subtree escape hatch, written verbatim as single-line
//     YAML; value has no defined rendering for a composite, and from cannot
//     render one (Validate refuses composite parameters behind from).
//   - array leaf (scalar items, e.g. managementPolicies): value is a
//     comma-separated list rendered as a flow sequence of quoted strings;
//     raw passes verbatim; from is refused — a scalar parameter renders one
//     scalar, and array parameters do not exist in M1, so no wire can render
//     a list. This is the documented v1 ruling (see blueprint.Resource).
//   - map leaf (additionalProperties): raw only, same reasoning as a branch.
//   - scalar leaf: value is parsed per the node's declared type and emitted
//     canonically (an integer as an integer, not a quoted string — the
//     API server would reject '2048' against type: integer), strings via
//     quoteYAML; from requires the parameter's type to be compatible with
//     the node's, so a wire cannot render a YAML scalar of the wrong type.
func planEnvelope(r blueprint.Resource, b *blueprint.Blueprint, nodes map[string]*schema.Node) ([]envField, error) {
	paths := make([]string, 0, len(r.Envelope))
	for p := range r.Envelope {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	plan := make([]envField, 0, len(paths))
	for _, p := range paths {
		f := r.Envelope[p]
		n := nodes[p]
		branch := len(n.Children) > 0 || n.Type == "object"

		e := envField{path: strings.Split(p, ".")}
		switch {
		case f.Raw != "":
			e.rhs = f.Raw
		case f.Value != "":
			rhs, err := envelopeValueRHS(n, branch, f.Value)
			if err != nil {
				return nil, fmt.Errorf("resource %q: envelope %q: %w", r.Name, p, err)
			}
			e.rhs = rhs
		case f.From != "":
			param := strings.TrimPrefix(f.From, "params.")
			decl, ok := b.Spec.XRD.Parameters[param]
			if !ok {
				return nil, fmt.Errorf("resource %q envelope %q: unknown parameter %q", r.Name, p, param)
			}
			switch {
			case n.Type == "array":
				return nil, fmt.Errorf("resource %q: envelope %q is an array, and a from: wire cannot "+
					"render a list in v1 — a scalar parameter renders one scalar, and array parameters "+
					"are not supported. Use value: with comma-separated entries, or raw: for literal "+
					"YAML", r.Name, p)
			case branch:
				return nil, fmt.Errorf("resource %q: envelope %q is an object; a from: wire renders one "+
					"scalar and cannot fill it — wire its individual children (e.g. %s.<key>), or set "+
					"the whole node with raw:", r.Name, p, p)
			case n.Type == "map":
				return nil, fmt.Errorf("resource %q: envelope %q is a map, and a from: wire cannot "+
					"render one in v1. Set it with raw:", r.Name, p)
			}
			if !envelopeTypeCompatible(n.Type, decl.Type) {
				return nil, fmt.Errorf("resource %q: envelope %q has type %q in the CRD schema, but "+
					"parameter %q has type %q — the wire would render a YAML scalar of the wrong type, "+
					"which the API server rejects on apply", r.Name, p, n.Type, param, decl.Type)
			}
			e.rhs = fmt.Sprintf("{{ $spec.%s }}", param)
			if !decl.Required {
				// Same guard rule as planFields: only the XRD's required gate
				// makes an unguarded dereference safe under missingkey=error.
				// (A defaulted parameter is also always present after schema
				// defaulting, but the guard is kept for consistency with
				// forProvider fields — harmless when the key is present.)
				e.optional, e.param = true, param
			}
		}
		plan = append(plan, e)
	}

	// A top-level envelope key required by the spec schema must not be
	// omittable: if every entry under it is optional, an XR that sets none of
	// the wired parameters would omit the key entirely (that is how optional
	// groups render, see writeEnvelopeNode) and the API server would reject
	// the resource — a generated artifact that can never apply for a
	// perfectly valid XR. Caught at generation time instead.
	for i := 0; i < len(plan); {
		key := plan[i].path[0]
		allOptional := true
		j := i
		for ; j < len(plan) && plan[j].path[0] == key; j++ {
			if !plan[j].optional {
				allOptional = false
			}
		}
		if allOptional && nodes[key] != nil && nodes[key].Required {
			return nil, fmt.Errorf("resource %q: envelope %q is required by the kind's spec schema, but "+
				"every blueprint entry under it is wired from an optional parameter — an XR that sets "+
				"none of them would omit the key and fail to apply. Wire it from a required parameter "+
				"or give it a value", r.Name, key)
		}
		i = j
	}
	return plan, nil
}

// envelopeValueRHS renders a value entry for the node it targets. Errors are
// wrapped by the caller with the resource and path context.
func envelopeValueRHS(n *schema.Node, branch bool, value string) (string, error) {
	if branch {
		if n.Type == "array" {
			return "", fmt.Errorf("it is an array of objects; value's comma-separated form renders " +
				"scalar entries only — set the whole array with raw:")
		}
		return "", fmt.Errorf("it is an object; value has no defined rendering for one — set its " +
			"individual children, or set the whole node with raw:")
	}
	switch n.Type {
	case "array":
		// The documented v1 ruling for scalar-item arrays: value is a
		// comma-separated list, one flow sequence of quoted strings out.
		// Order is the author's own — a list's order can be semantic.
		parts := strings.Split(value, ",")
		quoted := make([]string, len(parts))
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return "", fmt.Errorf("value %q has an empty entry — it renders as a comma-separated "+
					"list, e.g. \"Observe, Create\"", value)
			}
			quoted[i] = quoteYAML(part)
		}
		return "[" + strings.Join(quoted, ", ") + "]", nil
	case "map":
		return "", fmt.Errorf("it is a map; value has no defined rendering for one — set it with raw:")
	case "integer":
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("value %q is not a valid integer (the CRD schema declares type: integer)", value)
		}
		return strconv.FormatInt(i, 10), nil
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("value %q is not a valid finite number (the CRD schema declares type: number)", value)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	case "boolean":
		if value != "true" && value != "false" {
			return "", fmt.Errorf(`value %q is not a valid boolean (must be "true" or "false"; the CRD `+
				"schema declares type: boolean)", value)
		}
		return value, nil
	default:
		// string, or an untyped node: a quoted scalar is always safe.
		return quoteYAML(value), nil
	}
}

// envelopeTypeCompatible reports whether a parameter of paramType can be
// wired onto a scalar envelope node of nodeType without rendering a YAML
// scalar the schema rejects. An untyped node constrains nothing.
func envelopeTypeCompatible(nodeType, paramType string) bool {
	switch nodeType {
	case "":
		return true
	case "string":
		return paramType == "string"
	case "integer":
		return paramType == "integer"
	case "number":
		return paramType == "number" || paramType == "integer"
	case "boolean":
		return paramType == "boolean"
	}
	return false
}

// writeEnvelope emits the resource's spec envelope: the blueprint's entries
// and the computed providerConfigRef, merged in sorted top-level key order so
// the emitted document is deterministic and an envelope-free blueprint
// renders byte-identically to before (just the providerConfigRef block).
//
// providerConfigRef itself is always the computed block — Validate and
// checkEnvelopePaths both refuse envelope entries for it — and is only
// emitted for the namespaced envelope, whose shape ({kind, name}) is the one
// this generator renders; blueprint.Validate refuses scope: Cluster outright.
func writeEnvelope(d *Doc, ti int, plan []envField, wantNamespaced bool) {
	wrotePCR := false
	for i := 0; i < len(plan); {
		key := plan[i].path[0]
		if wantNamespaced && !wrotePCR && key > "providerConfigRef" {
			writeProviderConfigRef(d, ti)
			wrotePCR = true
		}
		var group []envField
		j := i
		for ; j < len(plan) && plan[j].path[0] == key; j++ {
			e := plan[j]
			e.path = e.path[1:]
			group = append(group, e)
		}
		writeEnvelopeNode(d, ti+1, key, group, "")
		i = j
	}
	if wantNamespaced && !wrotePCR {
		writeProviderConfigRef(d, ti)
	}
}

// writeProviderConfigRef is the computed envelope default: the v2 namespaced
// envelope requires both kind and name here; the cluster-scoped variant
// instead takes {name, policy}. The name dereference is unguarded because
// blueprint.Validate requires providerName to be a required parameter.
func writeProviderConfigRef(d *Doc, ti int) {
	d.Line(ti, "  providerConfigRef:")
	d.Line(ti, "    kind: ClusterProviderConfig")
	d.Line(ti, "    name: {{ $spec.providerName }}")
}

// writeEnvelopeNode emits one envelope key and everything under it.
//
// Guarding: a subtree whose every entry is wired from an optional parameter
// is omitted ENTIRELY — key line included — when none of those parameters is
// present on the XR, by wrapping the whole block in a hasKey guard. Unlike
// forProvider (which spec.required forces to exist, hence writeMapField's
// `{}` fallback), an envelope key the blueprint left conditional has no
// business rendering empty: `writeConnectionSecretToRef: {}` would fail the
// CRD's own required-children rule at apply time, and a bare `key:` is a
// null. A subtree containing at least one unconditional entry always renders
// its key; its optional descendants keep their individual guards.
//
// inheritedGuard suppresses an exact duplicate of an enclosing wrapper (a
// single optional wire would otherwise be guarded twice, once around the
// group and once around its own line); a subtree needing a DIFFERENT guard
// than its parent's still gets its own, because the parent's disjunction
// passing does not imply this subtree's own parameters are present.
func writeEnvelopeNode(d *Doc, level int, key string, entries []envField, inheritedGuard string) {
	allOptional := true
	var params []string
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.optional {
			allOptional = false
		} else if !seen[e.param] {
			seen[e.param] = true
			params = append(params, e.param)
		}
	}

	guard := ""
	if allOptional {
		guard = envGuardExpr(params)
	}
	wrap := guard != "" && guard != inheritedGuard
	if wrap {
		d.Line(level, "{{- if %s }}", guard)
		inheritedGuard = guard
	}

	if len(entries) == 1 && len(entries[0].path) == 0 {
		// The key itself is the assignment. (Multiple entries at one exact
		// path cannot happen: map keys are unique and Validate rejects a
		// path that prefixes another.)
		d.Line(level, "%s: %s", key, entries[0].rhs)
	} else {
		d.Line(level, "%s:", key)
		for i := 0; i < len(entries); {
			childKey := entries[i].path[0]
			var sub []envField
			j := i
			for ; j < len(entries) && entries[j].path[0] == childKey; j++ {
				e := entries[j]
				e.path = e.path[1:]
				sub = append(sub, e)
			}
			writeEnvelopeNode(d, level+1, childKey, sub, inheritedGuard)
			i = j
		}
	}

	if wrap {
		d.Line(level, "{{- end }}")
	}
}

// envGuardExpr renders the presence condition for a set of optional
// parameters: a single hasKey for one, an or-of-hasKeys for several — the
// same hasKey idiom §8 mandates (never `with`, which hard-fails under
// missingkey=error when the key is genuinely absent).
func envGuardExpr(params []string) string {
	if len(params) == 1 {
		return fmt.Sprintf("hasKey $spec %q", params[0])
	}
	conds := make([]string, len(params))
	for i, p := range params {
		conds[i] = fmt.Sprintf("(hasKey $spec %q)", p)
	}
	return "or " + strings.Join(conds, " ")
}
