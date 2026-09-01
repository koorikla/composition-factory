package blueprint

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"
)

// validTypes are the parameter types M1 accepts.
//
// "array" is deliberately absent. See the array branch in Validate for the
// full reasoning; briefly, both of its exits are broken today: the XRD
// emitter writes `type: array` with no `items:` (no structural schema
// accepts that), and a `from:` mapping on an array parameter renders Go's
// fmt of the slice.
var validTypes = map[string]bool{
	"string": true, "integer": true, "number": true,
	"boolean": true, "object": true,
}

// compositeTypes are the parameter types whose values are not scalars.
// A `from:` mapping cannot render one correctly in M1 (see Validate).
var compositeTypes = map[string]bool{"object": true, "array": true}

// Identifier formats, checked because every one of these values reaches
// emitted output as a raw YAML map key or a structural value (an OpenAPI
// property name, a CRD names.kind/plural/... field, or a
// composition-resource-name annotation). A value that is syntactically legal
// in the blueprint file (e.g. a quoted YAML mapping key) but not a legal
// identifier either breaks the emitted YAML outright (a colon or a leading
// '#' in a parameter name), parses but is silently reinterpreted (a
// parameter named "yes" becomes the boolean key true), or is rejected later,
// more confusingly, by the API server at apply time (an illegal OpenAPI
// property name). Validating at the source closes the class for every
// emitter that reads these fields, not just the XRD emitter. Compiled once
// at package init, not per call.
var (
	// paramNameRE matches camelCase identifiers, the shape of real CRD spec
	// properties (forProvider, writeConnectionSecretToRef, maxMessageSize).
	paramNameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	// groupRE matches a DNS subdomain, per Kubernetes' own rule for API groups.
	groupRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	// kindRE matches a Kubernetes Kind: starts uppercase, alphanumeric.
	kindRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	// pluralRE matches a Kubernetes plural resource name: all lowercase.
	pluralRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	// versionRE matches a Kubernetes API version: v1, v1beta1, v1alpha1, ...
	versionRE = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)
	// resourceNameRE matches a Kubernetes-style DNS label, since a resource's
	// name becomes a composition-resource-name annotation value.
	resourceNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	// providerRefRE matches an OCI image reference: registry/path[:tag] or
	// registry/path@digest, optionally with a :port on the registry host
	// (registry:5000/path). Deliberately permissive -- it is not a full OCI
	// reference grammar, just the character class every legal reference is
	// built from ([a-zA-Z0-9._/:@-]), covering the plain-tag, digest-pinned
	// and port forms actually used by cmd/cf/gen.go, cmd/cf/serve.go and
	// internal/api/generate.go (all pass Source.Provider / Resource.Provider
	// straight to cache.Store.Load). It exists to reject control characters
	// and other junk here, at the source, rather than let a malformed value
	// reach the cache's filesystem slug or the registry client unchecked;
	// it does not attempt to fully validate OCI reference syntax.
	providerRefRE = regexp.MustCompile(`^[a-zA-Z0-9._/:@-]+$`)
)

// yamlKeywords are scalars that go-yaml (used transitively by
// sigs.k8s.io/yaml, and by whatever parses the emitted output) resolves to a
// non-string value when written unquoted, per YAML 1.1's keyword rules --
// regardless of surrounding context. Format regexes alone do not exclude
// these: "yes" and "no" are both plain letter sequences and satisfy
// paramNameRE and pluralRE on their own, and a bare (dot-free) "no" also
// satisfies groupRE. But all three reach emitted output as a raw, unquoted
// YAML scalar -- a parameter name as a map key (internal/emit/xrd.go's
// `d.Line(7, "%s:", n)`), plural and group as values (`d.Line(2, "plural:
// %s", ...)`, `d.Line(1, "group: %s", ...)`) -- so a value shaped like "yes"
// would silently become the boolean true, not the string "yes" the user
// wrote. Checked case-insensitively, matching YAML 1.1 resolution, and
// shared by every field below rather than duplicated per field.
var yamlKeywords = map[string]bool{
	"true": true, "false": true, "yes": true, "no": true,
	"on": true, "off": true, "null": true, "y": true, "n": true,
}

