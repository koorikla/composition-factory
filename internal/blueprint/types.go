// Package blueprint is the on-disk intermediate representation: the file the
// user edits and the single source of truth for generated output.
package blueprint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// NativeProvider is the provider label for native Kubernetes kinds — the
// value a resource's provider field carries to compose a Deployment or
// Service directly, and the provider label those kinds wear in the index
// and /api/kinds. It is a label, not a package reference: native kinds are
// vendored into cf itself (internal/schema/k8s, pinned per Kubernetes
// version) and are always available, so nothing ever fetches, caches or
// digest-pins a source named "k8s" — which is why Validate refuses it in
// spec.sources.
const (
	NativeProvider = "k8s"
	APIVersion     = "factory.crossplane.io/v1alpha1"
	Kind           = "Blueprint"
)

// Blueprint is the root document.
type Blueprint struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

var sourcePaths sync.Map // *Blueprint -> string

// SourcePath returns the path the blueprint was loaded from, if known.
func (b *Blueprint) SourcePath() string {
	if b == nil {
		return ""
	}
	if v, ok := sourcePaths.Load(b); ok {
		return v.(string)
	}
	return ""
}

// SetSourcePath sets the file path this blueprint was loaded from or saved to.
func (b *Blueprint) SetSourcePath(p string) {
	if b != nil {
		if p == "" {
			sourcePaths.Delete(b)
		} else {
			sourcePaths.Store(b, p)
		}
	}
}

type Metadata struct {
	Name string `json:"name"`
}

type Spec struct {
	Sources []Source `json:"sources"`
	XRD     XRD      `json:"xrd"`
	// Templates are user-defined Go templates, name -> body. Each is emitted
	// as a {{- define "<name>" }} block heading the Composition's template
	// and is callable from a field via template: <name> (or applied by a
	// convention). Bodies are validated by parsing them under the real
	// engine's contract — text/template with missingkey=error and
	// function-go-templating's function set (sprig minus env/expandenv, plus
	// its own additions) — so a body that cannot parse at render time is
	// refused at the source. A body renders with the minimal context the
	// field call passes: .spec (the composite's spec), .xr (the composite's
	// metadata.name), .resource (the composed resource's name) and .field
	// (the field path being set). Dereferences of optional .spec keys inside
	// a body are the author's contract: guard them with hasKey, exactly as
	// the generator does.
	Templates map[string]string `json:"templates"`
	// Conventions apply a template to every matching field a resource does
	// NOT set explicitly. Match is a case-sensitive suffix of a top-level
	// forProvider field name (e.g. "tags" matches tags; "Name" matches
	// queueName); the first matching convention in list order wins for each
	// field, and an explicit field always wins over any convention — that is
	// the override mechanism. Conventions apply to MANAGED resources only:
	// a native Kubernetes kind (provider "k8s") has no forProvider plan for
	// them to fill, and its top-level spec is structural (replicas, selector,
	// template...), where a silently defaulted field would change workload
	// semantics — so Validate refuses the combination outright rather than
	// guessing (see load.go).
	Conventions []Convention `json:"conventions"`
	Resources   []Resource   `json:"resources"`
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
	// Emit configures how the generator emits the composition. When absent
	// or templateSource: Inline, the template body is embedded directly in
	// the Composition manifest. In FileSystem mode, templates are exported
	// as individual files packed into ConfigMaps and mounted into the function.
	Emit *Emit `json:"emit,omitempty"`
}

// Emit contains emission preferences for the blueprint.
type Emit struct {
	TemplateSource string `json:"templateSource,omitempty"`
	Engine         string `json:"engine,omitempty"`
}

const (
	TemplateSourceInline     = "Inline"
	TemplateSourceFileSystem = "FileSystem"

	EngineGoTemplating = "go-templating"
	EngineKCL          = "kcl"
	EnginePython       = "python"

	KCLFunctionName       = "function-kcl"
	KCLFunctionPackage    = "xpkg.upbound.io/crossplane-contrib/function-kcl:v0.11.2"
	PythonFunctionName    = "function-python"
	PythonFunctionPackage = "xpkg.upbound.io/crossplane-contrib/function-python:v0.5.0"
)

// SupportedEngines lists all composition rendering engines supported by composition-factory.
var SupportedEngines = []string{
	EngineGoTemplating,
	EngineKCL,
	EnginePython,
}

