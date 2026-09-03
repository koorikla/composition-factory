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
	// A native Kubernetes kind (blueprint provider "k8s") has no Crossplane
	// envelope AT ALL: the composed object is not a managed resource, so
	// there is no managementPolicies, writeConnectionSecretToRef or
	// providerConfigRef to set — schema.CRD.Envelope honestly returns an
	// empty tree for it, which would make every entry an unknown path below.
	// Refused explicitly instead, with the reason, rather than letting the
	// generic unknown-path message imply a typo the author could fix.
	if crd.Native {
		return nil, fmt.Errorf("resource %q: kind %q is a native Kubernetes kind, and a native object "+
			"has no Crossplane envelope — it is the composed object itself, not a managed resource, "+
			"so there is no managementPolicies, writeConnectionSecretToRef or providerConfigRef to "+
			"set. Remove the envelope block; every settable field is addressable through fields: "+
			"(e.g. spec.template.spec.containers[0].image)", r.Name, r.Kind)
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
		first, _, _ := strings.Cut(p, ".")
		if known[p] != nil {
			continue
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
func walkNodes(nodes []*schema.Node, prefix string, out map[string]*schema.Node) {
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
		walkNodes(n.Children, childPrefix, out)
	}
}

func walkEnvelopeNodes(nodes []*schema.Node, prefix string, out map[string]*schema.Node) {
	walkNodes(nodes, prefix, out)
}

// envField is one envelope entry resolved to a renderable form: the dotted
// path split into segments, the right-hand side, and — for a wired optional
// parameter or object member — the guard expression it needs (same
// semantics as forProviderField.guard: a hasKey for an optional parameter,
// a memberGuard shape for a typed object member).
type envField struct {
	path       []string
	rhs        string
	optional   bool
	guard      string
	structured structuredRHS
}

// planEnvelope resolves r.Envelope into a deterministic, path-sorted plan,
// type-checking every entry against the schema node it targets (nodes comes
// from checkEnvelopePaths, so every path is known to resolve).
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
			if flowMap, ok := blueprint.ParseFlowStyleMap(f.Raw); ok && branch {
				keys := make([]string, 0, len(flowMap))
				for k := range flowMap {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					v := flowMap[k]
					plan = append(plan, envField{
						path: append(strings.Split(p, "."), k),
						rhs:  quoteYAML(v),
						structured: structuredRHS{
							kind:       rhsLiteral,
							value:      v,
							targetType: "string",
						},
					})
				}
				continue
			}
			e.rhs = blueprint.NormalizeRawGoTemplate(f.Raw)
			e.structured = structuredRHS{kind: rhsRaw, value: f.Raw}
		case f.Value != "":
			rhs, err := envelopeValueRHS(n, branch, f.Value)
			if err != nil {
				return nil, fmt.Errorf("resource %q: envelope %q: %w", r.Name, p, err)
			}
			e.rhs = rhs
			e.structured = structuredRHS{kind: rhsLiteral, value: f.Value, targetType: n.Type}
		case f.From != "":
			if envKey, ok := blueprint.EnvRef(f.From); ok {
				envDecl, exists := b.Spec.Environment[envKey]
				if !exists {
					return nil, blueprint.UnknownEnvKeyError(fmt.Sprintf("resource %q envelope %q", r.Name, p), envKey, b.Spec.Environment)
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
				if !envelopeTypeCompatible(n.Type, envDecl.Type) {
					return nil, fmt.Errorf("resource %q: envelope %q has type %q in the CRD schema, but "+
						"environment key %q has type %q — the wire would render a YAML scalar of the wrong type, "+
						"which the API server rejects on apply", r.Name, p, n.Type, envKey, envDecl.Type)
				}
				if envDecl.Default != "" {
					defVal := formatEnvDefault(envDecl)
					expr := fmt.Sprintf("default %s (index $env %q)", defVal, envKey)
					if n.Type == "string" {
						e.rhs = fmt.Sprintf("{{ %s | quote }}", expr)
					} else {
						e.rhs = fmt.Sprintf("{{ %s }}", expr)
					}
					e.optional = false
					e.guard = ""
					e.structured = structuredRHS{
						kind:       rhsEnv,
						param:      envKey,
						paramSegs:  []string{envKey},
						rawExpr:    expr,
						targetType: n.Type,
						optional:   false,
						guard:      "",
					}
				} else {
					deref := "$env." + envKey
					if n.Type == "string" {
						e.rhs = fmt.Sprintf("{{ %s | quote }}", deref)
					} else {
						e.rhs = fmt.Sprintf("{{ %s }}", deref)
					}
					g := fmt.Sprintf("hasKey $env %q", envKey)
					e.optional, e.guard = true, g
					e.structured = structuredRHS{
						kind:       rhsEnv,
						param:      envKey,
						paramSegs:  []string{envKey},
						rawExpr:    deref,
						targetType: n.Type,
						optional:   true,
						guard:      g,
					}
				}
				plan = append(plan, e)
				continue
			}
			param, member, _ := blueprint.ParamRef(f.From)
			chainRef := param
			if member != "" {
				chainRef = param + "." + member
			}
			segs, chain, err := blueprint.ParamChain(b.Spec.XRD,
				fmt.Sprintf("resource %q envelope %q", r.Name, p), chainRef)
			if err != nil {
				return nil, err
			}
			wireDecl := chain[len(chain)-1]
			refName := strings.Join(segs, ".")
			deref := "$spec." + refName
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
			if !envelopeTypeCompatible(n.Type, wireDecl.Type) {
				return nil, fmt.Errorf("resource %q: envelope %q has type %q in the CRD schema, but "+
					"parameter %q has type %q — the wire would render a YAML scalar of the wrong type, "+
					"which the API server rejects on apply", r.Name, p, n.Type, refName, wireDecl.Type)
			}
			if n.Type == "string" {
				e.rhs = fmt.Sprintf("{{ %s | quote }}", deref)
			} else {
				e.rhs = fmt.Sprintf("{{ %s }}", deref)
			}
			e.structured = structuredRHS{
				kind:       rhsParam,
				param:      chainRef,
				paramSegs:  segs,
				rawExpr:    deref,
				targetType: n.Type,
			}
			switch {
			case member != "":
				if g := chainGuard(segs, chain); g != "" {
					e.optional, e.guard = true, g
					e.structured.optional = true
					e.structured.guard = g
				}
			case !chain[0].Required:
				g := fmt.Sprintf("hasKey $spec %q", param)
				e.optional, e.guard = true, g
				e.structured.optional = true
				e.structured.guard = g
			}
		}
		plan = append(plan, e)
	}

	// If providerConfigRef is partially configured in envelope, default kind or name appropriately.
	hasPCRName := false
	hasPCRKind := false
	hasPCROther := false
	for _, e := range plan {
		if len(e.path) > 0 && e.path[0] == "providerConfigRef" {
			if len(e.path) == 2 && e.path[1] == "name" {
				hasPCRName = true
			} else if len(e.path) == 2 && e.path[1] == "kind" {
				hasPCRKind = true
			} else {
				hasPCROther = true
			}
		}
	}
	if (hasPCRName || hasPCROther) && !hasPCRKind {
		plan = append(plan, envField{
			path: []string{"providerConfigRef", "kind"},
			rhs:  "ClusterProviderConfig",
			structured: structuredRHS{
				kind:  rhsLiteral,
				value: "ClusterProviderConfig",
			},
		})
	}
	if hasPCRKind && !hasPCRName {
		plan = append(plan, envField{
			path: []string{"providerConfigRef", "name"},
			rhs:  "{{ $spec.providerName }}",
			structured: structuredRHS{
				kind:      rhsParam,
				param:     "providerName",
				paramSegs: []string{"providerName"},
				rawExpr:   "$spec.providerName",
			},
		})
	}
	sort.Slice(plan, func(i, j int) bool {
		return strings.Join(plan[i].path, ".") < strings.Join(plan[j].path, ".")
	})

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
		return "", fmt.Errorf("it is an object; value cannot render a composite — set its individual "+
			"children (e.g. %s.<key>), or set the whole node with raw:", n.Name)
	}
	switch n.Type {
	case "string":
		return quoteYAML(value), nil
	case "integer":
		i, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("value %q is not a valid integer: %w", value, err)
		}
		return strconv.FormatInt(i, 10), nil
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("value %q is not a valid number (NaN and Inf are refused)", value)
		}
		return value, nil
	case "boolean":
		switch strings.ToLower(value) {
		case "true", "false":
			return strings.ToLower(value), nil
		default:
			return "", fmt.Errorf("value %q is not a valid boolean (use true or false)", value)
		}
	case "array":
		entries := strings.Split(value, ",")
		quoted := make([]string, 0, len(entries))
		for _, e := range entries {
			s := strings.TrimSpace(e)
			if s == "" {
				return "", fmt.Errorf("value %q contains an empty entry (check for trailing/double commas)", value)
			}
			quoted = append(quoted, quoteYAML(s))
		}
		return "[" + strings.Join(quoted, ", ") + "]", nil
	}
	return "", fmt.Errorf("unsupported schema type %q", n.Type)
}

