package blueprint

import (
	"fmt"
	"os"
	"regexp"
	"slices"
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

// memberTypes are the types an object parameter's member may declare in v1:
// scalars only. An object member would be a second nesting level (refused —
// one level in v1), and an array member has both of the problems that keep
// type: array off the top level (no items: schema, Go-fmt rendering behind
// from:).
var memberTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true,
}

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

// validateParameterScalars checks the free-text scalar declarations shared
// by top-level parameters and object members — description, default and enum
// content (see checkScalar), and the default-vs-type rule. fieldPath names
// the parameter or member in errors (e.g. spec.xrd.parameters.tuning, or
// spec.xrd.parameters.tuning.properties.maxSize). One helper rather than two
// copies: a member takes the SAME declarations a top-level parameter does,
// and two hand-synchronised rule sets would drift the first time one gained
// a case.
func validateParameterScalars(fieldPath string, p Parameter) error {
	// Description, default and every enum entry are user-authored free text
	// that internal/emit/xrd.go writes straight into the XRD. See checkScalar:
	// a newline in any of them grows the XRD a bogus top-level key while
	// leaving it parseable.
	if err := checkScalar(fieldPath+".description", p.Description); err != nil {
		return err
	}
	if err := checkScalar(fieldPath+".default", p.Default); err != nil {
		return err
	}
	for i, e := range p.Enum {
		if err := checkScalar(fmt.Sprintf("%s.enum[%d]", fieldPath, i), e); err != nil {
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
			return fmt.Errorf("%s: default is not valid for type %q "+
				"(only string, integer, number and boolean defaults are supported)", fieldPath, p.Type)
		case "boolean":
			if p.Default != "true" && p.Default != "false" {
				return fmt.Errorf("%s: default %q is not a valid boolean "+
					`(must be "true" or "false")`, fieldPath, p.Default)
			}
		case "integer":
			if _, err := strconv.ParseInt(p.Default, 10, 64); err != nil {
				return fmt.Errorf("%s: default %q is not a valid integer", fieldPath, p.Default)
			}
		case "number":
			if _, err := strconv.ParseFloat(p.Default, 64); err != nil {
				return fmt.Errorf("%s: default %q is not a valid number", fieldPath, p.Default)
			}
		}
	}
	return nil
}

