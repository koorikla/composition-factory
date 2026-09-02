package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
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

	// Blueprint-declared steps surround the built-in templating step:
	// before-steps here, after-steps at the bottom, declaration order
	// preserved within each side. A blueprint that declares no pipeline gets
	// the default auto-ready step (see effectivePipeline).
	beforeSteps, afterSteps := splitPipeline(effectivePipeline(b))
	for _, s := range beforeSteps {
		if err := writePipelineStep(d, s); err != nil {
			return nil, err
		}
	}

	d.Line(1, "- step: %s", blueprint.TemplatingStepName)
	d.Line(2, "functionRef:")
	d.Line(3, "name: %s", blueprint.TemplatingFunctionName)
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

	// User-defined templates head the template body as {{- define }} blocks,
	// in sorted name order (determinism). The lines come from
	// blueprint.TemplateBlockLines — the SAME assembly Validate parsed under
	// the real engine contract, so what was validated is what ships. Every
	// declared template is emitted whether or not anything calls it: a define
	// block renders nothing by itself, and pruning "unused" ones would make
	// emission depend on reference analysis it does not need.
	tmplNames := make([]string, 0, len(b.Spec.Templates))
	for n := range b.Spec.Templates {
		tmplNames = append(tmplNames, n)
	}
	sort.Strings(tmplNames)
	for _, n := range tmplNames {
		for _, line := range blueprint.TemplateBlockLines(n, b.Spec.Templates[n]) {
			d.Line(ti, "%s", line)
		}
	}

	d.Line(ti, "{{- $spec := .observed.composite.resource.spec -}}")
	d.Line(ti, "{{- $xr := .observed.composite.resource.metadata.name -}}")

	for _, r := range b.Spec.Resources {
		crd, err := resolveKind(crds, r, wantNamespaced)
		if err != nil {
			return nil, err
		}
		apiVersion, err := crd.APIVersion()
		if err != nil {
			return nil, fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
		}
		if err := checkFieldPaths(r, crd); err != nil {
			return nil, err
		}
		// Envelope paths are checked against the resolved variant's ACTUAL
		// envelope schema (see envelope.go) — the namespaced .m. and
		// cluster-scoped envelopes differ structurally, so this cannot be a
		// hard-coded list.
		envNodes, err := checkEnvelopePaths(r, crd)
		if err != nil {
			return nil, err
		}
		if err := checkStatusRefs(r, b, crds, wantNamespaced); err != nil {
			return nil, err
		}
		// forEach wraps the resource's WHOLE document in a range over the
		// loop count, so every line below — separator, envelope,
		// providerConfigRef — repeats per iteration; fields render exactly
		// as they do outside a loop. The count is an integer XRD parameter
		// (params.<name>: blueprint.Validate pins the type to integer and
		// requires required-or-default, which is what makes the bare $spec
		// dereference below safe under missingkey=error: a required key is
		// present on any admitted XR, and a defaulted one is injected by the
		// API server's schema defaulting before the function runs) or another
		// resource's observed status integer (resources.<name>.status.<path>
		// — see the guarded form below). The (int ...) cast is load-bearing,
		// not defensive:
		// function-go-templating receives the observed composite over
		// protobuf, whose Struct type carries every number as a float64, and
		// sprig's until takes an int — text/template converts between
		// integer kinds but never float64 → int, so `until $spec.n` is a
		// render-time error. sprig's int (cast.ToInt) handles the float64.
		//
		// `range $i := ...` binds $i to the ELEMENT, not the index — but
		// until yields [0 1 ... n-1], so the element IS the iteration index.
		// when wraps the resource's WHOLE document — OUTSIDE the forEach
		// range, so a false condition skips every iteration (and the range's
		// own loop-bound dereference) in one test rather than testing an
		// invariant condition once per iteration. The dereference is bare
		// and unguarded on purpose: blueprint.Validate pins the parameter to
		// required-or-default (the same rule as the loop bound below), which
		// is what makes it safe under missingkey=error.
		conditional := r.When != ""
		if conditional {
			cond, err := whenCondition(r.When)
			if err != nil {
				return nil, fmt.Errorf("resource %q: %w", r.Name, err)
			}
			d.Line(ti, "{{- if %s }}", cond)
		}
		// The loop bound comes in two forms. params.<name> dereferences the
		// composite's spec bare (safe: Validate pins it required-or-default).
		// resources.<name>.status.<path> is OBSERVED state — it does not
		// exist until the source resource has been observed reporting that
		// leaf (first reconcile, a fresh XR, `crossplane composition render`
		// with no --observed-resources) — so the WHOLE range wraps in the
		// same hasKey/kindIs guard chain a status wire uses, and an
		// unobserved source fans out to ZERO instances. That semantic is
		// load-bearing, not an edge case: NOTHING of the looped resource
		// exists until the cluster says how many, and Crossplane creates the
		// instances on a later reconcile once the count is observed —
		// exactly how a status wire's field materialises late, lifted from
		// one field to a whole document set. The alternative (hard-failing
		// like the params form) would make every fresh XR unrenderable,
		// because no XRD gate can make observed state unconditional.
		looped := r.ForEach != ""
		loopGuarded := false
		if looped {
			if ref, perr := blueprint.ParseFrom(r.ForEach); perr == nil && ref.Resource != "" {
				guard, expr, err := forEachStatusBound(ref, r, b, crds, wantNamespaced)
				if err != nil {
					return nil, err
				}
				d.Line(ti, "{{- if %s }}", guard)
				d.Line(ti, "{{- range $i := until (int %s) }}", expr)
				loopGuarded = true
			} else {
				d.Line(ti, "{{- range $i := until (int $spec.%s) }}", strings.TrimPrefix(r.ForEach, "params."))
			}
		}
		d.Line(ti, "---")
		d.Line(ti, "apiVersion: %s", apiVersion)
		d.Line(ti, "kind: %s", crd.Kind)
		d.Line(ti, "metadata:")
		d.Line(ti, "  annotations:")
		if looped {
			// Indexed, per the §8 rule: the composition-resource-name
			// annotation is how Crossplane keys composed resources, so a
			// constant name inside a range collapses every iteration into
			// ONE resource — silently, since the collapsed document is
			// legal. r.Name is a validated DNS label (resourceNameRE), so
			// interpolating it bare inside the printf format is safe.
			d.Line(ti, `    {{ setResourceNameAnnotation (printf "%s-%%d" $i) }}`, r.Name)
		} else {
			d.Line(ti, "    {{ setResourceNameAnnotation %q }}", r.Name)
		}
		// Conventions fill in matching fields the blueprint does NOT set
		// explicitly; an explicit field always wins — that IS the override
		// mechanism. The merge happens on a copy, never on r.Fields itself.
		// Native resources are exempt by design: their top-level leaves are
		// structural (Secret.type, ConfigMap.data), never convention targets.
		fields := r.Fields
		if r.Provider != blueprint.NativeProvider {
			var cerr error
			fields, cerr = conventionFields(r, b, crd)
			if cerr != nil {
				return nil, cerr
			}
		}
		rc := r
		rc.Fields = fields
		plan, err := planFields(rc, b, crds, wantNamespaced)
		if err != nil {
			return nil, err
		}
		// A native Kubernetes kind takes the whole other branch of the
		// envelope fork: apiVersion/kind/metadata/spec ARE the composed
		// object, so its (object-rooted) field paths render as a real nested
		// tree and NOTHING in the managed branch below — no forProvider, no
		// providerConfigRef, no managementPolicies — applies to it. A native
		// object is not a managed resource: it has no provider credentials
		// to reference, and a Crossplane envelope key would be silently
		// pruned by the API server, the exact defect class checkFieldPaths
		// exists to close. Both branches rejoin BELOW the fork for the
		// looped/conditional {{- end }} lines: the range and if wrappers
		// opened above enclose the whole document either way, so a native
		// resource inside a forEach or behind a when closes its blocks
		// exactly as a managed one does.
		if crd.Native {
			// template: fields are refused on native resources (v1 ruling, see
			// blueprint.Validate): templateCallRHS re-indents its output to the
			// fixed forProvider column (templateFieldNindent), which a native
			// leaf at an arbitrary nesting depth breaks. Enforced here too
			// because Composition is exported.
			for _, fld := range plan {
				if f, ok := rc.Fields[fld.path]; ok && f.Template != "" {
					return nil, fmt.Errorf("resource %q field %q: template: fields are not supported on "+
						"native Kubernetes kind %q in v1 -- the template call's output re-indents to the "+
						"fixed forProvider column, which a native field at an arbitrary nesting depth "+
						"breaks (see blueprint.Validate)", r.Name, fld.path, r.Kind)
				}
			}
			if err := writeNativeFields(d, ti, r.Name, plan); err != nil {
				return nil, err
			}
		} else {
			d.Line(ti, "spec:")
			// M1 assumes a forProvider-shaped spec envelope for MANAGED
			// resources, and this is the one place that assumption is written
			// down as code (native kinds branch off above, before any envelope
			// exists to assume).
			//
			// Global Constraint 8 says never hard-code the managed-resource spec
			// envelope: compute `envelope = spec.properties - {forProvider,
			// initProvider}` and render what remains from its own schema. That is
			// deliberately DEFERRED TO M2. internal/schema already computes it
			// (CRD.Envelope), but rendering an arbitrary envelope subtree means
			// deciding, per node, what a blueprint may set and what the generator
			// must supply, which is a design problem M1 does not need to solve to
			// generate a single upjet managed resource.
			//
			// What M1 does instead is refuse to guess. checkFieldPaths above
			// errors when the resolved CRD has no forProvider at all (a real
			// case: provider-kubernetes' ObservedObjectCollection), rather than
			// emitting a `forProvider: {}` this resource has no such field for
			// and letting the API server prune the lot. The only other envelope
			// key emitted below, providerConfigRef, is likewise hard-coded to the
			// v2 namespaced shape -- which is why blueprint.Validate refuses
			// scope: Cluster outright rather than letting a cluster-scoped
			// blueprint through this function.
			writeMapField(d, ti, "forProvider", ti+2, plan)
			// The rest of the spec envelope: blueprint-authored entries merged
			// with the computed providerConfigRef (namespaced only — the v2
			// namespaced envelope requires both kind and name there; the
			// cluster-scoped variant instead takes {name, policy}). deletionPolicy
			// is never emitted for a namespaced MR: it is absent from that
			// envelope (0 of 102 EC2 m-variants surveyed carry it), and
			// checkEnvelopePaths rejects a blueprint that asks for it. An
			// envelope-free resource renders byte-identically to before this
			// grammar existed: just the providerConfigRef block.
			envPlan, err := planEnvelope(r, b, envNodes)
			if err != nil {
				return nil, err
			}
			writeEnvelope(d, ti, envPlan, wantNamespaced)
		}
		if looped {
			d.Line(ti, "{{- end }}")
			if loopGuarded {
				// The observed-bound guard closes AFTER the range and before
				// any when: condition-outside-guard-outside-range, mirroring
				// the opening order above.
				d.Line(ti, "{{- end }}")
			}
		}
		if conditional {
			d.Line(ti, "{{- end }}")
		}
	}

	for _, s := range afterSteps {
		if err := writePipelineStep(d, s); err != nil {
			return nil, err
		}
	}
	return d.Bytes(), nil
}