// checkScalar rejects control characters in a user-controlled scalar.
//
// Every emitter in internal/emit builds its document with Doc.Line, which
// writes `indent + text + "\n"` verbatim. quoteYAML wraps a user scalar in
// single quotes, which handles ": ", " #" and keyword-shaped values -- but a
// single-quoted YAML scalar is still a ONE-LINE construct here, because
// nothing re-indents its continuation. An embedded "\n" therefore lands the
// rest of the value at column 0 of the emitted file, outside the quotes,
// outside the block scalar, outside every indentation context the emitter
// established. Two things follow, and the second is worse:
//
//	Field.Value = "eu-north-1\nbogus: injected"
//	  -> a Composition whose `template: |` block scalar is terminated early;
//	     sigs.k8s.io/yaml refuses the document outright. Loud.
//	Parameter.Description = "line one\nline two: injected"
//	  -> an XRD that still PARSES, having silently grown a bogus top-level
//	     key. `cf gen --check` then reports "in sync". Silent.
//
// The second is this project's central defect class: legal YAML, every gate
// green, exit 0, wrong artifact. Quoting cannot close it -- the break
// happens before the quote can matter -- so it is closed here, at the layer
// that already owns identifier validation, for every emitter at once rather
// than once per call site.
//
// The check is deliberately total over control runes rather than just \n and
// \r. A carriage return is a YAML line break in its own right; \t is trimmed
// away by Doc.Line's TrimRight when it lands at end of line, which silently
// changes the value the user wrote; NEL (U+0085) is a line break under YAML
// 1.1; and the remaining C0/C1/DEL runes have no legitimate use in a CRD
// description, an enum value or a resource field and are not worth
// individually reasoning about. U+2028 and U+2029 are added explicitly:
// unicode.IsControl does not classify them (they are Zl/Zp), but YAML 1.1
// treats both as line breaks.
func checkScalar(fieldPath, s string) error {
	for i, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%s: contains the control character %q at byte %d; "+
				"newlines, carriage returns, tabs and other non-printable runes are not allowed "+
				"because the emitter writes this value as a single-line YAML scalar -- "+
				"a line break escapes it and silently changes the generated document's structure",
				fieldPath, r, i)
		}
	}
	return nil
}

// groupIsBareKeyword reports whether group -- split on '.' -- is a single
// label that is itself a YAML keyword (e.g. a bare "no"). group is emitted
// as one unquoted YAML scalar (`group: %s`): a single-label group whose one
// segment is a keyword resolves as a boolean/null, not the string the user
// wrote. A multi-label group is never at risk this way -- "no.example.com"
// is unambiguously a string once it contains a dot, YAML 1.1's keyword
// grammar matches whole scalars, not substrings of one -- so a legitimate
// group whose first label happens to read as a keyword must not be rejected.
func groupIsBareKeyword(group string) bool {
	segments := strings.Split(group, ".")
	return len(segments) == 1 && yamlKeywords[strings.ToLower(segments[0])]
}

// ReadError indicates that the blueprint file itself could not be read (the
// path does not exist, is a directory, or is otherwise inaccessible) — as
// opposed to being read successfully and then failing to parse as YAML or
// to validate. That distinction is not visible from the error message alone
// (both cases just wrap an *fs.PathError or a yaml/Validate error via %w)
// and Load's own signature stays plain `(*Blueprint, error)`, but a caller
// that wants to react differently to the two cases — the HTTP API does,
// treating an inaccessible fixed server path as a 500 and a bad document as
// a 400 — can use errors.As(err, new(*blueprint.ReadError)).
type ReadError struct {
	Err error
}

func (e *ReadError) Error() string { return e.Err.Error() }
func (e *ReadError) Unwrap() error { return e.Err }

// Load reads and validates a blueprint file.
func Load(path string) (*Blueprint, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, &ReadError{Err: fmt.Errorf("read blueprint: %w", err)}
	}
	var b Blueprint
	if err := yaml.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &b, nil
}