// validateParameterMembers checks a typed object parameter's declared
// members: identifier-shaped names (each becomes a raw YAML map key in the
// emitted XRD schema, the same position a parameter name occupies), scalar
// types only, no nesting past one level, and the same scalar/default rules
// top-level parameters get. Members are visited in sorted order so the same
// blueprint names the same problem first, every time.
func validateParameterMembers(paramPath string, p Parameter) error {
	names := make([]string, 0, len(p.Properties))
	for m := range p.Properties {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, m := range names {
		mPath := paramPath + ".properties." + m
		if !paramNameRE.MatchString(m) || yamlKeywords[strings.ToLower(m)] {
			return fmt.Errorf("%s: invalid member name "+
				"(must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)", mPath)
		}
		mp := p.Properties[m]
		// An object member recurses: members nest to arbitrary depth (the
		// openapi-editor shape). Propertyless keeps the free-form string
		// map the top-level rule gives it. Objects carry no default/enum —
		// both are scalar concepts.
		if mp.Type == "object" {
			if mp.Default != "" || len(mp.Enum) > 0 {
				return fmt.Errorf("%s: an object member takes no default or enum — those belong on "+
					"its scalar members", mPath)
			}
			if err := validateParameterMembers(mPath, mp); err != nil {
				return err
			}
			continue
		}
		if len(mp.Properties) > 0 {
			return fmt.Errorf("%s: properties are only valid on type: object members (this member is %q)",
				mPath, mp.Type)
		}
		if !memberTypes[mp.Type] {
			if mp.Type == "array" {
				return fmt.Errorf("%s: member type \"array\" is not supported — members are scalar "+
					"(string, integer, number, boolean) or nested object", mPath)
			}
			return fmt.Errorf("%s: unknown type %q", mPath, mp.Type)
		}
		if err := validateParameterScalars(mPath, mp); err != nil {
			return err
		}
	}
	return nil
}

// resolveParamRef resolves a from: parameter reference — params.<name> or
// params.<name>.<member> — against the declared parameters, returning the
// declaration whose type governs the wire: the member's for a member
// reference, the parameter's own otherwise. context prefixes every error
// (e.g. `resource "q" field "x"`). The params. prefix must already be
// stripped by the caller, whose own fallback error names its full grammar.
func resolveParamRef(x XRD, context, ref string) (Parameter, error) {
	_, chain, err := ParamChain(x, context, ref)
	if err != nil {
		return Parameter{}, err
	}
	return chain[len(chain)-1], nil
}

// ParamChain walks a dotted parameter reference (post-"params." — "obj",
// "obj.member", "obj.a.b", any depth) through the declared parameter tree,
// returning the segments and the declaration at every level. The chain is
// what guard emission needs: a dereference is only safe past the last
// all-required prefix, and each level's Required lives on its own decl.
func ParamChain(x XRD, context, ref string) ([]string, []Parameter, error) {
	segs := strings.Split(ref, ".")
	decl, exists := x.Parameters[segs[0]]
	if !exists {
		return nil, nil, fmt.Errorf("%s: references unknown parameter %q", context, segs[0])
	}
	chain := []Parameter{decl}
	for i := 1; i < len(segs); i++ {
		cur := chain[i-1]
		at := strings.Join(segs[:i], ".")
		if cur.Type != "object" {
			return nil, nil, fmt.Errorf("%s: params.%s has type %q, not \"object\" — a member "+
				"reference needs an object with declared properties", context, at, cur.Type)
		}
		if len(cur.Properties) == 0 {
			return nil, nil, fmt.Errorf("%s: params.%s declares no properties — a member reference "+
				"needs a typed object; declare the member under its properties", context, at)
		}
		m, ok := cur.Properties[segs[i]]
		if !ok {
			return nil, nil, fmt.Errorf("%s: references unknown member %q of params.%s "+
				"(declared members: %s)", context, segs[i], at, strings.Join(sortedMemberNames(cur), ", "))
		}
		chain = append(chain, m)
	}
	return segs, chain, nil
}

// sortedMemberNames lists a typed object parameter's members, sorted, for
// error messages and the XRD emitter's deterministic iteration.
func sortedMemberNames(p Parameter) []string {
	out := make([]string, 0, len(p.Properties))
	for m := range p.Properties {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
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
	b, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// Parse decodes and validates raw blueprint YAML — the same gate Load runs
// on a file, exposed for callers that hold the bytes themselves (the HTTP
// import endpoint, tests).
func Parse(body []byte) (*Blueprint, error) {
	var b Blueprint
	if err := yaml.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("parse blueprint: %w", err)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// BlueprintAnnotation is where cf package embeds the blueprint source in a
// Configuration meta document, and where ParseAny recovers it from.
const BlueprintAnnotation = "factory.crossplane.io/blueprint"

// ParseAny accepts either raw blueprint YAML or a package.yaml stream (the
// "output yaml" form of cf package): a stream whose Configuration meta
// document carries the blueprint under the factory.crossplane.io/blueprint
// annotation is unwrapped and the embedded blueprint goes through the same
// Parse gate. Everything else fails with Parse's own error.
func ParseAny(body []byte) (*Blueprint, error) {
	b, perr := Parse(body)
	if perr == nil {
		return b, nil
	}
	for _, doc := range splitDocs(body) {
		var meta struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if yaml.Unmarshal(doc, &meta) != nil || meta.Kind != "Configuration" {
			continue
		}
		src, ok := meta.Metadata.Annotations[BlueprintAnnotation]
		if !ok {
			return nil, fmt.Errorf("Configuration package has no %s annotation to recover a blueprint from", BlueprintAnnotation)
		}
		b, err := Parse([]byte(src))
		if err != nil {
			return nil, fmt.Errorf("embedded blueprint: %w", err)
		}
		return b, nil
	}
	return nil, perr
}

// splitDocs splits a multi-document YAML stream on "---" at column zero.
func splitDocs(in []byte) [][]byte {
	return SplitDocs(in)
}

// Validate reports the first structural problem, naming the offending field.
func (b *Blueprint) Validate() error {
	if b.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion: %q is not valid (must be %q)", b.APIVersion, APIVersion)
	}
	if b.Kind != Kind {
		return fmt.Errorf("kind: %q is not valid (must be %q)", b.Kind, Kind)
	}

	// metadata.name reaches every generated file's provenance header
	// (emit.header writes "# Source: blueprints/<name>.cf.yaml"). A newline
	// there ends the comment and puts whatever follows at column 0 of a
	// document that otherwise parses fine.
	if err := checkScalar("metadata.name", b.Metadata.Name); err != nil {
		return err
	}

	if b.Spec.Emit != nil {
		if b.Spec.Emit.TemplateSource != "" {
			switch b.Spec.Emit.TemplateSource {
			case TemplateSourceInline, TemplateSourceFileSystem:
			default:
				return fmt.Errorf("spec.emit.templateSource: %q is not a valid template source (must be %q or %q)",
					b.Spec.Emit.TemplateSource, TemplateSourceInline, TemplateSourceFileSystem)
			}
		}
		if b.Spec.Emit.Engine != "" {
			switch strings.ToLower(b.Spec.Emit.Engine) {
			case EngineGoTemplating, EngineKCL, EnginePython:
			default:
				return fmt.Errorf("spec.emit.engine: %q is not a valid engine (must be %q, %q, or %q)",
					b.Spec.Emit.Engine, EngineGoTemplating, EngineKCL, EnginePython)
			}
		}
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
		// A crds: source is a CRD manifest file, not a package — its own
		// checks and nothing from the provider branch below.
		if s.CRDs != "" {
			if s.Provider != "" {
				return fmt.Errorf("spec.sources[%d]: provider and crds are mutually exclusive — "+
					"a source is either a provider package or a CRD manifest file", i)
			}
			if err := checkScalar(fmt.Sprintf("spec.sources[%d].crds", i), s.CRDs); err != nil {
				return err
			}
			// The .yaml suffix is the family marker: emit's resolveKind
			// routes resources whose provider ends in .yaml/.yml down the
			// object-rooted path, and no OCI package ref can carry it.
			if !strings.HasSuffix(s.CRDs, ".yaml") && !strings.HasSuffix(s.CRDs, ".yml") {
				return fmt.Errorf("spec.sources[%d].crds: %q must be a .yaml/.yml file path", i, s.CRDs)
			}
			continue
		}
		if s.Provider == "" {
			return fmt.Errorf("spec.sources[%d]: one of provider (a package ref) or crds (a CRD manifest file) is required", i)
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
		// Description, default and enum are validated by the same helper an
		// object member's are — one set of rules, one code path.
		if err := validateParameterScalars("spec.xrd.parameters."+n, p); err != nil {
			return err
		}
		// properties turns an object parameter into a typed one; on any other
		// type there is no member schema for it to describe, so it is a
		// mistake to refuse loudly rather than a declaration to ignore.
		if len(p.Properties) > 0 {
			if p.Type != "object" {
				return fmt.Errorf("spec.xrd.parameters.%s: properties is only valid on type \"object\" "+
					"(got type %q) — only an object parameter has members to declare", n, p.Type)
			}
			if err := validateParameterMembers("spec.xrd.parameters."+n, p); err != nil {
				return err
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

	// Templates and conventions are validated before the resources loop:
	// a field's template: <name> reference below is checked against the set
	// this call has already accepted.
	if err := b.validateTemplates(); err != nil {
		return err
	}

	// The full resource-name set is built before the per-resource loop
	// because a status wire may legally reference a resource declared LATER
	// in the list (observed.resources is keyed by name at render time, so
	// declaration order carries no meaning), and the existence check below
	// needs every name up front. Uniqueness is enforced here too: a
	// resources.<name> reference must be unambiguous, and the name is also
	// the composition-resource-name annotation — node identity (spec §7) —
	// so two resources sharing one name would silently collapse into one
	// composed resource.
	resourceNames := make(map[string]bool, len(b.Spec.Resources))
	for i, r := range b.Spec.Resources {
		if resourceNames[r.Name] {
			return fmt.Errorf("spec.resources[%d]: duplicate resource name %q -- the name is the "+
				"composition-resource-name annotation (node identity) and the key status wires "+
				"reference, so it must be unique", i, r.Name)
		}
		if r.Name != "" {
			resourceNames[r.Name] = true
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
			// A resource may reference a crds: source by its path — those
			// refs skip the OCI shape check (they are file paths).
			crdsSource := false
			for _, src := range b.Spec.Sources {
				if src.CRDs != "" && src.CRDs == r.Provider {
					crdsSource = true
					break
				}
			}
			isSpecial := r.Provider == NativeProvider || r.Provider == "cluster" || crdsSource
			if !isSpecial && !providerRefRE.MatchString(r.Provider) {
				return fmt.Errorf("spec.resources[%d].provider: %q is not a valid provider reference "+
					"(e.g. ghcr.io/org/provider-name:v1.2.3, or ...@sha256:<digest>)", i, r.Provider)
			}
			// spec.sources is the dependency manifest: startup and generate
			// load provider schemas from it alone, so a resource pinned to a
			// provider nobody declared works on a warm server (the runtime
			// add extended the index) and then fails hours later, after a
			// restart, as "kind not found in any cached provider". Native
			// and cluster kinds are exempt.
			if !isSpecial {
				declared := false
				for _, src := range b.Spec.Sources {
					if src.Provider == r.Provider {
						declared = true
						break
					}
				}
				if !declared {
					return fmt.Errorf("spec.resources[%d] (%q): provider %q is not declared in "+
						"spec.sources; add it there so generation can load its schemas after a restart",
						i, r.Name, r.Provider)
				}
			}
		}
		// Templates and conventions are forProvider-plan features in v1, and a
		// native Kubernetes kind has no forProvider plan. Conventions simply
		// DO NOT APPLY to native resources (the emitter skips them — a
		// native object's top-level leaves are structural fields, a Secret's
		// type or a ConfigMap's data, where a silently defaulted value would
		// change workload semantics), so a blueprint may freely mix
		// conventions with native kinds. Only an explicit template: FIELD on
		// a native resource stays refused: a template call's output
		// re-indents to the fixed forProvider column (templateFieldNindent),
		// which a native field at an arbitrary nesting depth breaks.
		// forEach repeats the resource's whole rendered document N times, N
		// read at render time from an integer XRD parameter
		// (internal/emit/composition.go wraps the document in
		// `{{- range $i := until (int $spec.<name>) }}`). That is a bare,
		// unguarded dereference of the loop bound, and under
		// options: ["missingkey=error"] an absent key is a hard render
		// failure — so the parameter must be one whose presence in the
		// observed composite's spec is unconditional. Two XRD gates provide
		// that, and only those two: a required parameter is present on any
		// XR the API server admits, and a defaulted parameter is injected
		// into the XR's spec by schema defaulting before the composition
		// function ever sees it. A parameter that is neither can be
		// genuinely absent at render time, so it cannot be a loop bound.
		// A second forEach form reads the loop bound from another resource's
		// OBSERVED status (resources.<name>.status.<path>) — the same
		// reference grammar as a field's from:, with the CRD-schema half (the
		// path names an integer/number status leaf) checked in internal/emit,
		// which holds the CRDs. The observed bound needs none of the
		// required-or-default machinery below: it is dereferenced BEHIND the
		// same hasKey guard chain a status wire uses, so an unobserved source
		// renders zero instances instead of hard-failing (see
		// internal/emit/composition.go).
		if r.ForEach != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].forEach", i), r.ForEach); err != nil {
				return err
			}
			if strings.HasPrefix(r.ForEach, statusRefPrefix) {
				// The parameter rules below are the params form's alone.
				if err := b.validateForEachStatusRef(r); err != nil {
					return err
				}
			} else if err := validateForEachParamRef(x, r); err != nil {
				return err
			}
		}
		// when wraps the resource's whole rendered document in a template
		// conditional that dereferences its parameter unguarded, so the
		// parameter gets exactly forEach's required-or-default rule, for
		// exactly forEach's reason. The grammar is pinned by ParseWhen; the
		// type rules here keep the condition honest: a bare form on a
		// non-boolean would test Go-template truthiness of an arbitrary
		// value, and a comparison on a non-string would compare against a
		// value the schema says can never be a string. Both are conditions
		// that "work" and are silently wrong.
		if r.When != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].when", i), r.When); err != nil {
				return err
			}
			// Conditions stay top-level in v1, like loop bounds. ParseWhen's
			// grammar already refuses a dotted parameter, but its generic
			// grammar error would send the author to the wrong fix — the
			// member-shaped case gets the ruling named instead.
			if head, _, _ := strings.Cut(r.When, " "); strings.HasPrefix(head, "params.") &&
				strings.Contains(strings.TrimPrefix(head, "params."), ".") {
				return fmt.Errorf("resource %q: when cannot reference an object member (got %q) — "+
					"conditions reference top-level parameters only in v1", r.Name, head)
			}
			param, op, literal, err := ParseWhen(r.When)
			if err != nil {
				return fmt.Errorf("resource %q: %w", r.Name, err)
			}
			decl, exists := x.Parameters[param]
			if !exists {
				return fmt.Errorf("resource %q: when references unknown parameter %q", r.Name, param)
			}
			if !decl.Required && decl.Default == "" {
				return fmt.Errorf("resource %q: when parameter %q must be required or carry a default -- "+
					`the condition dereferences it unguarded, and under options: ["missingkey=error"] `+
					"an absent key hard-fails the whole render; only the XRD's required gate or its "+
					"schema default makes the key's presence unconditional", r.Name, param)
			}
			switch op {
			case "":
				if decl.Type != "boolean" {
					return fmt.Errorf("resource %q: when parameter %q has type %q, want boolean -- "+
						"the bare form renders {{- if $spec.%s }}, a truthiness test; compare a string "+
						`parameter explicitly: when: params.%s == "<literal>"`,
						r.Name, param, decl.Type, param, param)
				}
			default: // "==" or "!=", ParseWhen admits nothing else
				if decl.Type != "string" {
					return fmt.Errorf("resource %q: when parameter %q has type %q, want string -- "+
						"the %s form compares against a string literal", r.Name, param, decl.Type, op)
				}
				if len(decl.Enum) > 0 && !slices.Contains(decl.Enum, literal) {
					return fmt.Errorf("resource %q: when literal %q is not among parameter %q's enum values %v -- "+
						"the XRD schema admits no XR carrying it, so the condition would be constant: "+
						"a resource that silently never (or always) exists", r.Name, literal, param, decl.Enum)
				}
			}
		}
		paths := make([]string, 0, len(r.Fields))
		for p := range r.Fields {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			basePath, mapKey, isMap := ParseFieldPath(p)
			if isMap {
				if mapKey == "" {
					return fmt.Errorf("resource %q field %q: empty map key inside brackets", r.Name, p)
				}
				if err := checkScalar(fmt.Sprintf("resource %q field %q: map key", r.Name, p), mapKey); err != nil {
					return err
				}
				if _, hasWhole := r.Fields[basePath]; hasWhole {
					return fmt.Errorf("resource %q field %q conflicts with field %q, which sets the whole map; "+
						"set the whole map or set individual keys, not both", r.Name, p, basePath)
				}
			}
			f := r.Fields[p]
			set := 0
			for _, v := range []string{f.From, f.Value, f.Raw, f.Template} {
				if v != "" {
					set++
				}
			}
			if set != 1 {
				return fmt.Errorf("resource %q field %q: set exactly one of from, value, raw or template (got %d)",
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
				{"from", f.From}, {"raw", f.Raw}, {"template", f.Template}, {"value", f.Value},
			} {
				if err := checkScalar(fmt.Sprintf("resource %q field %q: %s", r.Name, p, src.label), src.val); err != nil {
					return err
				}
			}
			if f.Template != "" {
				if _, ok := b.Spec.Templates[f.Template]; !ok {
					return fmt.Errorf("resource %q field %q: references unknown template %q "+
						"(declare it under spec.templates)", r.Name, p, f.Template)
				}
				// Same v1 ruling as conventions above, for the mechanical half
				// of the reason: a template call's output is re-indented to the
				// fixed forProvider field column (templateFieldNindent in
				// internal/emit), and a native field at any deeper nesting level
				// would take that output at the wrong column — structurally
				// broken YAML in the rendered document.
				if r.Provider == NativeProvider {
					return fmt.Errorf("resource %q field %q: template: fields are not supported on a native "+
						"Kubernetes resource (provider %q) in v1 -- a template call's output re-indents to "+
						"the fixed forProvider column, which a native field at an arbitrary nesting depth "+
						"breaks. Set the field with value:, raw: or from:", r.Name, p, NativeProvider)
				}
			}
			if f.From != "" {
				ref, err := ParseFrom(f.From)
				if err != nil {
					return fmt.Errorf("resource %q field %q: %w", r.Name, p, err)
				}
				if ref.Resource != "" {
					if err := b.validateStatusRef(r, fmt.Sprintf("field %q", p), f.From); err != nil {
						return err
					}
					continue
				}
				// resolveParamRef returns the governing declaration: the
				// member's for a params.<name>.<member> reference (always a
				// scalar, by member validation above), the parameter's own
				// otherwise — so the composite check below applies to exactly
				// the type the wire would render. ref.Param carries everything
				// after the params. prefix, member half included — ParseFrom
				// is the single grammar entry point, resolveParamRef the
				// member-aware resolver behind it.
				decl, err := resolveParamRef(x, fmt.Sprintf("resource %q field %q", r.Name, p), ref.Param)
				if err != nil {
					return err
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
					param, _, _ := ParamRef(f.From)
					// A typed object has a working alternative — say so
					// instead of dead-ending at the generic composite ruling.
					if decl.Type == "object" && len(decl.Properties) > 0 {
						return fmt.Errorf("resource %q field %q: parameter %q is a typed object — a from: "+
							"mapping cannot render the whole object; wire one of its declared members "+
							"instead (params.%s.<member>; declared members: %s)",
							r.Name, p, param, param, strings.Join(sortedMemberNames(decl), ", "))
					}
					return fmt.Errorf("resource %q field %q: parameter %q has type %q, and a from: "+
						"mapping cannot render a composite value in M1 -- it emits a bare "+
						"{{ $spec.%s }}, which Go's template engine formats with fmt "+
						"(an object renders as \"map[k:v]\", an array as \"[a b c]\"). Both are valid "+
						"YAML and silently wrong. Use a scalar parameter, or set the field with raw:",
						r.Name, p, param, decl.Type, param)
				}
			}
		}
		// Envelope entries get the same structural discipline as fields (see
		// envelope.go); schema-aware checks live in internal/emit, which
		// holds the resolved CRD.
		if err := validateResourceEnvelope(x, r); err != nil {
			return err
		}
		// Annotations too (see annotations.go): key grammar and value forms
		// here, status-schema checks at emit time.
		if err := b.validateResourceAnnotations(r); err != nil {
			return err
		}
	}

	// spec.pipeline last: its checks are self-contained (see pipeline.go), so
	// putting them after the resource checks keeps the first-error contract of
	// every existing case unchanged.
	return b.validatePipeline()
}

// validateStatusRef checks the blueprint-level half of a cross-resource
// status reference (resources.<name>.status.<path>): grammar, that the
// referenced resource is declared, that the reference is not to the
// resource's own status, that the target is not looped, and that every path
// segment is a clean identifier (each one reaches emitted template text
// inside hasKey guards and a dereference expression, so the identifier check
// is a structural requirement, not a style rule). The CRD-schema half — does
// <path> name a scalar leaf in the referenced kind's declared status —
// belongs to internal/emit, which holds the CRDs.
//
// what names where the reference sits on r, preformatted (`field "podSpec"`,
// `annotation "eks.amazonaws.com/role-arn"`), so one checker serves both wire
// surfaces without the field messages changing a byte.
func (b *Blueprint) validateStatusRef(r Resource, what, from string) error {
	target, path, ok := StatusRef(from)
	if !ok {
		return fmt.Errorf("resource %q %s: a resources. reference must be "+
			"resources.<name>.status.<path>, e.g. resources.main-queue.status.atProvider.url (got %q)",
			r.Name, what, from)
	}
	if target == r.Name {
		return fmt.Errorf("resource %q %s: references its own status -- a resource cannot be "+
			"wired to itself; the value it would read is the one its own document produces", r.Name, what)
	}
	decl := b.ResourceNamed(target)
	if decl == nil {
		return fmt.Errorf("resource %q %s: references unknown resource %q", r.Name, what, target)
	}
	if decl.ForEach != "" {
		return fmt.Errorf("resource %q %s: resource %q is looped (forEach: %s), so its composed "+
			"documents are named %s-0, %s-1, ... and the un-indexed key %q never appears in the observed "+
			"resources map -- the reference could never resolve. Reference an unlooped resource",
			r.Name, what, target, decl.ForEach, target, target, target)
	}
	for _, seg := range strings.Split(path, ".") {
		if !paramNameRE.MatchString(seg) {
			return fmt.Errorf("resource %q %s: status path segment %q in %q is not a valid "+
				"field name (must be camelCase, e.g. atProvider.url) -- each segment is written into "+
				"the emitted template as a dereference and a hasKey guard", r.Name, what, seg, from)
		}
	}
	return nil
}

// validateForEachParamRef checks the params.<name> forEach form: the loop
// bound is an integer XRD parameter that is required or carries a default.
// The Composition dereferences this bound UNGUARDED, and under
// options: ["missingkey=error"] an absent key is a hard render failure — so
// the parameter must be one whose presence in the observed composite's spec
// is unconditional. Two XRD gates provide that, and only those two: a
// required parameter is present on any XR the API server admits, and a
// defaulted parameter is injected into the XR's spec by schema defaulting
// before the composition function ever sees it.
func validateForEachParamRef(x XRD, r Resource) error {
	param, ok := strings.CutPrefix(r.ForEach, "params.")
	if !ok {
		return fmt.Errorf("resource %q: forEach must reference a parameter as params.<name>, "+
			"or another resource's observed status as resources.<name>.status.<path> (got %q)",
			r.Name, r.ForEach)
	}
	// Loop bounds stay top-level in v1: a member is addressable by a
	// field's from:, but not as a repetition count. Rejected with the
	// scope named rather than falling through to a confusing
	// unknown-parameter error for "obj.member".
	if name, member, nested := strings.Cut(param, "."); nested {
		return fmt.Errorf("resource %q: forEach cannot reference member %q of parameter %q — "+
			"loop bounds stay top-level integer parameters in v1 (got %q)",
			r.Name, member, name, r.ForEach)
	}
	decl, exists := x.Parameters[param]
	if !exists {
		return fmt.Errorf("resource %q: forEach references unknown parameter %q", r.Name, param)
	}
	if decl.Type != "integer" {
		return fmt.Errorf("resource %q: forEach parameter %q has type %q, want integer -- "+
			"the loop bound renders as until (int $spec.%s), a repetition count",
			r.Name, param, decl.Type, param)
	}
	if !decl.Required && decl.Default == "" {
		return fmt.Errorf("resource %q: forEach parameter %q must be required or carry a default -- "+
			`the loop bound is dereferenced unguarded, and under options: ["missingkey=error"] `+
			"an absent key hard-fails the whole render; only the XRD's required gate or its "+
			"schema default makes the key's presence unconditional", r.Name, param)
	}
	return nil
}

// validateForEachStatusRef checks the blueprint-level half of an
// observed-count loop bound (forEach: resources.<name>.status.<path>).
func (b *Blueprint) validateForEachStatusRef(r Resource) error {
	return b.validateStatusRef(r, "forEach", r.ForEach)
}

// ParseFieldPath parses a field path, separating the base path from an optional map key.
// Examples:
// "tags[Environment]" -> basePath: "tags", key: "Environment", isMap: true
// "tags[\"Environment\"]" -> basePath: "tags", key: "Environment", isMap: true
// "spec.selector.matchLabels[app]" -> basePath: "spec.selector.matchLabels", key: "app", isMap: true
// "queueName" -> basePath: "queueName", key: "", isMap: false
func ParseFieldPath(p string) (basePath, key string, isMap bool) {
	if strings.HasSuffix(p, "]") {
		if idx := strings.LastIndex(p, "["); idx > 0 {
			inner := p[idx+1 : len(p)-1]
			if inner != "0" && !isDigits(inner) {
				inner = strings.Trim(inner, `"'`)
				return p[:idx], inner, true
			}
		}
	}
	return p, "", false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