// checkFieldPaths resolves every blueprint field path for r against the
// resolved kind's settable-field schema — spec.forProvider for a managed
// resource, the object's own vendored schema for a native kind — erroring on
// any path the schema does not define.
//
// This closes a silent-wrongness path of exactly the kind this project
// treats as its central defect class. A typo'd `visibiltyTimeout` used to be
// emitted verbatim: the Composition is valid YAML, `crossplane composition
// render` renders it happily (it does not schema-check composed resources),
// every gate exits 0 -- and then the API server silently PRUNES the unknown
// field on apply. The queue comes up with a default visibility timeout and
// nothing anywhere says why. Checking here, against the provider's own
// schema, is the only layer that can catch it: blueprint.Validate does not
// have the CRDs, and the API server does not report what it pruned.
//
// It is also what makes internal/schema/tree.go's Leaves a live code path
// rather than a tested-but-uncalled package.
//
// Branch paths are accepted, not just leaves. A leaf set alone would reject
// `redrivePolicy: {raw: ...}` -- setting a whole subtree with the raw escape
// hatch is legitimate -- so every dotted ancestor of every leaf is admitted
// too. What is rejected is a path that matches nothing in the schema at any
// depth, which is the typo case.
func checkFieldPaths(r blueprint.Resource, crd schema.CRD) error {
	// FieldTree branches on what "the settable fields" means: the
	// spec.forProvider subtree for a managed resource, the object's own
	// (object-rooted) schema for a native kind — so a native blueprint path
	// like spec.template.spec.containers[0].image validates against the
	// vendored Kubernetes schema through the same known-set logic below.
	nodes, err := crd.FieldTree()
	if err != nil {
		return fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
	}
	if len(nodes) == 0 {
		if crd.Native {
			// Unlike the forProvider case below this is never a legitimate
			// schema shape: every vendored native kind has settable fields,
			// so an empty tree means the vendored subset itself is broken.
			return fmt.Errorf("resource %q: native kind %q resolved to an empty vendored schema; "+
				"this is a defect in the vendored OpenAPI subset (internal/schema/k8s), not in the blueprint", r.Name, r.Kind)
		}
		// Not an internal error: provider-kubernetes' ObservedObjectCollection
		// genuinely has no forProvider. M1 cannot compose such a resource
		// (see the envelope comment in Composition), and saying so is far
		// better than emitting `spec: {forProvider: {}}` against a schema
		// with no such key, which the API server prunes without a word.
		return fmt.Errorf("resource %q: kind %q has no spec.forProvider properties in its CRD; "+
			"M1 can only compose forProvider-shaped managed resources "+
			"(computing the full spec envelope is M2 work)", r.Name, r.Kind)
	}
	where := crd.Kind + " spec.forProvider"
	if crd.Native {
		where = "the native " + crd.Kind + " schema"
	}

	leaves := schema.Leaves(nodes, "")
	known := make(map[string]bool, len(leaves)*2)
	suggestions := make([]string, 0, len(leaves))
	for _, l := range leaves {
		known[l.Path] = true
		suggestions = append(suggestions, l.Path)
		for _, ancestor := range ancestorPaths(l.Path) {
			known[ancestor] = true
		}
	}

	paths := make([]string, 0, len(r.Fields))
	for p := range r.Fields {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic: the same blueprint names the same field first
	for _, p := range paths {
		if known[p] {
			continue
		}
		if s := closestPath(p, suggestions); s != "" {
			return fmt.Errorf("resource %q: field %q is not in %s; did you mean %q? "+
				"(an unknown field is silently pruned by the API server on apply, so it must be "+
				"caught here)", r.Name, p, where, s)
		}
		return fmt.Errorf("resource %q: field %q is not in %s "+
			"(an unknown field is silently pruned by the API server on apply, so it must be "+
			"caught here)", r.Name, p, where)
	}
	return nil
}

// templateFieldNindent is the inner-document column a template call's output
// is re-indented to. Every field line is written at childIndent = ti+2 Doc
// levels, which lands at column 4 of the INNER document (the block scalar
// strips ti levels), so the field's children belong at column 6. nindent
// makes both output shapes land correctly there: a scalar becomes the
// field's next-line value ("name:\n      myqueue" parses as name: myqueue),
// and a multi-line mapping nests under the key. If writeMapField's
// childIndent ever moves, this must move with it.
const templateFieldNindent = 6

// templateCallRHS renders a field's template: <name> as an include call with
// the documented minimal context — .spec (the composite's spec), .xr (its
// metadata.name), .resource (the composed resource's name) and .field (the
// field path being set). trim strips the define block's own leading/trailing
// newlines so nindent's re-indentation is exact for scalars and blocks
// alike. resource is a validated DNS label and field a schema-checked path,
// so the %q interpolations are exact.
func templateCallRHS(name, resource, field string) string {
	return fmt.Sprintf(`{{ include %q (dict "spec" $spec "xr" $xr "resource" %q "field" %q) | trim | nindent %d }}`,
		name, resource, field, templateFieldNindent)
}

// conventionFields merges spec.conventions into r's explicit fields: every
// TOP-LEVEL leaf of the resolved CRD's forProvider (no dots, no array
// indices — a convention names a field, not a subtree path) whose name ends
// with a convention's match, and which the blueprint does not set
// explicitly, gains a template field. The first matching convention in list
// order wins for each leaf; an explicit field of any form wins over every
// convention — that is the override mechanism, so it is a merge rule here
// rather than a special case anywhere else. The receiver's Fields map is
// never mutated.
func conventionFields(r blueprint.Resource, b *blueprint.Blueprint, crd schema.CRD) (map[string]blueprint.Field, error) {
	if len(b.Spec.Conventions) == 0 {
		return r.Fields, nil
	}
	// blueprint.Validate refuses this combination at the source (see the
	// conventions ruling in load.go); kept as a real error rather than a
	// silent skip because Composition is exported and callable on its own —
	// the same discipline checkStatusRefs applies to its unknown-resource
	// case. A silent skip here would be a convention that silently never
	// applies, this project's central defect class.
	if crd.Native {
		return nil, fmt.Errorf("resource %q: spec.conventions cannot apply to native Kubernetes kind %q "+
			"-- a native object has no forProvider plan for a convention to fill (v1 ruling; see "+
			"blueprint.Validate)", r.Name, r.Kind)
	}
	nodes, err := crd.ForProvider()
	if err != nil {
		return nil, fmt.Errorf("resource %q (kind %q): %w", r.Name, r.Kind, err)
	}
	merged := make(map[string]blueprint.Field, len(r.Fields)+len(nodes))
	for k, v := range r.Fields {
		merged[k] = v
	}
	for _, n := range nodes {
		if len(n.Children) > 0 {
			continue // a branch is a subtree, not a settable field
		}
		if _, explicit := merged[n.Name]; explicit {
			continue // explicit wins: that IS the override
		}
		for _, c := range b.Spec.Conventions {
			if strings.HasSuffix(n.Name, c.Match) {
				merged[n.Name] = blueprint.Field{Template: c.Template}
				break
			}
		}
	}
	return merged, nil
}

// whenCondition compiles a validated when expression to the template
// condition it wraps the document in:
//
//	params.x            -> $spec.x
//	params.x == "lit"   -> eq $spec.x "lit"
//	params.x != "lit"   -> ne $spec.x "lit"
//
// The literal is %q-quoted, which for ParseWhen's character class (no '"',
// no '\\', no control runes past checkScalar) is exactly the literal wrapped
// in plain quotes — one written form, byte-deterministic.
func whenCondition(when string) (string, error) {
	param, op, literal, err := blueprint.ParseWhen(when)
	if err != nil {
		return "", err
	}
	switch op {
	case "":
		return fmt.Sprintf("$spec.%s", param), nil
	case "==":
		return fmt.Sprintf("eq $spec.%s %q", param, literal), nil
	default: // "!=", ParseWhen admits nothing else
		return fmt.Sprintf("ne $spec.%s %q", param, literal), nil
	}
}

// statusScalarTypes are the schema node types a cross-resource status
// reference may resolve to. A reference renders as a bare template
// dereference, which Go's template engine formats with fmt — an object would
// render as "map[k:v]" and an array as "[a b c]", both valid YAML and both
// silently wrong (the same defect class blueprint.Validate closes for
// composite parameters behind from:). An untyped node ("") is refused too:
// with no declared type there is nothing to promise about what fmt will
// print.
var statusScalarTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true,
}

