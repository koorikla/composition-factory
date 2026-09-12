package emit

import (
	"fmt"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

// SampleXR synthesizes a sample Composite Resource from the blueprint's XRD
// declaration for render-time validation and testing.
func SampleXR(b *blueprint.Blueprint) ([]byte, error) {
	spec := map[string]any{}
	for name, p := range b.Spec.XRD.Parameters {
		if !hasRequiredOrDefault(p) && !isForEachParam(b, name) {
			continue
		}
		val := placeholderValue(p)
		if obj, ok := val.(map[string]any); ok && !p.Required && len(obj) == 0 && !isForEachParam(b, name) {
			continue
		}
		spec[name] = val
	}
	metadata := map[string]any{"name": "render-check"}
	if b.Spec.XRD.Scope == "Namespaced" {
		metadata["namespace"] = "default"
	}
	// sigs.k8s.io/yaml sorts map keys, so this marshal is deterministic.
	return yaml.Marshal(map[string]any{
		"apiVersion": b.Spec.XRD.Group + "/" + b.Spec.XRD.Version,
		"kind":       b.Spec.XRD.Kind,
		"metadata":   metadata,
		"spec":       spec,
	})
}

func hasRequiredOrDefault(p blueprint.Parameter) bool {
	if p.Required || p.Default != "" {
		return true
	}
	if p.Type == "object" {
		for _, member := range p.Properties {
			if hasRequiredOrDefault(member) {
				return true
			}
		}
	}
	return false
}

func isForEachParam(b *blueprint.Blueprint, paramName string) bool {
	target := "params." + paramName
	for _, r := range b.Spec.Resources {
		if r.ForEach == target {
			return true
		}
	}
	return false
}

func placeholderValue(p blueprint.Parameter) any {
	if p.Default != "" {
		if v := parseParamScalar(p.Default, p.Type); v != nil {
			return v
		}
	}
	if len(p.Enum) > 0 {
		if v := parseParamScalar(p.Enum[0], p.Type); v != nil {
			return v
		}
		return p.Enum[0]
	}
	switch p.Type {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "object":
		obj := map[string]any{}
		for name, member := range p.Properties {
			if hasRequiredOrDefault(member) {
				obj[name] = placeholderValue(member)
			}
		}
		return obj
	default: // string
		return "sample"
	}
}

func envPlaceholderValue(k blueprint.EnvironmentKey) any {
	if k.Default != "" {
		if v := parseParamScalar(k.Default, k.Type); v != nil {
			return v
		}
	}
	switch k.Type {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	default:
		return "sample"
	}
}

func parseParamScalar(val, paramType string) any {
	switch paramType {
	case "integer":
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	case "number":
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			return f
		}
	case "boolean":
		if val == "true" {
			return true
		} else if val == "false" {
			return false
		}
	case "string":
		return val
	}
	return nil
}
