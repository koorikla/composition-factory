// This file renders a resource's blueprint-authored metadata.annotations.
// Annotations sit in the SHARED half of the composed document — the
// metadata block the emitter writes before the native/managed fork — so one
// planner and one writer serve both families identically; that is what makes
// the grammar legal on a native ServiceAccount and a managed Role alike.
//
// Two rendering rules specific to this surface, both following from the fact
// that an annotation value is ALWAYS a string:
//
//   - Every from: wire (params.<name> or resources.<name>.status.<path>) is
//     piped through sprig's quote, never interpolated bare the way a field
//     wire is. A field's schema declares its type, so a bare integer
//     interpolation is correct there; here the API server requires a string,
//     and an unquoted `count: 3` would land as a YAML integer — legal in the
//     rendered document, rejected at apply. quote turns any scalar into a
//     double-quoted string with Go-syntax escaping, which is valid YAML
//     double-quoted style for everything checkScalar admits.
//   - Keys and literal values are quoteYAML'd: annotation keys carry dots
//     and slashes as a matter of course, and always quoting both (rather
//     than deciding per key) keeps the output byte-deterministic with one
//     written form.
//
// Guard discipline is exactly the field surface's: an optional parameter
// wire gets a hasKey guard, a status wire gets the full hasKey/kindIs chain
// over $.observed.resources, and a guarded entry that does not fire omits
// the KEY cleanly — never an empty-valued annotation, never "<no value>".
// The annotations block itself can never render empty, because the
// unconditional setResourceNameAnnotation line always precedes these
// entries (see Composition).
package emit

import (
	"fmt"
	"sort"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// planAnnotations resolves r.Annotations into a deterministic, key-sorted
// plan (reusing forProviderField: path holds the annotation key). Template
// references and wires are resolved with the same machinery fields use —
// templateCallRHS and statusWire — so the two surfaces cannot drift.
//
// Validate has already checked key grammar and value forms; the checks
// repeated here are the ones this layer owes anyway (the CRD half of a
// status wire) plus the cheap defensive ones the codebase repeats wherever
// Composition, being exported, can be reached without Generate's Validate
// call (the reserved-key collision and the unknown-template case — both
// would otherwise ship silently-wrong output, this project's central defect
// class).
func planAnnotations(r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) ([]forProviderField, error) {
	keys := make([]string, 0, len(r.Annotations))
	for k := range r.Annotations {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	plan := make([]forProviderField, 0, len(keys))
	for _, k := range keys {
		if blueprint.ReservedAnnotationKey(k) {
			return nil, fmt.Errorf("resource %q annotation %q: this key is the composition-resource-name "+
				"annotation, written by the generator itself via setResourceNameAnnotation (node identity); "+
				"a blueprint entry for it would silently collide with the function-set value (see "+
				"blueprint.Validate)", r.Name, k)
		}
		f := r.Annotations[k]
		switch {
		case f.Value != "":
			plan = append(plan, forProviderField{path: k, rhs: quoteYAML(f.Value)})
		case f.Raw != "":
			// The raw escape hatch, verbatim as everywhere else. The author
			// owns making it a string in the rendered document.
			plan = append(plan, forProviderField{path: k, rhs: f.Raw})
		case f.Template != "":
			if _, ok := b.Spec.Templates[f.Template]; !ok {
				return nil, fmt.Errorf("resource %q annotation %q: unknown template %q", r.Name, k, f.Template)
			}
			// The same include call fields emit, .field carrying the
			// annotation key. templateFieldNindent is correct here by
			// construction, not coincidence: annotation entries are written at
			// the same inner-document column as forProvider children (both
			// land at column 4, children at 6) — see writeAnnotations.
			plan = append(plan, forProviderField{path: k, rhs: templateCallRHS(f.Template, r.Name, k)})
		case f.From != "":
			ref, err := blueprint.ParseFrom(f.From)
			if err != nil {
				return nil, fmt.Errorf("resource %q annotation %q: %w", r.Name, k, err)
			}
			if ref.Resource != "" {
				guard, expr, err := statusWire(ref, r, fmt.Sprintf("annotation %q", k), b, crds, wantNamespaced)
				if err != nil {
					return nil, err
				}
				// Always conditional, exactly like a field wire: the value
				// does not exist until the source resource is observed, so
				// the KEY is omitted cleanly until then and Crossplane fills
				// it on a later reconcile.
				plan = append(plan, forProviderField{path: k, rhs: "{{ " + expr + " | quote }}", guard: guard})
				continue
			}
			decl, ok := b.Spec.XRD.Parameters[ref.Param]
			if !ok {
				return nil, fmt.Errorf("resource %q annotation %q: unknown parameter %q", r.Name, k, ref.Param)
			}
			rhs := fmt.Sprintf("{{ $spec.%s | quote }}", ref.Param)
			if decl.Required {
				plan = append(plan, forProviderField{path: k, rhs: rhs})
				continue
			}
			// Optional: the same hasKey guard planFields emits, for the same
			// missingkey=error reason — and the same clean omission: an XR
			// that never set the parameter renders no entry for this key.
			plan = append(plan, forProviderField{path: k, rhs: rhs, guard: fmt.Sprintf("hasKey $spec %q", ref.Param)})
		}
	}
	return plan, nil
}

// writeAnnotations emits the planned entries as children of the
// metadata.annotations block Composition has already opened, at the same
// literal "    " offset its setResourceNameAnnotation line uses — inner-
// document column 4, the same column forProvider children occupy, which is
// what lets templateCallRHS's fixed nindent serve both surfaces. Guard lines
// wrap exactly as writeField's do; keys are always quoteYAML'd (dots and
// slashes are the norm here, and one written form keeps the bytes
// deterministic).
func writeAnnotations(d *Doc, ti int, plan []forProviderField) {
	for _, fld := range plan {
		if fld.guard != "" {
			d.Line(ti, "    {{- if %s }}", fld.guard)
		}
		d.Line(ti, "    %s: %s", quoteYAML(fld.path), fld.rhs)
		if fld.guard != "" {
			d.Line(ti, "    {{- end }}")
		}
	}
}