// checkStatusRefs resolves every cross-resource status reference on r
// against the referenced kind's own CRD status schema, erroring on any path
// the schema does not define as a scalar leaf.
//
// This is the CRD half of the check blueprint.Validate starts (grammar,
// the resource exists, it is not looped, not self) — the same split as field
// paths, where Validate owns the shape and checkFieldPaths owns the schema.
// It matters for the same reason: a status path the provider never writes is
// not an error anywhere downstream. The guard chain just stays false forever
// and the field silently never materialises — every gate green, a wire that
// never carries a value. The provider's declared status schema is the only
// thing that can catch the typo, and this is the layer that holds it.
func checkStatusRefs(r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) error {
	paths := make([]string, 0, len(r.Fields))
	for p := range r.Fields {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic: the same blueprint names the same field first

	for _, p := range paths {
		target, statusPath, ok := blueprint.StatusRef(r.Fields[p].From)
		if !ok {
			continue
		}
		var targetRes *blueprint.Resource
		for i := range b.Spec.Resources {
			if b.Spec.Resources[i].Name == target {
				targetRes = &b.Spec.Resources[i]
				break
			}
		}
		if targetRes == nil {
			// Validate already refuses this; Generate validates before
			// emitting. Kept as a real error, not a panic, because
			// Composition is exported and callable on its own.
			return fmt.Errorf("resource %q field %q: references unknown resource %q", r.Name, p, target)
		}
		// The TARGET resource resolves through the same path as its own
		// emission — provider-aware, so a native target (provider: k8s)
		// resolves to its vendored schema, whose status subtree
		// (Deployment.status and friends) is as checkable as a managed
		// kind's. A status wire into either family goes through here.
		crd, err := resolveKind(crds, *targetRes, wantNamespaced)
		if err != nil {
			return fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
		}
		nodes, err := crd.Status()
		if err != nil {
			return fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
		}
		if len(nodes) == 0 {
			return fmt.Errorf("resource %q field %q: kind %q declares no status schema in its CRD, "+
				"so resources.%s.status.%s can never carry a value", r.Name, p, crd.Kind, target, statusPath)
		}

		leaves := schema.Leaves(nodes, "")
		var leaf *schema.Leaf
		scalars := make([]string, 0, len(leaves))
		for i := range leaves {
			// Indexed paths (conditions[0].status) are excluded from the
			// suggestion pool: the reference grammar has no array indexing,
			// so suggesting one would suggest something unwritable.
			if statusScalarTypes[leaves[i].Node.Type] && !strings.Contains(leaves[i].Path, "[") {
				scalars = append(scalars, leaves[i].Path)
			}
			if leaves[i].Path == statusPath {
				leaf = &leaves[i]
			}
		}
		if leaf == nil {
			if s := closestPath(statusPath, scalars); s != "" {
				return fmt.Errorf("resource %q field %q: %q is not a scalar leaf in %s's status schema; "+
					"did you mean %q? (a status path the provider never writes would leave the guard "+
					"false forever and the field silently absent, so it must be caught here)",
					r.Name, p, statusPath, crd.Kind, s)
			}
			return fmt.Errorf("resource %q field %q: %q is not a scalar leaf in %s's status schema "+
				"(a status path the provider never writes would leave the guard false forever and "+
				"the field silently absent, so it must be caught here)", r.Name, p, statusPath, crd.Kind)
		}
		if !statusScalarTypes[leaf.Node.Type] {
			return fmt.Errorf("resource %q field %q: status path %q in %s has type %q, and a from: "+
				"reference can only carry a scalar (string, integer, number, boolean) -- Go's template "+
				"engine would format a composite with fmt, producing valid YAML that is silently wrong",
				r.Name, p, statusPath, crd.Kind, leaf.Node.Type)
		}
	}
	return nil
}

// ancestorPaths returns every proper dotted prefix of a leaf path, with any
// "[0]" array index both kept and stripped, so that "containers[0].image"
// admits both "containers[0]" and "containers".
func ancestorPaths(path string) []string {
	var out []string
	segments := strings.Split(path, ".")
	for i := 1; i < len(segments); i++ {
		prefix := strings.Join(segments[:i], ".")
		out = append(out, prefix)
		if trimmed, found := strings.CutSuffix(prefix, "[0]"); found {
			out = append(out, trimmed)
		}
	}
	return out
}

// closestPath returns the candidate nearest to path by edit distance, or ""
// when nothing is close enough to be worth suggesting. The threshold is
// deliberately tight: a wrong suggestion on a typo is worse than none,
// because it invites a second blind edit.
func closestPath(path string, candidates []string) string {
	best, bestDist := "", 0
	for _, c := range candidates {
		d := editDistance(path, c)
		if best == "" || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" || bestDist > 3 || bestDist*2 >= len(path) {
		return ""
	}
	return best
}

// editDistance is Levenshtein distance over runes, two rows at a time.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// forProviderField is one blueprint field resolved to a template line,
// ready to write. guard, when non-empty, is the template condition the line
// must be gated on ({{- if <guard> }} ... {{- end }}) — a hasKey check for an
// optional parameter, or a hasKey/kindIs chain over $.observed.resources for a
// cross-resource status reference. An empty guard means the line always
// renders — see writeMapField.
type forProviderField struct {
	path  string
	rhs   string
	guard string
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
func planFields(r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) ([]forProviderField, error) {
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
		case f.Template != "":
			if _, ok := b.Spec.Templates[f.Template]; !ok {
				return nil, fmt.Errorf("resource %q field %q: unknown template %q", r.Name, p, f.Template)
			}
			plan = append(plan, forProviderField{path: p, rhs: templateCallRHS(f.Template, r.Name, p)})
		case f.From != "":
			ref, err := blueprint.ParseFrom(f.From)
			if err != nil {
				return nil, fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
			}
			if ref.Resource != "" {
				guard, rhs, err := statusWire(ref, r, p, b, crds, wantNamespaced)
				if err != nil {
					return nil, err
				}
				// A status wire is ALWAYS conditional: the value does not
				// exist until the referenced resource has been observed
				// (first reconcile, a fresh XR, `crossplane composition
				// render` with no --observed-resources), so the field is
				// omitted while unobserved and Crossplane fills it on a
				// later reconcile.
				plan = append(plan, forProviderField{path: p, rhs: rhs, guard: guard})
				continue
			}
			// ref.Param carries everything after params., member half
			// included; ParamRef splits the two for the member wire below.
			param, member, _ := blueprint.ParamRef(f.From)
			decl, ok := b.Spec.XRD.Parameters[param]
			if !ok {
				return nil, fmt.Errorf("resource %q field %q: unknown parameter %q", r.Name, p, param)
			}
			// A member wire (params.<name>.<member>) dereferences one level
			// into a typed object parameter. Its guard shape follows the two
			// requiredness gates (see memberGuard); the dereference itself is
			// $spec.<param>.<member>, evaluated only where the guard chain has
			// proven every key on the path. Enforced here as well as in
			// blueprint.Validate because Composition is exported — an unknown
			// member reaching the template would hard-fail every render.
			if member != "" {
				mdecl, ok := decl.Properties[member]
				if !ok {
					return nil, fmt.Errorf("resource %q field %q: references unknown member %q of "+
						"parameter %q", r.Name, p, member, param)
				}
				plan = append(plan, forProviderField{
					path:  p,
					rhs:   fmt.Sprintf("{{ $spec.%s.%s }}", param, member),
					guard: memberGuard(param, member, decl.Required, mdecl.Required),
				})
				continue
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
			plan = append(plan, forProviderField{path: p, rhs: rhs, guard: fmt.Sprintf("hasKey $spec %q", param)})
		}
	}
	return plan, nil
}

// memberGuard is the render-time condition for a typed object member wire
// (params.<param>.<member>), mirroring the forProvider guard discipline
// under options: ["missingkey=error"]:
//
//   - object optional: a hasKey chain on BOTH levels — the object key can be
//     genuinely absent (an XR that never set it), and when present the
//     member can still be absent, so each dereference is proven first. The
//     conjunction short-circuits (text/template's and, since Go 1.18), so
//     $spec.<param> is never indexed while the first link is false.
//   - object required, member optional: hasKey on the member alone — the
//     XRD's required gate makes $spec.<param> present on any admitted XR,
//     so indexing it inside the hasKey argument is safe.
//   - both required: no guard. Required within a required object means the
//     API server admits no XR without the member, exactly the gate that
//     makes a top-level required parameter's bare dereference safe.
//
// A defaulted member behind a present object is also injected by schema
// defaulting, but the guard is kept for any non-required member — the same
// consistency rule planFields applies to defaulted top-level parameters
// (harmless when the key is present).
func memberGuard(param, member string, paramRequired, memberRequired bool) string {
	switch {
	case !paramRequired:
		return fmt.Sprintf("and (hasKey $spec %q) (hasKey $spec.%s %q)", param, param, member)
	case !memberRequired:
		return fmt.Sprintf("hasKey $spec.%s %q", param, member)
	default:
		return ""
	}
}

// scalarStatusTypes are the status-leaf types a wire may carry. Object,
// array and map leaves are refused because a bare interpolation renders
// Go's fmt of the composite ("map[k:v]", "[a b c]") — valid YAML, silently
// wrong — the same rule blueprint.Validate applies to composite parameters
// behind from:. An untyped leaf ({} in the schema) is refused too: it
// cannot be proven scalar, and guessing is how fmt-of-a-map reaches a live
// resource.
var scalarStatusTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true,
}

