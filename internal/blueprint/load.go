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
var validTypes = map[string]bool{
	"string": true, "integer": true, "number": true,
	"boolean": true, "object": true, "array": true,
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

// Load reads and validates a blueprint file.
func Load(path string) (*Blueprint, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read blueprint: %w", err)
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
	case "Namespaced", "Cluster":
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
				_, exists := x.Parameters[param]
				if !exists {
					return fmt.Errorf("resource %q field %q: references unknown parameter %q",
						r.Name, p, param)
				}
			}
		}
	}
	return nil
}
