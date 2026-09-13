package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

// validateXRD validates the structural fields of the composite XRD definition.
func validateXRD(x XRD) error {
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

	if name := x.Plural + "." + x.Group; len(name) > 63 {
		return fmt.Errorf("spec.xrd: name %q is %d characters (must be at most 63 characters; "+
			"Crossplane labels CompositionRevisions with this name and Kubernetes label values cap at 63 characters)",
			name, len(name))
	}

	switch x.Scope {
	case "Namespaced":
	case "Cluster":
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
	return nil
}

// validateStatus validates spec.xrd.status definitions and status wire constraints.
func (b *Blueprint) validateStatus() error {
	x := b.Spec.XRD
	if len(x.Status) == 0 {
		return nil
	}
	names := make([]string, 0, len(x.Status))
	for n := range x.Status {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		p := x.Status[n]
		if err := b.validateStatusParameter("spec.xrd.status."+n, p); err != nil {
			return err
		}
	}
	return nil
}

func (b *Blueprint) validateStatusParameter(path string, p Parameter) error {
	segs := strings.Split(path, ".")
	name := segs[len(segs)-1]
	if !paramNameRE.MatchString(name) || yamlParamKeywords[strings.ToLower(name)] {
		return fmt.Errorf("%s: invalid status field name "+
			"(must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)", path)
	}

	if p.Type == "array" {
		return fmt.Errorf("%s: type \"array\" is not supported in M1", path)
	}
	if !validTypes[p.Type] {
		return fmt.Errorf("%s: unknown type %q", path, p.Type)
	}

	if err := validateParameterScalars(path, p); err != nil {
		return err
	}

	if len(p.Properties) > 0 {
		if p.Type != "object" {
			return fmt.Errorf("%s: properties is only valid on type \"object\" (got type %q)", path, p.Type)
		}
		if p.From != "" {
			return fmt.Errorf("%s: an object with properties cannot specify from: directly", path)
		}
		propNames := make([]string, 0, len(p.Properties))
		for m := range p.Properties {
			propNames = append(propNames, m)
		}
		sort.Strings(propNames)
		for _, m := range propNames {
			if err := b.validateStatusParameter(path+".properties."+m, p.Properties[m]); err != nil {
				return err
			}
		}
		return nil
	}

	if p.From != "" {
		target, statusPath, ok := StatusRef(p.From)
		if !ok {
			return fmt.Errorf("%s: a status wire must reference another resource's observed status as resources.<name>.status.<path> (got %q)",
				path, p.From)
		}
		decl := b.ResourceNamed(target)
		if decl == nil {
			return fmt.Errorf("%s: references unknown resource %q", path, target)
		}
		if decl.ForEach != "" {
			return fmt.Errorf("%s: resource %q is looped (forEach: %s), so its composed documents are named %s-0, %s-1, ... and the un-indexed key %q never appears in the observed resources map -- the reference could never resolve. Reference an unlooped resource",
				path, target, decl.ForEach, target, target, target)
		}
		for _, seg := range strings.Split(statusPath, ".") {
			if !paramNameRE.MatchString(seg) {
				return fmt.Errorf("%s: status path segment %q in %q is not a valid field name (must be camelCase, e.g. atProvider.url)",
					path, seg, p.From)
			}
		}
	}
	return nil
}