// statusWire resolves one resources.<name>.status.<path> wire for field p on
// resource r: it checks the path against the SOURCE resource's CRD status
// schema (the only layer that can — blueprint.Validate has no CRDs) and
// returns the render-time guard condition plus the value expression.
//
// The path must resolve to a scalar LEAF of the status tree. The wired
// value itself is interpolated bare, exactly like a params wire: its type
// is schema-checked here at generation time, and the observed value arrives
// from the provider's own controller conforming to that same CRD — the
// pre-existing, documented quoting decision for from: values (see
// planFields), not a new one.
func statusWire(ref blueprint.FromRef, r blueprint.Resource, p string, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) (guard, rhs string, err error) {
	var src *blueprint.Resource
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == ref.Resource {
			src = &b.Spec.Resources[i]
			break
		}
	}
	if src == nil {
		// Validate refuses this before any emitter runs; kept as a defensive
		// error because planFields is reachable from in-memory blueprints.
		return "", "", fmt.Errorf("resource %q field %q: references the status of unknown resource %q",
			r.Name, p, ref.Resource)
	}
	crd, err := resolveKind(crds, *src, wantNamespaced)
	if err != nil {
		return "", "", fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
	}
	nodes, err := crd.Status()
	if err != nil {
		return "", "", fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
	}
	if len(nodes) == 0 {
		return "", "", fmt.Errorf("resource %q field %q: kind %q declares no status schema in its CRD; "+
			"nothing can be wired from resource %q's status", r.Name, p, src.Kind, ref.Resource)
	}

	path := strings.Join(ref.StatusPath, ".")
	leaves := schema.Leaves(nodes, "")
	branches := make(map[string]bool, len(leaves))
	suggestions := make([]string, 0, len(leaves))
	var leaf *schema.Node
	for _, l := range leaves {
		suggestions = append(suggestions, l.Path)
		if l.Path == path {
			leaf = l.Node
		}
		for _, ancestor := range ancestorPaths(l.Path) {
			branches[ancestor] = true
		}
	}
	switch {
	case leaf == nil && branches[path]:
		return "", "", fmt.Errorf("resource %q field %q: status path %q on %s addresses an object "+
			"subtree, not a scalar leaf -- interpolating it would render Go's fmt of the map, "+
			"which is valid YAML and silently wrong", r.Name, p, path, crd.Kind)
	case leaf == nil:
		if s := closestPath(path, suggestions); s != "" {
			return "", "", fmt.Errorf("resource %q field %q: status path %q is not in %s's status "+
				"schema; did you mean %q?", r.Name, p, path, crd.Kind, s)
		}
		return "", "", fmt.Errorf("resource %q field %q: status path %q is not in %s's status schema",
			r.Name, p, path, crd.Kind)
	case !scalarStatusTypes[leaf.Type]:
		typ := leaf.Type
		if typ == "" {
			typ = "untyped"
		}
		return "", "", fmt.Errorf("resource %q field %q: status path %q on %s is %s, and a wire can "+
			"only carry a scalar (string, integer, number, boolean) -- a composite would render "+
			"Go's fmt of the value", r.Name, p, path, crd.Kind, typ)
	}

	guard, expr := statusGuard(ref.Resource, ref.StatusPath)
	return guard, "{{ " + expr + " }}", nil
}

