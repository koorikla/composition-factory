package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Composition renders the Composition for b, resolving each resource's kind
// against crds.
func Composition(b *blueprint.Blueprint, crds []schema.CRD) ([]byte, error) {
	x := b.Spec.XRD
	wantNamespaced := x.Scope == "Namespaced"

	d := NewDoc()
	header(d, "blueprints/"+b.Metadata.Name+".cf.yaml")
	d.Line(0, "apiVersion: apiextensions.crossplane.io/v1")
	d.Line(0, "kind: Composition")
	d.Line(0, "metadata:")
	d.Line(1, "name: %s.%s", x.Plural, x.Group)
	d.Line(0, "spec:")
	d.Line(1, "compositeTypeRef:")
	d.Line(2, "apiVersion: %s/%s", x.Group, x.Version)
	d.Line(2, "kind: %s", x.Kind)
	d.Line(1, "mode: Pipeline")
	d.Line(1, "pipeline:")
	d.Line(1, "- step: render-resources")
	d.Line(2, "functionRef:")
	d.Line(3, "name: function-go-templating")
	d.Line(2, "input:")
	d.Line(3, "apiVersion: gotemplating.fn.crossplane.io/v1beta1")
	d.Line(3, "kind: GoTemplate")
	d.Line(3, "source: Inline")
	// options is a SIBLING of inline, not nested inside it. The function's own
	// README shows it nested; that is a fatal runtime error. Without this
	// option, a missing field renders the literal string "<no value>" into a
	// live managed resource, and because that string is legal YAML the whole
	// validate -> render -> validate pipeline still exits 0.
	d.Line(3, `options: ["missingkey=error"]`)
	d.Line(3, "inline:")
	d.Line(4, "template: |")

	const ti = 5 // template body indent level
	d.Line(ti, "{{- $spec := .observed.composite.resource.spec -}}")
	d.Line(ti, "{{- $xr := .observed.composite.resource.metadata.name -}}")

	for _, r := range b.Spec.Resources {
		crd, err := resolveKind(crds, r.Kind, wantNamespaced)
		if err != nil {
			return nil, err
		}
		apiVersion, err := crd.APIVersion()
		if err != nil {
			return nil, fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
		}
		d.Line(ti, "---")
		d.Line(ti, "apiVersion: %s", apiVersion)
		d.Line(ti, "kind: %s", crd.Kind)
		d.Line(ti, "metadata:")
		d.Line(ti, "  annotations:")
		d.Line(ti, "    {{ setResourceNameAnnotation %q }}", r.Name)
		plan, err := planFields(r, b)
		if err != nil {
			return nil, err
		}
		d.Line(ti, "spec:")
		writeMapField(d, ti, "forProvider", ti+2, plan)
		// The v2 namespaced envelope requires both kind and name here; the
		// cluster-scoped variant instead takes {name, policy}. deletionPolicy
		// is never emitted for a namespaced MR: it is absent from that
		// envelope (0 of 102 EC2 m-variants surveyed carry it) and would be
		// silently pruned by the API server if present.
		if wantNamespaced {
			d.Line(ti, "  providerConfigRef:")
			d.Line(ti, "    kind: ClusterProviderConfig")
			d.Line(ti, "    name: {{ $spec.providerName }}")
		}
	}

	d.Line(1, "- step: auto-ready")
	d.Line(2, "functionRef:")
	d.Line(3, "name: function-auto-ready")
	return d.Bytes(), nil
}

// forProviderField is one blueprint field resolved to a template line,
// ready to write. optional is set (with param carrying the parameter name)
// when the line must be gated on hasKey — see writeMapField.
type forProviderField struct {
	path     string
	rhs      string
	optional bool
	param    string
}

