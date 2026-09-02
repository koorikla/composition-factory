// This file validates a resource's metadata.annotations entries: the checks
// that need no CRD, mirroring envelope.go's split — key grammar, the
// exactly-one-of value forms, and the reference checks that only need the
// document. The CRD half of a status wire (does the path name a scalar leaf
// in the source kind's status schema) belongs to internal/emit, which holds
// the resolved CRDs, exactly as it does for Fields.
package blueprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// annotationNameRE is the NAME half of a Kubernetes qualified name (the part
// after the optional prefix/): alphanumeric ends, '-', '_' and '.' inside —
// upstream validation.IsQualifiedName's name grammar, which is deliberately
// NOT paramNameRE: annotation names carry dashes and dots as a matter of
// course (role-arn, kubernetes.io/description-style names), and running them
// through the camelCase path grammar would refuse the entire idiom.
var annotationNameRE = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)

// Kubernetes' own length limits for a qualified name.
const (
	annotationPrefixMaxLen = 253
	annotationNameMaxLen   = 63
)

// reservedAnnotationKeys are the two spellings of the composition-resource-
// name annotation. The emitter writes {{ setResourceNameAnnotation "<name>" }}
// into every composed document's annotations block; the function renders that
// as its gotemplating.fn.crossplane.io/composition-resource-name key and
// translates it to crossplane.io/composition-resource-name on the composed
// resource. That key is node identity (spec §7): a blueprint entry for either
// spelling would sit in the same map as the function-set value and silently
// fight it — two writers, one key, whichever lands last wins. Refused at the
// source instead. Every OTHER key is fair game, crossplane.io/external-name
// included: authoring that one is a legitimate, common need.
var reservedAnnotationKeys = map[string]bool{
	"crossplane.io/composition-resource-name":                 true,
	"gotemplating.fn.crossplane.io/composition-resource-name": true,
}

// ReservedAnnotationKey reports whether k is one of the reserved
// composition-resource-name spellings. Exported for internal/emit, which
// repeats the refusal defensively (Composition is exported and callable
// without Generate's Validate step), per the same discipline as its other
// duplicated source-level checks.
func ReservedAnnotationKey(k string) bool { return reservedAnnotationKeys[k] }

// checkAnnotationKey validates one annotation key against the Kubernetes
// qualified-name rules (an optional DNS-subdomain prefix + '/' + name).
// Control characters are the caller's job (checkScalar) so this function's
// errors can talk about shape alone.
func checkAnnotationKey(k string) error {
	if k == "" {
		return fmt.Errorf("annotation key is empty")
	}
	if strings.Count(k, "/") > 1 {
		return fmt.Errorf("an annotation key has at most one '/' (an optional DNS-subdomain " +
			"prefix, then a name, e.g. eks.amazonaws.com/role-arn)")
	}
	name := k
	if prefix, rest, hasPrefix := strings.Cut(k, "/"); hasPrefix {
		if prefix == "" {
			return fmt.Errorf("the prefix before '/' is empty; drop the slash or name a " +
				"DNS-subdomain prefix (e.g. eks.amazonaws.com/role-arn)")
		}
		if len(prefix) > annotationPrefixMaxLen {
			return fmt.Errorf("the prefix %q is %d characters; a DNS-subdomain prefix is at most %d",
				prefix, len(prefix), annotationPrefixMaxLen)
		}
		if !groupRE.MatchString(prefix) {
			return fmt.Errorf("the prefix %q is not a DNS subdomain (lowercase alphanumerics, '-' "+
				"and '.', e.g. eks.amazonaws.com)", prefix)
		}
		name = rest
	}
	if name == "" {
		return fmt.Errorf("the name after '/' is empty; an annotation key is <prefix>/<name> " +
			"or a bare <name>")
	}
	if len(name) > annotationNameMaxLen {
		return fmt.Errorf("the name %q is %d characters; an annotation name is at most %d",
			name, len(name), annotationNameMaxLen)
	}
	if !annotationNameRE.MatchString(name) {
		return fmt.Errorf("the name %q is not a valid annotation name (alphanumeric ends, with "+
			"'-', '_' and '.' allowed inside, e.g. role-arn)", name)
	}
	return nil
}