// forEachCountTypes are the status-leaf types an observed loop bound
// (forEach: resources.<name>.status.<path>) may resolve to. A loop bound
// COUNTS something, so only integer and number qualify — number included
// because protojson delivers every numeric value as float64 anyway and upjet
// providers declare many genuinely-integral fields as number; the (int ...)
// cast in the range head handles both. A string is refused even though
// cast.ToInt could parse one: the count would then ride on string parsing of
// arbitrary provider output, silently looping zero times on anything
// non-numeric. Maps, arrays, untyped leaves and object branches have no
// defensible count at all.
var forEachCountTypes = map[string]bool{
	"integer": true, "number": true,
}

// forEachStatusBound resolves an observed loop bound for resource r: it
// checks ref's status path against the SOURCE resource's CRD status schema
// (the only layer that can — blueprint.Validate has no CRDs, same split as
// statusWire) and returns the render-time guard chain plus the bare count
// expression the range head dereferences. The guard is the same
// hasKey/kindIs chain a status wire uses, because the failure mode is the
// same: nothing on the $.observed.resources chain exists until the source is
// observed, and under missingkey=error every unguarded dereference of a
// missing key hard-fails the whole render. Behind the guard the semantic is
// deliberate and documented at the emit site: an unobserved source fans out
// to ZERO instances.
func forEachStatusBound(ref blueprint.FromRef, r blueprint.Resource, b *blueprint.Blueprint, crds []schema.CRD, wantNamespaced bool) (guard, expr string, err error) {
	var src *blueprint.Resource
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == ref.Resource {
			src = &b.Spec.Resources[i]
			break
		}
	}
	if src == nil {
		// Validate refuses this before any emitter runs; kept as a defensive
		// error because Composition is exported and callable on its own.
		return "", "", fmt.Errorf("resource %q: forEach references the status of unknown resource %q",
			r.Name, ref.Resource)
	}
	crd, err := resolveKind(crds, *src, wantNamespaced)
	if err != nil {
		return "", "", fmt.Errorf("resource %q: forEach: %w", r.Name, err)
	}
	nodes, err := crd.Status()
	if err != nil {
		return "", "", fmt.Errorf("resource %q: forEach: %w", r.Name, err)
	}
	if len(nodes) == 0 {
		return "", "", fmt.Errorf("resource %q: kind %q declares no status schema in its CRD, "+
			"so forEach: %s can never carry a count and the resource would silently never fan out",
			r.Name, src.Kind, r.ForEach)
	}

	path := strings.Join(ref.StatusPath, ".")
	leaves := schema.Leaves(nodes, "")
	suggestions := make([]string, 0, len(leaves))
	var leaf *schema.Node
	for _, l := range leaves {
		// Only countable leaves enter the suggestion pool: suggesting a
		// string (or an indexed conditions[0] path the grammar cannot even
		// write) would suggest something unusable as a loop bound.
		if forEachCountTypes[l.Node.Type] && !strings.Contains(l.Path, "[") {
			suggestions = append(suggestions, l.Path)
		}
		if l.Path == path {
			leaf = l.Node
		}
	}
	switch {
	case leaf == nil:
		if s := closestPath(path, suggestions); s != "" {
			return "", "", fmt.Errorf("resource %q: forEach status path %q is not an integer or number "+
				"leaf in %s's status schema; did you mean %q? (a status path the provider never writes "+
				"would leave the guard false forever — a fan-out that silently never happens — so it "+
				"must be caught here)", r.Name, path, crd.Kind, s)
		}
		return "", "", fmt.Errorf("resource %q: forEach status path %q is not an integer or number "+
			"leaf in %s's status schema (a status path the provider never writes would leave the "+
			"guard false forever — a fan-out that silently never happens — so it must be caught here)",
			r.Name, path, crd.Kind)
	case !forEachCountTypes[leaf.Type]:
		typ := leaf.Type
		if typ == "" {
			typ = "untyped"
		}
		return "", "", fmt.Errorf("resource %q: forEach status path %q on %s is %s, and a loop bound "+
			"must be an integer or number status leaf — it renders as until (int <observed value>), "+
			"a repetition count", r.Name, path, crd.Kind, typ)
	}

	guard, expr = statusGuard(ref.Resource, ref.StatusPath)
	return guard, expr, nil
}

