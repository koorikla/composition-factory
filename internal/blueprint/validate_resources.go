package blueprint

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// validateResources validates the spec.resources list, resource-level properties, and fields.
func (b *Blueprint) validateResources() error {
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
		if err := checkScalar(fmt.Sprintf("spec.resources[%d].kind", i), r.Kind); err != nil {
			return err
		}
		if !kindRE.MatchString(r.Kind) {
			return fmt.Errorf("spec.resources[%d].kind: %q is not a valid Kind (must start with an uppercase letter, e.g. Queue)", i, r.Kind)
		}
		if r.Provider != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].provider", i), r.Provider); err != nil {
				return err
			}
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
		if r.ForEach != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].forEach", i), r.ForEach); err != nil {
				return err
			}
			if strings.HasPrefix(r.ForEach, statusRefPrefix) {
				if err := b.validateForEachStatusRef(r); err != nil {
					return err
				}
			} else if _, ok := EnvRef(r.ForEach); ok {
				if err := b.validateForEachEnvRef(r); err != nil {
					return err
				}
			} else if err := validateForEachParamRef(b.Spec.XRD, r); err != nil {
				return err
			}
		}
		if r.When != "" {
			if err := checkScalar(fmt.Sprintf("spec.resources[%d].when", i), r.When); err != nil {
				return err
			}
			if head, _, _ := strings.Cut(r.When, " "); strings.HasPrefix(head, "params.") &&
				strings.Contains(strings.TrimPrefix(head, "params."), ".") {
				return fmt.Errorf("resource %q: when cannot reference an object member (got %q) — "+
					"conditions reference top-level parameters only in v1", r.Name, head)
			}
			source, name, op, literal, err := ParseWhen(r.When)
			if err != nil {
				return fmt.Errorf("resource %q: %w", r.Name, err)
			}
			if source == "env" {
				decl, exists := b.Spec.Environment[name]
				if !exists {
					return UnknownEnvKeyError(fmt.Sprintf("resource %q: when", r.Name), name, b.Spec.Environment)
				}
				switch op {
				case "":
					if decl.Type != "boolean" {
						return fmt.Errorf("resource %q: when environment key %q has type %q, want boolean -- "+
							"the bare form renders {{- if and (hasKey $env %q) $env.%s }}, a truthiness test; compare a string "+
							"key explicitly: when: env.%s == \"<literal>\"",
							r.Name, name, decl.Type, name, name, name)
					}
				default:
					if decl.Type != "string" {
						return fmt.Errorf("resource %q: when environment key %q has type %q, want string -- "+
							"the %s form compares against a string literal", r.Name, name, decl.Type, op)
					}
				}
			} else {
				decl, exists := b.Spec.XRD.Parameters[name]
				if !exists {
					return fmt.Errorf("resource %q: when references unknown parameter %q", r.Name, name)
				}
				if !decl.Required && decl.Default == "" {
					return fmt.Errorf("resource %q: when parameter %q must be required or carry a default -- "+
						"the condition dereferences it unguarded, and under options: [\"missingkey=error\"] "+
						"an absent key hard-fails the whole render; only the XRD's required gate or its "+
						"schema default makes the key's presence unconditional", r.Name, name)
				}
				switch op {
				case "":
					if decl.Type != "boolean" {
						return fmt.Errorf("resource %q: when parameter %q has type %q, want boolean -- "+
							"the bare form renders {{- if $spec.%s }}, a truthiness test; compare a string "+
							"parameter explicitly: when: params.%s == \"<literal>\"",
							r.Name, name, decl.Type, name, name)
					}
				default:
					if decl.Type != "string" {
						return fmt.Errorf("resource %q: when parameter %q has type %q, want string -- "+
							"the %s form compares against a string literal", r.Name, name, decl.Type, op)
					}
					if len(decl.Enum) > 0 && !slices.Contains(decl.Enum, literal) {
						return fmt.Errorf("resource %q: when literal %q is not among parameter %q's enum values %v -- "+
							"the XRD schema admits no XR carrying it, so the condition would be constant: "+
							"a resource that silently never (or always) exists", r.Name, literal, name, decl.Enum)
					}
				}
			}
		}

		if err := b.validateFields(r); err != nil {
			return err
		}

		if err := validateResourceEnvelope(b, r); err != nil {
			return err
		}
		if err := b.validateResourceAnnotations(r); err != nil {
			return err
		}
	}
	return nil
}

