package blueprint

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// validTypes are the parameter types M1 accepts.
var validTypes = map[string]bool{
	"string": true, "integer": true, "number": true,
	"boolean": true, "object": true, "array": true,
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
	if x.Group == "" || x.Kind == "" || x.Plural == "" || x.Version == "" {
		return fmt.Errorf("spec.xrd needs group, kind, plural and version")
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
		if t := x.Parameters[n].Type; !validTypes[t] {
			return fmt.Errorf("spec.xrd.parameters.%s: unknown type %q", n, t)
		}
	}

	for _, r := range b.Spec.Resources {
		if r.Name == "" || r.Kind == "" {
			return fmt.Errorf("every resource needs a name and a kind")
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