// statusGuard builds the render-time guard for a status dereference, plus
// the BARE value expression it protects (callers wrap it: a status wire as
// an interpolation, an observed forEach bound inside its range head). The
// value lives at
// $.observed.resources.<name>.resource.status.<path> — but nothing on that
// chain is guaranteed to exist (the whole resources key is absent until
// something is observed, and each level below appears as the resource
// converges), and under missingkey=error every unguarded dereference of a
// missing key is a hard render failure. So the guard is a single short-
// circuiting `and` (text/template evaluates and/or lazily — measured, not
// assumed) that proves each level with `hasKey` before the next level
// touches it, exactly the spec §8 idiom; `{{- with }}` is banned there for
// hard-failing the same case.
//
// Two mechanics that are invisible in the output but load-bearing:
//
//   - The resource name is dereferenced with `index`, never `.name`: a
//     composition-resource-name is a DNS label and may carry dashes, which
//     template field-access syntax cannot address.
//   - Each descent below the entry also proves `kindIs "map"` before the
//     next hasKey, because hasKey's argument is typed map[string]any and a
//     nil (or non-map) intermediate is a "wrong type for value" execution
//     error, not a false — measured against the real engine. A
//     half-populated or hand-written observed fixture (`status:` with
//     nothing under it) must degrade to "field omitted", never to a failed
//     render of the whole composition.
func statusGuard(resName string, segs []string) (guard, expr string) {
	entry := fmt.Sprintf("(index $.observed.resources %q)", resName)
	conds := []string{
		`(hasKey $.observed "resources")`,
		`(kindIs "map" $.observed.resources)`,
		fmt.Sprintf("(hasKey $.observed.resources %q)", resName),
		fmt.Sprintf(`(kindIs "map" %s)`, entry),
		fmt.Sprintf(`(hasKey %s "resource")`, entry),
	}
	cur := entry + ".resource"
	for _, seg := range append([]string{"status"}, segs...) {
		conds = append(conds,
			fmt.Sprintf(`(kindIs "map" %s)`, cur),
			fmt.Sprintf("(hasKey %s %q)", cur, seg))
		cur += "." + seg
	}
	return "(and " + strings.Join(conds, " ") + ")", cur
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
		if fld.guard == "" {
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
		conds[i] = "(" + fld.guard + ")"
	}
	d.Line(childIndent, "{{- if or %s }}", strings.Join(conds, " "))
	for _, fld := range plan {
		writeField(d, childIndent, fld)
	}
	d.Line(childIndent, "{{- else }}")
	d.Line(childIndent, "{}")
	d.Line(childIndent, "{{- end }}")
}

