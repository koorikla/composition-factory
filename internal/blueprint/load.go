package blueprint

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

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
		if t := x.Parameters[n].Type; !validTypes[t] {
			return fmt.Errorf("spec.xrd.parameters.%s: unknown type %q", n, t)
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
			if f.From != "" {
				param, ok := strings.CutPrefix(f.From, "params.")
				if !ok {
					return fmt.Errorf("resource %q field %q: from must start with params. (got %q)",
						r.Name, p, f.From)
				}
				if _, exists := x.Parameters[param]; !exists {
					return fmt.Errorf("resource %q field %q: references unknown parameter %q",
						r.Name, p, param)
				}
			}
		}
	}
	return nil
}

// DereferencedParams returns the parameter names any template actually reads,
// sorted. Every one must be required in the emitted XRD so a missing value
// fails at the XR gate rather than rendering the literal string "<no value>".
func (b *Blueprint) DereferencedParams() []string {
	seen := map[string]bool{}
	for _, r := range b.Spec.Resources {
		for _, f := range r.Fields {
			if param, ok := strings.CutPrefix(f.From, "params."); ok && f.From != "" {
				seen[param] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
