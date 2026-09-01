// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

// Blueprint is the root document.
type Blueprint struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name string `json:"name"`
}

type Spec struct {
	Sources   []Source   `json:"sources"`
	XRD       XRD        `json:"xrd"`
	Resources []Resource `json:"resources"`
	// Pipeline, when non-empty, fully declares the Composition pipeline steps
	// that surround the built-in go-templating step. When absent (or empty),
	// the generator emits its default pipeline: the templating step followed
	// by a function-auto-ready step (see internal/emit). Declaring ANY step
	// replaces that default in full, so a blueprint that wants readiness
	// propagation alongside its own steps must declare auto-ready explicitly:
	//
	//	pipeline:
	//	  - name: auto-ready
	//	    functionRef: function-auto-ready
	//	    package: xpkg.crossplane.io/crossplane-contrib/function-auto-ready
	//
	// omitempty keeps a blueprint that never declared the key from gaining a
	// literal `pipeline: null` when the API server persists it back; an empty
	// list means the same thing as an absent one, so nothing is lost by
	// collapsing the two.
	Pipeline []PipelineStep `json:"pipeline,omitempty"`
}

// Source is one schema source. M1 supports provider packages only.
type Source struct {
	Provider string `json:"provider"`
}

// XRD describes the composite API to generate.
type XRD struct {
	Group      string               `json:"group"`
	Kind       string               `json:"kind"`
	Plural     string               `json:"plural"`
	Version    string               `json:"version"`
	Scope      string               `json:"scope"`
	Parameters map[string]Parameter `json:"parameters"`
}

// Parameter is one spec field of the composite API. It is single-source: this
// declaration produces both the XRD schema and the template default.
type Parameter struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum"`
	Default     string   `json:"default"`
	Description string   `json:"description"`
}

// PipelineStep is one blueprint-declared Composition pipeline step, placed
// relative to the built-in go-templating step (TemplatingStepName).
//
// Input is the function's typed input object, held VERBATIM as the raw YAML
// string the user wrote — never normalised on load, so a loaded blueprint
// marshals back byte-for-byte. The emitter parses it and re-renders it
// deterministically (sorted keys) into the Composition; Validate guarantees
// it parses and carries apiVersion/kind before it can get that far.
type PipelineStep struct {
	Name        string `json:"name"`
	FunctionRef string `json:"functionRef"`
	Package     string `json:"package"`
	Input       string `json:"input,omitempty"`
	Position    string `json:"position,omitempty"`
}

// Resource is one composed resource.
//
// ForEach, when set, repeats the resource's whole rendered document N times,
// with N read at render time from an integer XRD parameter. The value grammar
// is exactly "params.<name>" — the same reference shape as Field.From. The
// referenced parameter must be an integer and must be either required or
// carry a default (see Validate): the Composition dereferences the loop
// bound unguarded, and under options: ["missingkey=error"] an absent key is
// a hard render failure, so only the XRD's required gate or its schema
// default makes the dereference safe.
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	ForEach  string           `json:"forEach"`
	Fields   map[string]Field `json:"fields"`
	// Envelope sets paths on the resource's Crossplane-native spec envelope —
	// the kind's spec.properties minus forProvider/initProvider, exactly what
	// schema.CRD.Envelope computes from the .m. CRD (managementPolicies,
	// writeConnectionSecretToRef, and whatever else that variant actually
	// carries; the namespaced and cluster-scoped variants differ structurally,
	// so paths are checked against the resolved variant's own schema at emit
	// time, never against a hard-coded list). Entries use the same
	// exactly-one-of {from|value|raw} Field forms as Fields, and override the
	// generator's computed defaults field by field: an unset entry keeps
	// today's default, so an envelope-free blueprint emits byte-identical
	// output.
	//
	// Two deliberate v1 rulings, enforced by Validate and the emitter:
	//
	//   - providerConfigRef cannot be set here. It is derived from the
	//     required providerName parameter (one source of truth); an envelope
	//     entry for it would be a second, silently-divergent one.
	//   - An array-typed envelope leaf (e.g. managementPolicies, an array of
	//     enum strings) takes `value` as a COMMA-SEPARATED list, rendered as a
	//     YAML flow sequence of quoted strings ("Observe, Create" ->
	//     ['Observe', 'Create']), or `raw` as the literal-YAML escape hatch.
	//     `from` is rejected on array leaves: a scalar parameter renders one
	//     scalar, and M1 refuses array-typed parameters outright (see
	//     load.go), so there is nothing a wire could correctly render.
	//
	// omitempty for the same reason Spec.Pipeline has it: a resource that
	// never declared the key must not gain a literal `envelope: null` when the
	// document is persisted back.
	Envelope map[string]Field `json:"envelope,omitempty"`
}

// Field sets one path on a composed resource. Exactly one of From, Value or Raw
// must be set.
type Field struct {
	From  string `json:"from"`
	Value string `json:"value"`
	Raw   string `json:"raw"`
}