// planFields resolves r.Fields into a deterministic, path-sorted plan,
// erroring on any field that references an unknown parameter. It does not
// write anything: writeMapField needs the full plan up front to decide
// whether the parent key can ever render with zero children.
//
// Quoting: the template body is a YAML block scalar (`template: |`), so the
// outer document never needs escaping for what we write into it — the block
// scalar takes its content literally, whatever quotes or colons appear.
// What matters is the document that block scalar's content becomes once
// function-go-templating renders it: a second, inner YAML document that gets
// applied to the cluster. f.Value is blueprint-authored free text written
// straight into that inner document as a plain scalar, so it gets the same
// treatment as XRD's description/enum values (see yaml.go's quoteYAML): a
// colon makes it invalid, a "#" truncates it, and "yes"/"no"/"1.0" silently
// change type. f.Raw is deliberately NOT quoted — "raw" is the escape hatch
// for a blueprint author who wants literal YAML (a number, a bool, a nested
// map, or even another Go template expression) passed through unprocessed;
// quoting it would force everything through it to render as a string. f.From
// is emitted as a bare `{{ $spec.param }}` template expression, not a
// literal string, so it isn't a quoting candidate here either — its value
// arrives at render time from the composite's own (schema-typed) spec.
func planFields(r blueprint.Resource, b *blueprint.Blueprint) ([]forProviderField, error) {
	paths := make([]string, 0, len(r.Fields))
	for p := range r.Fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	plan := make([]forProviderField, 0, len(paths))
	for _, p := range paths {
		f := r.Fields[p]
		switch {
		case f.Value != "":
			plan = append(plan, forProviderField{path: p, rhs: quoteYAML(f.Value)})
		case f.Raw != "":
			plan = append(plan, forProviderField{path: p, rhs: f.Raw})
		case f.From != "":
			param := strings.TrimPrefix(f.From, "params.")
			decl, ok := b.Spec.XRD.Parameters[param]
			if !ok {
				return nil, fmt.Errorf("resource %q field %q: unknown parameter %q", r.Name, p, param)
			}
			// A bare dereference, correct only because the value is a
			// scalar. Go's template engine formats whatever it finds with
			// fmt, so an object would render as "map[env:prod]" and an
			// array as "[a b c]" -- and "[a b c]" is valid YAML that an
			// items:{type:string} schema accepts as a ONE-element list.
			// blueprint.Validate refuses composite-typed parameters behind
			// a from: for exactly that reason. The M2 fix that lifts the
			// restriction is `{{ $spec.x | toYaml | nindent N }}`, which
			// needs the emitter to know N -- the field's own indent inside
			// the template body -- so it is a change here, not there.
			rhs := fmt.Sprintf("{{ $spec.%s }}", param)
			if decl.Required {
				plan = append(plan, forProviderField{path: p, rhs: rhs})
				continue
			}
			// Optional: gated on hasKey, not `with`. Under
			// options: ["missingkey=error"], `{{- with $spec.foo }}`
			// evaluates the pipeline (indexing the map) before deciding
			// truthiness, so a genuinely absent key — the normal case for
			// an optional field the caller never set — hard-fails the
			// entire render instead of gracefully omitting it. hasKey
			// performs the presence check inside the function argument,
			// sidestepping the template engine's own strict indexing.
			// Direct $spec.field access inside the guarded branch is safe:
			// Go templates never evaluate an untaken branch, and inside the
			// taken one the key provably exists.
			plan = append(plan, forProviderField{path: p, rhs: rhs, optional: true, param: param})
		}
	}
	return plan, nil
}

// writeMapField emits "key:" (or "key: {}") at keyIndent, plus plan's fields
// as children at childIndent.
//
// A parent key whose every child is conditional needs care: if the fields
// were simply written one after another, each behind its own
// {{- if hasKey ... }} guard, an XR that sets none of them would render a
// bare "key:" with nothing under it. YAML parses that as null, not an empty
// mapping, and a structural schema with `type: object` and no
// `nullable: true` rejects an explicit null at apply time.
//
// If plan has no fields at all, the key is known empty at generation time,
// so it's written inline as "key: {}" — no template logic needed. If plan
// has at least one unconditional field (Value, Raw, or a required
// parameter — the XRD gate makes a required field's presence unconditional
// on any valid XR), that field's line always renders regardless of which
// optional fields the XR happens to set, so the key can never end up empty;
// children are written exactly as their individual guards dictate. Only
// when every field in the plan is optional does the whole block need a
// render-time fallback: wrapped in {{- if or (hasKey ...) ... -}} that falls
// back to an explicit {} when none of the optional keys are present.
func writeMapField(d *Doc, keyIndent int, key string, childIndent int, plan []forProviderField) {
	if len(plan) == 0 {
		d.Line(keyIndent, "  %s: {}", key)
		return
	}

	anyGuaranteed := false
	for _, fld := range plan {
		if !fld.optional {
			anyGuaranteed = true
			break
		}
	}

	d.Line(keyIndent, "  %s:", key)

	if anyGuaranteed {
		for _, fld := range plan {
			writeField(d, childIndent, fld)
		}
		return
	}

	// Every field is optional: without this wrapper an XR that sets none of
	// them renders a bare key with nothing under it. See the function
	// comment above.
	conds := make([]string, len(plan))
	for i, fld := range plan {
		conds[i] = fmt.Sprintf("(hasKey $spec %q)", fld.param)
	}
	d.Line(childIndent, "{{- if or %s }}", strings.Join(conds, " "))
	for _, fld := range plan {
		writeField(d, childIndent, fld)
	}
	d.Line(childIndent, "{{- else }}")
	d.Line(childIndent, "{}")
	d.Line(childIndent, "{{- end }}")
}

// writeField emits one resolved field, gated on hasKey when optional.
func writeField(d *Doc, indent int, fld forProviderField) {
	if !fld.optional {
		d.Line(indent, "%s: %s", fld.path, fld.rhs)
		return
	}
	d.Line(indent, "{{- if hasKey $spec %q }}", fld.param)
	d.Line(indent, "%s: %s", fld.path, fld.rhs)
	d.Line(indent, "{{- end }}")
}

// resolveKind finds the CRD for kind, preferring the scope the XRD needs. For a
// Namespaced XRD that is the ".m." group variant; the legacy cluster-scoped one
// has a different spec envelope and its fields get pruned.
func resolveKind(crds []schema.CRD, kind string, wantNamespaced bool) (schema.CRD, error) {
	var fallback *schema.CRD
	for i := range crds {
		c := crds[i]
		if c.Kind != kind || !c.IsManaged() {
			continue
		}
		if c.Namespaced() == wantNamespaced {
			return c, nil
		}
		fallback = &crds[i]
	}
	scope := "cluster-scoped"
	if wantNamespaced {
		scope = "namespaced"
	}
	if fallback != nil {
		return schema.CRD{}, fmt.Errorf("kind %q: no %s variant found (only %s in %s); "+
			"a %s XRD needs the matching variant", kind, scope, fallback.Scope, fallback.Group, scope)
	}
	return schema.CRD{}, fmt.Errorf("kind %q not found in any cached provider; run cf provider add", kind)
}
