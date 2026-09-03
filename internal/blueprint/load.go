package blueprint

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
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
	b.SetSourcePath(path)
	return b, nil
}

// Parse decodes and validates raw blueprint YAML — the same gate Load runs
// on a file, exposed for callers that hold the bytes themselves (the HTTP
// import endpoint, tests).
func Parse(body []byte) (*Blueprint, error) {
	jsonBytes, err := yamlToJSON(body)
	if err != nil {
		return nil, fmt.Errorf("parse blueprint: %w", err)
	}
	var b Blueprint
	if err := json.Unmarshal(jsonBytes, &b); err != nil {
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
	for _, doc := range SplitDocs(body) {
		var meta struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		jsonDoc, err := yamlToJSON(doc)
		if err != nil || json.Unmarshal(jsonDoc, &meta) != nil || meta.Kind != "Configuration" {
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

// Validate reports the first structural problem, naming the offending field.
func (b *Blueprint) Validate() error {
	if err := b.validateRoot(); err != nil {
		return err
	}
	if err := validateSources(b.Spec.Sources); err != nil {
		return err
	}
	if err := validateXRD(b.Spec.XRD); err != nil {
		return err
	}
	if err := b.validateEnvironment(); err != nil {
		return err
	}
	if err := b.validateParameters(); err != nil {
		return err
	}
	if err := b.validateTemplates(); err != nil {
		return err
	}
	if err := b.validateResources(); err != nil {
		return err
	}
	return b.validatePipeline()
}

func (b *Blueprint) validateMetadataRef(r Resource, what, from string) error {
	target, path, ok := MetadataRef(from)
	if !ok || path != "name" {
		return fmt.Errorf("resource %q %s: a metadata reference must be resources.<name>.metadata.name (got %q)", r.Name, what, from)
	}
	if target == r.Name {
		return fmt.Errorf("resource %q %s: references its own metadata.name -- a resource cannot be wired to itself", r.Name, what)
	}
	decl := b.ResourceNamed(target)
	if decl == nil {
		return fmt.Errorf("resource %q %s: references unknown resource %q", r.Name, what, target)
	}
	if decl.ForEach != "" {
		return fmt.Errorf("resource %q %s: resource %q is looped (forEach: %s), so its name is indexed (%s-0, %s-1, ...) -- reference an unlooped resource",
			r.Name, what, target, decl.ForEach, target, target)
	}
	return nil
}

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

// validateForEachEnvRef checks the env.<key> forEach form: the loop bound is an integer environment key.
func (b *Blueprint) validateForEachEnvRef(r Resource) error {
	envKey, ok := strings.CutPrefix(r.ForEach, "env.")
	if !ok {
		return fmt.Errorf("resource %q: forEach must reference a parameter as params.<name>, an environment key as env.<key>, "+
			"or another resource's observed status as resources.<name>.status.<path> (got %q)",
			r.Name, r.ForEach)
	}
	decl, exists := b.Spec.Environment[envKey]
	if !exists {
		return UnknownEnvKeyError(fmt.Sprintf("resource %q: forEach", r.Name), envKey, b.Spec.Environment)
	}
	if decl.Type != "integer" {
		return fmt.Errorf("resource %q: forEach environment key %q has type %q, want integer -- "+
			"the loop bound renders as until (int $env.%s), a repetition count",
			r.Name, envKey, decl.Type, envKey)
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
