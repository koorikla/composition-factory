package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

// validateResourceEnvelope checks one resource's envelope entries: the checks
// that need no CRD. Path existence and type checks against the kind's actual
// envelope schema happen at emit time (internal/emit), which is the layer
// that holds the resolved CRD — the same split Fields has between this file's
// structural checks and emit's checkFieldPaths.
//
// Called from Validate inside the resources loop, so errors keep the
// first-problem contract and name the resource. Paths are visited in sorted
// order for the same reason the fields loop sorts: the same blueprint must
// name the same problem first, every time.
func validateResourceEnvelope(x XRD, r Resource) error {
	paths := make([]string, 0, len(r.Envelope))
	for p := range r.Envelope {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		// Every path segment becomes a raw, unquoted YAML map key in the
		// emitted Composition (the same position a forProvider field path
		// occupies), so each gets the identifier discipline parameter names
		// get: camelCase shape, no YAML keywords, and — implied by the
		// character class — no brackets, colons, or control characters. This
		// also rules out indexed segments like "policies[0]": array-of-object
		// envelope fields are not addressable per-element in v1 (set the
		// whole array with raw on its key instead).
		for _, seg := range strings.Split(p, ".") {
			if !paramNameRE.MatchString(seg) || yamlKeywords[strings.ToLower(seg)] {
				return fmt.Errorf("resource %q envelope %q: segment %q is not a valid envelope key "+
					"(must be camelCase, e.g. writeConnectionSecretToRef, and not a YAML keyword "+
					"like yes/no/true/false)", r.Name, p, seg)
			}
		}

		// providerConfigRef is derived from the required providerName
		// parameter — the Composition renders it for every composed resource
		// (internal/emit/composition.go). An envelope entry for it would be a
		// second source of truth that silently diverges from the one the XRD
		// already gates, so it is refused at the source.
		if first, _, _ := strings.Cut(p, "."); first == "providerConfigRef" {
			return fmt.Errorf("resource %q envelope %q: providerConfigRef cannot be set via the "+
				"envelope — it is derived from the required providerName parameter, which is the "+
				"single source of truth for which ProviderConfig a composed resource binds to. "+
				"Change the providerName parameter (or the XR's value for it) instead", r.Name, p)
		}

		f := r.Envelope[p]
		set := 0
		for _, v := range []string{f.From, f.Value, f.Raw} {
			if v != "" {
				set++
			}
		}
		if set != 1 {
			return fmt.Errorf("resource %q envelope %q: set exactly one of from, value or raw (got %d)",
				r.Name, p, set)
		}

		// Same single-line discipline as resource fields, for the same
		// reason: value and raw land inside the Composition's `template: |`
		// block scalar, where a line break escapes every enclosing context.
		// See the fields loop in load.go and checkScalar's doc comment.
		for _, src := range []struct{ label, val string }{
			{"from", f.From}, {"raw", f.Raw}, {"value", f.Value},
		} {
			if err := checkScalar(fmt.Sprintf("resource %q envelope %q: %s", r.Name, p, src.label), src.val); err != nil {
				return err
			}
		}

		if f.From != "" {
			param, ok := strings.CutPrefix(f.From, "params.")
			if !ok {
				// Deliberately narrower than a field's from: a cross-resource
				// status wire (resources.<name>.status.<path>) is not supported
				// in envelope entries in v1. The envelope planner
				// (internal/emit/envelope.go) models a wire as an optional
				// PARAMETER (its guard is a hasKey over $spec), not as the
				// observed-resources hasKey chain a status reference needs, so
				// admitting the grammar here would emit a guard that can never
				// be true. Refused at the source until the planner learns the
				// chain.
				return fmt.Errorf("resource %q envelope %q: from must start with params. (got %q) -- "+
					"cross-resource status wires are supported in fields:, not in envelope entries, in v1",
					r.Name, p, f.From)
			}
			decl, exists := x.Parameters[param]
			if !exists {
				return fmt.Errorf("resource %q envelope %q: references unknown parameter %q",
					r.Name, p, param)
			}
			// Same rule as a field's from, same failure mode: a bare
			// {{ $spec.x }} renders a composite with Go's fmt ("map[k:v]",
			// "[a b c]") — legal YAML, silently wrong.
			if compositeTypes[decl.Type] {
				return fmt.Errorf("resource %q envelope %q: parameter %q has type %q, and a from: "+
					"mapping cannot render a composite value in M1 — it emits a bare "+
					"{{ $spec.%s }}, which Go's template engine formats with fmt. "+
					"Use a scalar parameter, or set the entry with raw:",
					r.Name, p, param, decl.Type, param)
			}
		}
	}

	// No path may be a proper dotted prefix of another: "a" (set whole) and
	// "a.b" (set one child) both define the node "a", and the emitter would
	// have to pick one silently. Sorted order makes adjacency sufficient:
	// segments are alphanumeric (checked above) and '.' sorts before every
	// alphanumeric rune, so every path extending p with "." sorts immediately
	// after p, before any sibling like "ab".
	for i := 0; i+1 < len(paths); i++ {
		if strings.HasPrefix(paths[i+1], paths[i]+".") {
			return fmt.Errorf("resource %q envelope: %q and %q conflict — one sets the whole node the "+
				"other sets a child of; keep one of the two", r.Name, paths[i], paths[i+1])
		}
	}
	return nil
}