// TemplateSource returns the effective template source mode ("Inline" or "FileSystem").
func (b *Blueprint) TemplateSource() string {
	if b != nil && b.Spec.Emit != nil && b.Spec.Emit.TemplateSource == TemplateSourceFileSystem {
		return TemplateSourceFileSystem
	}
	return TemplateSourceInline
}

// Engine returns the effective composition render engine ("go-templating", "kcl", or "python").
func (b *Blueprint) Engine() string {
	if b != nil && b.Spec.Emit != nil {
		if strings.EqualFold(b.Spec.Emit.Engine, EngineKCL) {
			return EngineKCL
		}
		if strings.EqualFold(b.Spec.Emit.Engine, EnginePython) {
			return EnginePython
		}
	}
	return EngineGoTemplating
}

// ResourceNamed returns a pointer to the composed resource with the given name,
// or nil if no such resource exists.
func (b *Blueprint) ResourceNamed(name string) *Resource {
	if b == nil {
		return nil
	}
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == name {
			return &b.Spec.Resources[i]
		}
	}
	return nil
}

// Convention binds a template to every top-level forProvider leaf whose name
// ends with Match, on every resource that does not set that field itself.
type Convention struct {
	Match    string `json:"match"`
	Template string `json:"template"`
}

// Source is one schema source: a provider package (OCI ref) or a CRD
// manifest file. Exactly one of the two is set (Validate enforces it).
//
// CRDs points at a YAML file of CustomResourceDefinitions, relative to the
// blueprint's directory. Its kinds join the schema set object-rooted (the
// composed document IS the object — no forProvider, no providerConfigRef),
// which is what an Argo Workflow, another composition's XR, or any other
// operator-owned kind actually needs. Resources reference the source by
// this same path in their provider field.
type Source struct {
	Provider string `json:"provider,omitempty"`
	CRDs     string `json:"crds,omitempty"`
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
//
// Properties, valid ONLY on type: object, declares typed members: the
// parameter stops being the v1 free-form string map (additionalProperties:
// string) and becomes a real nested schema, one member per entry. Members
// are scalar (string, integer, number, boolean — no object or array members)
// and nest exactly one level in v1: a member may not declare properties of
// its own (deeper nesting is planned work, not a permanent ruling). Each
// member takes the same required/default/enum/description declarations a
// top-level parameter does, under the same validation rules, and is wired
// into a resource with from: params.<name>.<member>. An object parameter
// WITHOUT properties keeps today's free-form map semantics untouched.
//
// omitempty is load-bearing: the HTTP API re-marshals every parameter on
// every edit, and without it every scalar parameter in every blueprint would
// gain a literal `properties: null` the first time anyone touched the file.
type Parameter struct {
	Type        string               `json:"type"`
	Required    bool                 `json:"required"`
	Enum        []string             `json:"enum"`
	Default     string               `json:"default"`
	Description string               `json:"description"`
	Properties  map[string]Parameter `json:"properties,omitempty"`
}

// UnmarshalJSON permits scalar values (booleans, numbers, strings) for Default.
func (p *Parameter) UnmarshalJSON(data []byte) error {
	type rawParam struct {
		Type        string               `json:"type"`
		Required    bool                 `json:"required"`
		Enum        []string             `json:"enum"`
		Default     any                  `json:"default"`
		Description string               `json:"description"`
		Properties  map[string]Parameter `json:"properties,omitempty"`
	}
	var raw rawParam
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	p.Type = raw.Type
	p.Required = raw.Required
	p.Enum = raw.Enum
	p.Description = raw.Description
	p.Properties = raw.Properties
	if raw.Default != nil {
		switch val := raw.Default.(type) {
		case string:
			p.Default = val
		case bool:
			if val {
				p.Default = "true"
			} else {
				p.Default = "false"
			}
		case float64:
			if val == float64(int64(val)) {
				p.Default = strconv.FormatInt(int64(val), 10)
			} else {
				p.Default = strconv.FormatFloat(val, 'f', -1, 64)
			}
		default:
			p.Default = fmt.Sprintf("%v", val)
		}
	}
	return nil
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
// When, when set, wraps the resource's whole rendered document — outside any
// forEach range, so a false condition skips every iteration — in a template
// conditional. The grammar is minimal and exact (see ParseWhen):
//
//	params.<name>                  — a bare boolean parameter
//	params.<name> == "<literal>"   — string equality
//	params.<name> != "<literal>"   — string inequality
//
// The referenced parameter must be required or carry a default: the emitted
// condition dereferences it unguarded ({{- if $spec.<name> }}), and under
// options: ["missingkey=error"] an absent key is a hard render failure — the
// same rule, for the same reason, as ForEach's loop bound. The bare form
// requires a boolean parameter; the comparison forms require a string one,
// and when that parameter declares an enum the literal must be one of its
// values (a literal outside the enum makes the condition constant, silently
// dead — every gate green, a resource that can never (or always) exist).
type Resource struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Provider string           `json:"provider"`
	ForEach  string           `json:"forEach"`
	When     string           `json:"when"`
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
	// Envelope applies to MANAGED resources only. A native Kubernetes kind
	// (provider "k8s") has no Crossplane envelope — the composed object is
	// not a managed resource — so the emitter refuses envelope entries on
	// one outright (see internal/emit/envelope.go).
	//
	// Rules enforced by Validate and the emitter:
	//
	//   - providerConfigRef defaults to {kind: ClusterProviderConfig, name: {{ $spec.providerName }}}.
	//     Any resource can override providerConfigRef.name (e.g. wired to another parameter
	//     or set to a literal ProviderConfig name) or providerConfigRef.kind per resource in its envelope.
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
	// Annotations sets metadata.annotations entries on the composed document —
	// valid on BOTH families, because the annotations block is part of the
	// shared metadata the emitter writes before the native/managed fork (a
	// native ServiceAccount carrying eks.amazonaws.com/role-arn is the
	// motivating case: nothing else in the field surface can author it, since
	// top-level metadata is deliberately not a settable field path).
	//
	// Keys are free-form annotation keys, validated to the Kubernetes
	// qualified-name shape (an optional DNS-subdomain prefix + '/' + a name of
	// at most 63 chars; see validateResourceAnnotations) — NEVER the camelCase
	// path grammar fields use, because dots and slashes are the norm here, not
	// path separators. Two keys are reserved and refused: the
	// composition-resource-name annotation (both its crossplane.io spelling and
	// the gotemplating.fn.crossplane.io one the emitted
	// setResourceNameAnnotation call writes) — the emitter owns that key as
	// node identity, and a blueprint entry for it would silently collide with
	// the function-set value.
	//
	// Entries use the same exactly-one-of {value|from|raw|template} Field
	// forms as Fields. Annotation values are ALWAYS strings, which shapes the
	// rendering (see internal/emit/annotations.go): value is quoted literally,
	// and a from: wire — params.<name> (scalar) or
	// resources.<name>.status.<path> (scalar leaf), with exactly the guard
	// discipline field wires get — is piped through `quote` so a numeric or
	// boolean scalar still lands as the string the API server requires. An
	// absent optional parameter or an unobserved status source omits the KEY
	// cleanly (never an empty-valued entry, never "<no value>").
	//
	// template: is legal on native resources here, unlike in Fields: the
	// fields refusal exists because a template call's output re-indents to the
	// fixed forProvider column, which a native field at an arbitrary nesting
	// depth breaks — but every annotation entry sits at ONE fixed column
	// (metadata.annotations children), the same for both families, so the
	// mechanical reason does not apply.
	//
	// omitempty for the same reason Envelope has it; an empty map means the
	// same as an absent one (it authors nothing), so nothing is lost when the
	// two collapse on persist — the same documented ruling as Spec.Pipeline.
	Annotations map[string]Field `json:"annotations,omitempty"`
}

// when grammar, compiled once. The literal character class excludes '"' and
// '\\' so the emitted Go-syntax quoting (%q) is always the literal wrapped
// in plain quotes — byte-deterministic, no escape sequences to reason about.
var (
	whenBareRE = regexp.MustCompile(`^params\.([a-zA-Z][a-zA-Z0-9]*)$`)
	whenCmpRE  = regexp.MustCompile(`^params\.([a-zA-Z][a-zA-Z0-9]*) (==|!=) "([^"\\]*)"$`)
)

// ParseWhen splits a when expression into its parameter name, operator and
// literal. op is "" for the bare boolean form, "==" or "!=" for the
// comparison forms (with literal carrying the compared string, which may be
// empty: params.x == "" is legal). The grammar is exact — one space around
// the operator, double quotes around the literal — so that a when expression
// has exactly one written form and the emitted Composition is
// byte-deterministic.
func ParseWhen(expr string) (param, op, literal string, err error) {
	if m := whenBareRE.FindStringSubmatch(expr); m != nil {
		return m[1], "", "", nil
	}
	if m := whenCmpRE.FindStringSubmatch(expr); m != nil {
		return m[1], m[2], m[3], nil
	}
	return "", "", "", fmt.Errorf("when must be params.<name> (a boolean parameter), "+
		`params.<name> == "<literal>" or params.<name> != "<literal>" — exactly one space around `+
		"the operator, double quotes around the literal, no backslashes or embedded quotes (got %q)", expr)
}

// Field sets one path on a composed resource. Exactly one of From, Value,
// Raw or Template must be set. Template names an entry in spec.templates and
// renders as an include call whose output becomes the field's YAML value —
// a scalar on one line, or an indented block for a multi-line body (the
// generator owns the nindent).
//
// From accepts two reference grammars:
//
//	params.<name>                       — an XRD parameter of the composite
//	resources.<name>.status.<path>      — a status value observed on another
//	                                      composed resource in this blueprint
//
// A status reference is a cross-resource wire: the value exists only after
// the referenced resource has been observed at least once, so the emitter
// renders it behind a hasKey guard chain over $.observed.resources and the
// field is omitted cleanly until then — Crossplane fills it in on a later
// reconcile. The referenced resource must not itself be looped (forEach):
// a looped resource's composed names are indexed (<name>-0, <name>-1, ...),
// so the un-indexed key the reference names never appears in the observed
// map. Validate enforces both, plus that <path> resolves to a scalar leaf in
// the referenced kind's CRD status schema (checked in internal/emit, which
// holds the CRDs).
type Field struct {
	From     string `json:"from"`
	Value    string `json:"value"`
	Raw      string `json:"raw"`
	Template string `json:"template"`
}

// UnmarshalJSON permits scalar values (booleans, numbers, strings) for Value, From, Raw, Template.
func (f *Field) UnmarshalJSON(data []byte) error {
	type rawField struct {
		From     any `json:"from"`
		Value    any `json:"value"`
		Raw      any `json:"raw"`
		Template any `json:"template"`
	}
	var raw rawField
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	toString := func(v any) string {
		if v == nil {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case bool:
			if val {
				return "true"
			}
			return "false"
		case float64:
			if val == float64(int64(val)) {
				return strconv.FormatInt(int64(val), 10)
			}
			return strconv.FormatFloat(val, 'f', -1, 64)
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	f.From = toString(raw.From)
	f.Value = toString(raw.Value)
	f.Raw = toString(raw.Raw)
	f.Template = toString(raw.Template)
	return nil
}

// ParamRef splits a params.<name>[.<member>] reference into its parameter
// name and member. ok is false when ref does not carry the params. prefix at
// all (a resources. status reference, say). member is "" for a plain
// top-level reference. The member half is NOT validated here — it may be
// empty ("params.obj.") or dotted ("params.obj.a.b"); Validate is the layer
// that turns those into specific errors, and every other caller runs after
// Validate.
func ParamRef(ref string) (param, member string, ok bool) {
	rest, found := strings.CutPrefix(ref, "params.")
	if !found {
		return "", "", false
	}
	param, member, _ = strings.Cut(rest, ".")
	return param, member, true
}

// statusRefPrefix marks a Field.From cross-resource status reference.
const statusRefPrefix = "resources."

// StatusRef splits a well-formed cross-resource reference
// resources.<name>.status.<path> into its resource name and status-relative
// path. ok is false when from is not shaped that way at all — either not a
// resources. reference (a params. one, say) or a resources. reference whose
// grammar is broken (no .status. separator, or an empty name or path).
// Validate is the layer that turns the broken-grammar case into a specific
// error; every other caller runs after Validate and can treat !ok as
// "not a status reference".
func StatusRef(from string) (resource, path string, ok bool) {
	rest, found := strings.CutPrefix(from, statusRefPrefix)
	if !found {
		return "", "", false
	}
	resource, path, found = strings.Cut(rest, ".status.")
	if !found || resource == "" || path == "" {
		return "", "", false
	}
	return resource, path, true
}