// writeField emits one resolved field, gated on its guard when non-empty.
func writeField(d *Doc, indent int, fld forProviderField) {
	if fld.guard != "" {
		d.Line(indent, "{{- if %s }}", fld.guard)
	}
	d.Line(indent, "%s: %s", fld.path, fld.rhs)
	if fld.guard != "" {
		d.Line(indent, "{{- end }}")
	}
}

// resolveKind finds the CRD for r's kind, matching on (kind, provider): a
// resource whose provider is "k8s" resolves ONLY against the vendored native
// kinds, and every other resource resolves only against managed resources,
// preferring the scope the XRD needs. For a Namespaced XRD that is the ".m."
// group variant; the legacy cluster-scoped one has a different spec envelope
// and its fields get pruned.
//
// The native match is deliberately explicit-only. Kind names collide across
// the two families for real (provider-aws-ecs ships a managed "Service";
// Kubernetes has one too), and a resolution that quietly preferred one
// family for a bare kind name would emit a structurally different resource
// than the author meant — with no error anywhere. So a native kind is never
// selected without provider: k8s on the resource; a bare kind that matches
// only a native kind fails with the hint instead of being auto-upgraded.
func resolveKind(crds []schema.CRD, r blueprint.Resource, wantNamespaced bool) (schema.CRD, error) {
	if r.Provider == blueprint.NativeProvider {
		for _, c := range crds {
			if c.Native && c.Kind == r.Kind {
				return c, nil
			}
		}
		return schema.CRD{}, fmt.Errorf("resource %q: kind %q is not one of the vendored native Kubernetes kinds "+
			"(provider %q serves the subset pinned to Kubernetes %s)", r.Name, r.Kind, blueprint.NativeProvider, k8s.Version)
	}

	var fallback *schema.CRD
	nativeExists := false
	for i := range crds {
		c := crds[i]
		if c.Kind != r.Kind {
			continue
		}
		if c.Native {
			nativeExists = true
			continue
		}
		if !c.IsManaged() {
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
			"a %s XRD needs the matching variant", r.Kind, scope, fallback.Scope, fallback.Group, scope)
	}
	if nativeExists {
		return schema.CRD{}, fmt.Errorf("kind %q not found in any cached provider, but a native Kubernetes "+
			"kind with that name exists; to compose the native %s, set provider: %s on the resource",
			r.Kind, r.Kind, blueprint.NativeProvider)
	}
	return schema.CRD{}, fmt.Errorf("kind %q not found in any cached provider; run cf provider add", r.Kind)
}
