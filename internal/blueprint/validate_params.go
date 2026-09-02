package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

// yamlParamKeywords are YAML keywords that are boolean/null literals in YAML 1.2
// and cannot serve as parameter identifier names. Keywords like "n", "y", "yes", "no"
// are valid strings in YAML 1.2 and are handled cleanly.
var yamlParamKeywords = map[string]bool{
	"true": true, "false": true, "null": true,
}

// validateParameters validates spec.xrd.parameters definitions and required constraints.
func (b *Blueprint) validateParameters() error {
	x := b.Spec.XRD
	names := make([]string, 0, len(x.Parameters))
	for n := range x.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !paramNameRE.MatchString(n) || yamlParamKeywords[strings.ToLower(n)] {
			return fmt.Errorf("spec.xrd.parameters.%s: invalid parameter name "+
				"(must be camelCase, e.g. maxMessageSize, and not a YAML keyword like yes/no/true/false)", n)
		}
		p := x.Parameters[n]
		if p.Type == "array" {
			return fmt.Errorf("spec.xrd.parameters.%s: type \"array\" is not supported in M1. "+
				"The XRD emitter cannot write the required items: schema for it, and a from: "+
				"mapping would render Go's fmt of the slice (\"[a b c]\") -- valid YAML, silently "+
				"wrong. Use a scalar parameter, or a raw: field for a literal list", n)
		}
		if !validTypes[p.Type] {
			return fmt.Errorf("spec.xrd.parameters.%s: unknown type %q", n, p.Type)
		}
		if err := validateParameterScalars("spec.xrd.parameters."+n, p); err != nil {
			return err
		}
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

	hasManaged := false
	for _, r := range b.Spec.Resources {
		if r.Provider != NativeProvider {
			hasManaged = true
			break
		}
	}

	if x.Scope == "Namespaced" && hasManaged {
		p, ok := x.Parameters["providerName"]
		switch {
		case !ok:
			return fmt.Errorf("spec.xrd.parameters.providerName is required for a Namespaced XRD: " +
				"run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}")
		case p.Type != "string":
			return fmt.Errorf("spec.xrd.parameters.providerName: type must be string, got %q -- "+
				"it is rendered into providerConfigRef.name, which is a Kubernetes object name", p.Type)
		case !p.Required:
			return fmt.Errorf("spec.xrd.parameters.providerName: must be required: true -- " +
				"the Composition dereferences it unguarded for every composed resource, and only " +
				"the XRD's required gate makes that dereference safe")
		}
	}
	return nil
}