// validateFields validates all field entries on a resource.
func (b *Blueprint) validateFields(r Resource) error {
	x := b.Spec.XRD
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
			if whole, hasWhole := r.Fields[basePath]; hasWhole {
				isObjParam := false
				if whole.From != "" {
					if pref, _, _ := ParamRef(whole.From); pref != "" {
						if pdecl, ok := x.Parameters[pref]; ok && pdecl.Type == "object" {
							isObjParam = true
						}
					}
				}
				if !isObjParam {
					return fmt.Errorf("resource %q field %q conflicts with field %q, which sets the whole map; "+
						"set the whole map or set individual keys, not both", r.Name, p, basePath)
				}
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

		for _, src := range []struct{ label, val string }{
			{"from", f.From}, {"raw", f.Raw}, {"template", f.Template}, {"value", f.Value},
		} {
			if err := checkScalar(fmt.Sprintf("resource %q field %q: %s", r.Name, p, src.label), src.val); err != nil {
				return err
			}
		}

		if f.Raw != "" && b.Engine() != EngineGoTemplating && (strings.Contains(f.Raw, "{{") || IsBareGoTemplateExpr(f.Raw)) {
			return fmt.Errorf("resource %q field %q: raw %q contains Go-template syntax which is only supported with the go-templating engine (current engine is %q)",
				r.Name, p, f.Raw, b.Engine())
		}
		if f.Template != "" {
			if _, ok := b.Spec.Templates[f.Template]; !ok {
				return fmt.Errorf("resource %q field %q: references unknown template %q "+
					"(declare it under spec.templates)", r.Name, p, f.Template)
			}
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
				if ref.IsMetadataName() {
					if err := b.validateMetadataRef(r, fmt.Sprintf("field %q", p), f.From); err != nil {
						return err
					}
				} else {
					if err := b.validateStatusRef(r, fmt.Sprintf("field %q", p), f.From); err != nil {
						return err
					}
				}
				continue
			}
			if ref.Env != "" {
				_, exists := b.Spec.Environment[ref.Env]
				if !exists {
					return UnknownEnvKeyError(fmt.Sprintf("resource %q field %q", r.Name, p), ref.Env, b.Spec.Environment)
				}
				continue
			}
			decl, err := resolveParamRef(x, fmt.Sprintf("resource %q field %q", r.Name, p), ref.Param)
			if err != nil {
				return err
			}
			if decl.Type == "array" {
				param, _, _ := ParamRef(f.From)
				return fmt.Errorf("resource %q field %q: parameter %q has type %q, and a from: "+
					"mapping cannot render a composite value in M1 -- it emits a bare "+
					"{{ $spec.%s }}, which Go's template engine formats with fmt "+
					"(an object renders as \"map[k:v]\", an array as \"[a b c]\"). Both are valid "+
					"YAML and silently wrong. Use a scalar parameter, or set the field with raw:",
					r.Name, p, param, decl.Type, param)
			}
			if decl.Type == "object" {
				param, _, _ := ParamRef(f.From)
				if isMap {
					return fmt.Errorf("resource %q field %q: parameter %q has type %q, and a from: "+
						"mapping cannot render a composite value in M1 -- it emits a bare "+
						"{{ $spec.%s }}, which Go's template engine formats with fmt "+
						"(an object renders as \"map[k:v]\", an array as \"[a b c]\"). Both are valid "+
						"YAML and silently wrong. Use a scalar parameter, or set the field with raw:",
						r.Name, p, param, decl.Type, param)
				}
				hasChildMapKeys := false
				for otherPath := range r.Fields {
					if strings.HasPrefix(otherPath, p+"[") {
						hasChildMapKeys = true
						break
					}
				}
				isMapLeaf := hasChildMapKeys || p == "tags" || p == "labels" || p == "annotations"
				if len(decl.Properties) > 0 && !isMapLeaf {
					return fmt.Errorf("resource %q field %q: parameter %q is a typed object — a from: "+
						"mapping cannot render the whole object; wire one of its declared members "+
						"instead (params.%s.<member>; declared members: %s)",
						r.Name, p, param, param, strings.Join(sortedMemberNames(decl), ", "))
				}
				if len(decl.Properties) == 0 && !isMapLeaf {
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