// envelopeTypeCompatible checks whether a parameter type can safely fill a
// schema node. (Identical to typeCompatible in composition.go — kept local to
// avoid coupling the two planning passes.)
func envelopeTypeCompatible(nodeType, paramType string) bool {
	switch nodeType {
	case "string":
		return paramType == "string"
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

// writeEnvelope emits the resource's spec envelope: the blueprint's entries
// and the computed providerConfigRef, merged in sorted top-level key order so
// the emitted document is deterministic and an envelope-free blueprint
// renders byte-identically to before (just the providerConfigRef block).
//
// providerConfigRef defaults to {kind: ClusterProviderConfig, name: {{ $spec.providerName }}}
// for namespaced envelopes, but can be customized per resource in the envelope.
func writeEnvelope(d *Doc, ti int, plan []envField, wantNamespaced bool) {
	wrotePCR := false
	hasPCRInPlan := false
	for _, f := range plan {
		if len(f.path) > 0 && f.path[0] == "providerConfigRef" {
			hasPCRInPlan = true
			break
		}
	}

	for i := 0; i < len(plan); {
		key := plan[i].path[0]
		if wantNamespaced && !wrotePCR && !hasPCRInPlan && key > "providerConfigRef" {
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
		if key == "providerConfigRef" {
			wrotePCR = true
		}
		i = j
	}
	if wantNamespaced && !wrotePCR && !hasPCRInPlan {
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
	var guards []string
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.optional {
			allOptional = false
		} else if !seen[e.guard] {
			seen[e.guard] = true
			guards = append(guards, e.guard)
		}
	}

	guard := ""
	if allOptional {
		guard = orGuards(guards)
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

// orGuards renders the presence condition for a set of distinct entry
// guards: the guard itself for one, an or over each (parenthesized) for
// several — the same hasKey idiom §8 mandates (never `with`, which
// hard-fails under missingkey=error when the key is genuinely absent). A
// guard is a whole boolean expression (a plain hasKey, or a memberGuard
// conjunction), so the or-form parenthesizes each one; for plain
// parameters the output is byte-identical to what the pre-member
// or-of-hasKeys wrote.
func orGuards(guards []string) string {
	if len(guards) == 1 {
		return guards[0]
	}
	conds := make([]string, len(guards))
	for i, g := range guards {
		conds[i] = "(" + g + ")"
	}
	return "or " + strings.Join(conds, " ")
}