// validateResourceAnnotations checks one resource's annotations entries.
// Called from Validate inside the resources loop, after the fields and
// envelope checks, so errors keep the first-problem contract and name the
// resource. Keys are visited in sorted order for the same reason every other
// loop here sorts: the same blueprint must name the same problem first.
func (b *Blueprint) validateResourceAnnotations(r Resource) error {
	x := b.Spec.XRD
	keys := make([]string, 0, len(r.Annotations))
	for k := range r.Annotations {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		label := fmt.Sprintf("annotation %q", k)
		// The key becomes a (quoted) YAML map key inside the Composition's
		// `template: |` block scalar; quoting handles colons and keywords, but
		// a line break still escapes the single-line construct — checkScalar's
		// whole reason to exist — so it runs before the shape check.
		if err := checkScalar(fmt.Sprintf("resource %q %s: key", r.Name, label), k); err != nil {
			return err
		}
		if err := checkAnnotationKey(k); err != nil {
			return fmt.Errorf("resource %q %s: %w", r.Name, label, err)
		}
		if reservedAnnotationKeys[k] {
			return fmt.Errorf("resource %q %s: this key is the composition-resource-name annotation, "+
				"which the generator writes itself via setResourceNameAnnotation — it is how Crossplane "+
				"keys composed resources (node identity), and a blueprint entry for it would silently "+
				"collide with the function-set value. The resource's name field is the one source of "+
				"truth for it", r.Name, label)
		}

		f := r.Annotations[k]
		set := 0
		for _, v := range []string{f.From, f.Value, f.Raw, f.Template} {
			if v != "" {
				set++
			}
		}
		if set != 1 {
			return fmt.Errorf("resource %q %s: set exactly one of from, value, raw or template (got %d)",
				r.Name, label, set)
		}
		// Same single-line discipline as fields, same reason: every one of
		// these lands inside the Composition's `template: |` block scalar,
		// where a line break escapes every enclosing context. Same inspection
		// order as the fields loop, so the first error is deterministic.
		for _, src := range []struct{ lbl, val string }{
			{"from", f.From}, {"raw", f.Raw}, {"template", f.Template}, {"value", f.Value},
		} {
			if err := checkScalar(fmt.Sprintf("resource %q %s: %s", r.Name, label, src.lbl), src.val); err != nil {
				return err
			}
		}

		if f.Raw != "" && b.Engine() != EngineGoTemplating && strings.Contains(f.Raw, "{{") {
			return fmt.Errorf("resource %q %s: raw %q contains Go-template syntax \"{{\" which is only supported with the go-templating engine (current engine is %q)",
				r.Name, label, f.Raw, b.Engine())
		}

		if f.Template != "" {
			// Deliberately NO native refusal here, unlike fields: the fields
			// rule exists because a template call's output re-indents to the
			// fixed forProvider column, which a native field at an arbitrary
			// depth breaks — but every annotation sits at one fixed column
			// (metadata.annotations children, identical for both families), so
			// the mechanical reason does not apply. See Resource.Annotations.
			if _, ok := b.Spec.Templates[f.Template]; !ok {
				return fmt.Errorf("resource %q %s: references unknown template %q "+
					"(declare it under spec.templates)", r.Name, label, f.Template)
			}
		}
		if f.From != "" {
			ref, err := ParseFrom(f.From)
			if err != nil {
				return fmt.Errorf("resource %q %s: %w", r.Name, label, err)
			}
			if ref.Resource != "" {
				if ref.IsMetadataName() {
					if err := b.validateMetadataRef(r, label, f.From); err != nil {
						return err
					}
				} else {
					if err := b.validateStatusRef(r, label, f.From); err != nil {
						return err
					}
				}
				continue
			}
			decl, exists := x.Parameters[ref.Param]
			if !exists {
				return fmt.Errorf("resource %q %s: references unknown parameter %q",
					r.Name, label, ref.Param)
			}
			// Same rule as a field's from, same failure mode — with the extra
			// twist that an annotation value must be a STRING, so even the
			// emitter's quote pipe could only ever render Go's fmt of a
			// composite ("map[k:v]") as a quoted string: legal, silently wrong.
			if compositeTypes[decl.Type] {
				return fmt.Errorf("resource %q %s: parameter %q has type %q, and an annotation wire "+
					"can only carry a scalar — a composite would render Go's fmt of the value "+
					"(\"map[k:v]\", \"[a b c]\") as the annotation string, legal YAML and silently "+
					"wrong. Use a scalar parameter, or set the annotation with raw:",
					r.Name, label, ref.Param, decl.Type)
			}
		}
	}
	return nil
}