// Validate reports the first structural problem, naming the offending field.
func (b *Blueprint) Validate() error {
	// metadata.name reaches every generated file's provenance header
	// (emit.header writes "# Source: blueprints/<name>.cf.yaml"). A newline
	// there ends the comment and puts whatever follows at column 0 of a
	// document that otherwise parses fine.
	if err := checkScalar("metadata.name", b.Metadata.Name); err != nil {
		return err
	}

	// spec.sources[*].provider was never checked here before PUT
	// /api/blueprint existed: no route made the full document
	// client-writable, so an operator hand-editing the file was the only
	// path in, and a typo'd provider ref just failed loudly at `cf provider
	// add` / cache.Store.Load time. PUT changes that -- the whole document,
	// sources included, is now client-writable -- so the same three checks
	// applied to every other user-controlled scalar apply here too: it must
	// be present, free of control characters (see checkScalar; a source
	// reference does not currently reach emitted YAML the way a resource
	// field does, but it is persisted verbatim by writeBlueprintFile and
	// re-read on the next request, so a stray newline would still corrupt
	// the stored document), and shaped like a reference cache.Store.Load can
	// actually use.
	for i, s := range b.Spec.Sources {
		if s.Provider == "" {
			return fmt.Errorf("spec.sources[%d].provider is required", i)
		}
		// "k8s" is a label, not a package: every loader treats a source entry
		// as something to pull from the schema cache (cache.Store.Load), so a
		// source named "k8s" would fail there with a misleading "run: cf
		// provider add k8s". Refuse it here with the real explanation instead.
		if s.Provider == NativeProvider {
			return fmt.Errorf("spec.sources[%d].provider: %q is not a package source -- native Kubernetes "+
				"kinds are vendored into cf itself and always available. Delete this source entry and set "+
				"provider: %s on the resources that compose native kinds", i, s.Provider, NativeProvider)
		}
		if err := checkScalar(fmt.Sprintf("spec.sources[%d].provider", i), s.Provider); err != nil {
			return err
		}
		if !providerRefRE.MatchString(s.Provider) {
			return fmt.Errorf("spec.sources[%d].provider: %q is not a valid provider reference "+
				"(e.g. ghcr.io/org/provider-name:v1.2.3, or ...@sha256:<digest>)", i, s.Provider)
		}
	}

	x := b.Spec.XRD
	required := []struct{ name, val string }{
		{"group", x.Group}, {"kind", x.Kind}, {"plural", x.Plural}, {"version", x.Version},
	}
	var missing []string
	for _, f := range required {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) == 1 {
		return fmt.Errorf("spec.xrd.%s is required", missing[0])
	}
	if len(missing) > 1 {
		return fmt.Errorf("spec.xrd needs %s", strings.Join(missing, ", "))
	}

	if !groupRE.MatchString(x.Group) || groupIsBareKeyword(x.Group) {
		return fmt.Errorf("spec.xrd.group: %q is not a valid DNS subdomain "+
			"(e.g. platform.example.com), or is a bare YAML keyword like yes/no/true/false", x.Group)
	}
	if !kindRE.MatchString(x.Kind) {
		return fmt.Errorf("spec.xrd.kind: %q is not a valid Kind (must start with an uppercase letter, e.g. XQueue)", x.Kind)
	}
	if !pluralRE.MatchString(x.Plural) || yamlKeywords[strings.ToLower(x.Plural)] {
		return fmt.Errorf("spec.xrd.plural: %q is not a valid plural name "+
			"(must be all lowercase, e.g. xqueues, and not a YAML keyword like yes/no/true/false)", x.Plural)
	}
	if !versionRE.MatchString(x.Version) {
		return fmt.Errorf("spec.xrd.version: %q is not a valid API version (e.g. v1, v1beta1, v1alpha1)", x.Version)
	}

	switch x.Scope {
	case "Namespaced":
	case "Cluster":
		// Accepted by an earlier version of this function, which was a
		// half-composition: internal/emit/composition.go only emits a
		// providerConfigRef for the namespaced envelope, so a Cluster
		// blueprint generated a Composition whose every managed resource
		// silently landed on the ProviderConfig named "default" -- a legal,
		// rendering, exit-0 artifact pointed at the wrong credentials. The
		// cluster-scoped envelope is genuinely different ({name, policy}
		// rather than {kind, name}, plus deletionPolicy) and inventing it
		// here without a rendered test would be guessing. M1 is Namespaced
		// only, deliberately; Cluster scope is planned future work.
		return fmt.Errorf("spec.xrd.scope: Cluster is not supported in M1 -- use Namespaced. " +
			"The cluster-scoped managed-resource envelope differs from the namespaced one " +
			"(providerConfigRef is {name, policy}, not {kind, name}, and deletionPolicy exists) " +
			"and the Composition emitter does not yet render it; emitting it untested would " +
			"silently bind every composed resource to the ProviderConfig named \"default\". " +
			"Cluster scope is planned work, not a permanent restriction")
	case "LegacyCluster":
		return fmt.Errorf("spec.xrd.scope: LegacyCluster is not valid in apiextensions.crossplane.io/v2")
	case "":
		return fmt.Errorf("spec.xrd.scope must be set explicitly to Namespaced or Cluster; " +
			"the server and the crossplane CLI default it differently")
	default:
		return fmt.Errorf("spec.xrd.scope: unknown scope %q", x.Scope)
	}

	names := make([]string, 0, len(x.Parameters))
	for n := range x.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !paramNameRE.MatchString(n) || yamlKeywords[strings.ToLower(n)] {
			return fmt.Errorf("spec.xrd.parameters.%s: invalid parameter name "+
				"(must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)", n)
		}
		p := x.Parameters[n]
		// type: array has two exits and M1 gets both wrong, so it is refused
		// at the source rather than emitted broken.
		//
		//  1. internal/emit/xrd.go writes `type: array` with no `items:`.
		//     A structural schema (which is what an XRD's openAPIV3Schema
		//     is) requires items on an array; the API server rejects it.
		//     Loud, but still a generated artifact that cannot be applied.
		//  2. internal/emit/composition.go renders a `from:` mapping as a
		//     bare `{{ $spec.zones }}`, and Go's template engine formats a
		//     []any with fmt: `[a b c]`. That IS valid YAML, and a
		//     `type: array, items: {type: string}` schema accepts it as a
		//     ONE-element list whose single member is the string "a b c".
		//     Silent, which is worse.
		//
		// The proper fix for both is M2 work, not a validation rule: render
		// composite values as `{{ $spec.x | toYaml | nindent N }}` and emit a
		// real `items:` schema derived from the parameter declaration. Until
		// that exists, refusing the type is the honest option.
		if p.Type == "array" {
			return fmt.Errorf("spec.xrd.parameters.%s: type \"array\" is not supported in M1. "+
				"The XRD emitter cannot write the required items: schema for it, and a from: "+
				"mapping would render Go's fmt of the slice (\"[a b c]\") -- valid YAML, silently "+
				"wrong. Use a scalar parameter, or a raw: field for a literal list", n)
		}
		if !validTypes[p.Type] {
			return fmt.Errorf("spec.xrd.parameters.%s: unknown type %q", n, p.Type)
		}
		// Description, default and every enum entry are user-authored free
		// text that internal/emit/xrd.go writes straight into the XRD. See
		// checkScalar: a newline in any of them grows the XRD a bogus
		// top-level key while leaving it parseable.
		if err := checkScalar("spec.xrd.parameters."+n+".description", p.Description); err != nil {
			return err
		}
		if err := checkScalar("spec.xrd.parameters."+n+".default", p.Default); err != nil {
			return err
		}
		for i, e := range p.Enum {
			if err := checkScalar(fmt.Sprintf("spec.xrd.parameters.%s.enum[%d]", n, i), e); err != nil {
				return err
			}
		}
		// The XRD emitter honours Default, emitting it quoted for type:
		// string and unquoted for integer/number/boolean. It has no
		// sensible handling for a default on type: object or array, and
		// there is nothing to stop it from writing an unparseable
		// integer/number token or a non-boolean boolean unvalidated -- both
		// would produce an invalid CRD schema. Catch it here, at the
		// source, rather than in the emitter guessing.
		if p.Default != "" {
			switch p.Type {
			case "object", "array":
				return fmt.Errorf("spec.xrd.parameters.%s: default is not valid for type %q "+
					"(only string, integer, number and boolean defaults are supported)", n, p.Type)
			case "boolean":
				if p.Default != "true" && p.Default != "false" {
					return fmt.Errorf("spec.xrd.parameters.%s: default %q is not a valid boolean "+
						`(must be "true" or "false")`, n, p.Default)
				}
			case "integer":
				if _, err := strconv.ParseInt(p.Default, 10, 64); err != nil {
					return fmt.Errorf("spec.xrd.parameters.%s: default %q is not a valid integer", n, p.Default)
				}
			case "number":
				if _, err := strconv.ParseFloat(p.Default, 64); err != nil {
					return fmt.Errorf("spec.xrd.parameters.%s: default %q is not a valid number", n, p.Default)
				}
			}
		}
	}

	// internal/emit/composition.go emits, for every composed resource in a
	// Namespaced blueprint:
	//
	//	providerConfigRef:
	//	  kind: ClusterProviderConfig
	//	  name: {{ $spec.providerName }}
	//
	// That dereference is hard-coded there and unconditional, but nothing
	// used to require the blueprint to declare the parameter it reads. A
	// blueprint without it validated, generated, and produced a Composition
	// that can never render: under options: ["missingkey=error"] the
	// dereference is a hard render failure, and without that option it would
	// be worse -- the literal string "<no value>" as a ProviderConfig name.
	// Required (not merely declared) because the guard the Composition gives
	// optional parameters (hasKey) is not applied to this one; the XRD gate
	// is what makes the bare dereference safe.
	if x.Scope == "Namespaced" {
		p, ok := x.Parameters["providerName"]
		switch {
		case !ok:
			return fmt.Errorf("spec.xrd.parameters.providerName is required for a Namespaced XRD: " +
				"the Composition emits providerConfigRef.name as {{ $spec.providerName }} for every " +
				"composed resource, so a blueprint without this parameter generates a Composition " +
				"that can never render. Add: providerName: {type: string, required: true}")
		case p.Type != "string":
			return fmt.Errorf("spec.xrd.parameters.providerName: type must be string, got %q -- "+
				"it is rendered into providerConfigRef.name, which is a Kubernetes object name", p.Type)
		case !p.Required:
			return fmt.Errorf("spec.xrd.parameters.providerName: must be required: true -- " +
				"the Composition dereferences it unguarded for every composed resource, and only " +
				"the XRD's required gate makes that dereference safe")
		}
	}

	for i, r := range b.Spec.Resources {
		if r.Name == "" || r.Kind == "" {
			switch {
			case r.Name == "" && r.Kind == "":
				return fmt.Errorf("spec.resources[%d]: needs a name and a kind", i)
			case r.Name == "":
				return fmt.Errorf("spec.resources[%d] (kind %q): needs a name", i, r.Kind)
			default:
				return fmt.Errorf("spec.resources[%d] %q: needs a kind", i, r.Name)
			}
		}
		if !resourceNameRE.MatchString(r.Name) {
			return fmt.Errorf("spec.resources[%d] %q: invalid resource name (must be a DNS label, e.g. main-queue)", i, r.Name)
		}
		// r.Kind is looked up against resolveKind's list of cached CRDs by
		// exact string equality (internal/emit/composition.go), so a bogus
		// value fails loudly there ("kind %q not found in any cached
		// provider") rather than reaching emitted output -- but it still
		// isn't checkScalar-clean the way r.Name and every field value are,
		// and every real Kubernetes Kind it could ever legitimately match
		// satisfies kindRE (see spec.xrd.kind above), so enforcing the same
		// shape here is free and catches a typo before the cache lookup
		// even runs.
		if err := checkScalar(fmt.Sprintf("spec.resources[%d].kind", i), r.Kind); err != nil {
			return err
		}
		if !kindRE.MatchString(r.Kind) {
			return fmt.Errorf("spec.resources[%d].kind: %q is not a valid Kind (must start with an uppercase letter, e.g. Queue)", i, r.Kind)
		}
		// r.Provider is optional (M1 resolves a resource's kind against
		// every cached source, not just one), but when set it reaches
		// cache.Store.Load exactly the way spec.sources[*].provider does --
		// see the providerRefRE comment above -- so it gets the same checks.
		if r.Provider != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].provider", i), r.Provider); err != nil {
				return err
			}
			if !providerRefRE.MatchString(r.Provider) {
				return fmt.Errorf("spec.resources[%d].provider: %q is not a valid provider reference "+
					"(e.g. ghcr.io/org/provider-name:v1.2.3, or ...@sha256:<digest>)", i, r.Provider)
			}
		}
		paths := make([]string, 0, len(r.Fields))
		for p := range r.Fields {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			f := r.Fields[p]
			set := 0
			for _, v := range []string{f.From, f.Value, f.Raw} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return fmt.Errorf("resource %q field %q: set exactly one of from, value or raw (got %d)",
					r.Name, p, set)
			}
			// value is written into the rendered inner document as a
			// single-quoted scalar; raw is written verbatim. Both are
			// single-line constructs (see checkScalar), and both sit inside
			// the Composition's `template: |` block scalar, so a newline in
			// either terminates that block scalar and turns the rest of the
			// value into top-level keys of the Composition document.
			//
			// raw is NOT exempt, though it is the raw-YAML escape hatch and a
			// multi-line template body is a plausible thing to want. The
			// emitter writes it with a single `d.Line(indent, "%s: %s", ...)`
			// and has no machinery to re-indent continuation lines to the
			// block scalar's column; exempting raw would therefore hand the
			// user a documented way to corrupt the document structure, which
			// is precisely the class this check exists to close. A multi-line
			// raw form needs an emitter that indents each line -- that is the
			// feature to build, not a hole to leave open.
			// A slice, not a map: Validate reports the FIRST problem, so the
			// order it inspects these in is part of its contract.
			for _, src := range []struct{ label, val string }{
				{"from", f.From}, {"raw", f.Raw}, {"value", f.Value},
			} {
				if err := checkScalar(fmt.Sprintf("resource %q field %q: %s", r.Name, p, src.label), src.val); err != nil {
					return err
				}
			}
			if f.From != "" {
				param, ok := strings.CutPrefix(f.From, "params.")
				if !ok {
					return fmt.Errorf("resource %q field %q: from must start with params. (got %q)",
						r.Name, p, f.From)
				}
				decl, exists := x.Parameters[param]
				if !exists {
					return fmt.Errorf("resource %q field %q: references unknown parameter %q",
						r.Name, p, param)
				}
				// A from: mapping becomes a bare `{{ $spec.<param> }}` in the
				// template body, which Go's template engine renders with fmt.
				// For a composite value that means `map[env:prod]` or
				// `[a b c]` -- and `[a b c]` is valid YAML that a
				// `type: array, items: {type: string}` schema happily accepts
				// as a one-element list containing "a b c". Legal, applied,
				// wrong. See the type: array branch above; the M2 fix for
				// both is `{{ $spec.x | toYaml | nindent N }}`.
				if compositeTypes[decl.Type] {
					return fmt.Errorf("resource %q field %q: parameter %q has type %q, and a from: "+
						"mapping cannot render a composite value in M1 -- it emits a bare "+
						"{{ $spec.%s }}, which Go's template engine formats with fmt "+
						"(an object renders as \"map[k:v]\", an array as \"[a b c]\"). Both are valid "+
						"YAML and silently wrong. Use a scalar parameter, or set the field with raw:",
						r.Name, p, param, decl.Type, param)
				}
			}
		}
	}
	return nil
}
