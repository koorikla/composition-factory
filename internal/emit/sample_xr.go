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
		if !p.Required && !isForEachParam(b, name) {
			continue
		}
		spec[name] = placeholderValue(p)
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
		switch p.Type {
		case "integer":
			var n int
			if _, err := fmt.Sscanf(p.Default, "%d", &n); err == nil {
				return n
			}
		case "number":
			var f float64
			if _, err := fmt.Sscanf(p.Default, "%f", &f); err == nil {
				return f
			}
		case "boolean":
			if p.Default == "true" {
				return true
			} else if p.Default == "false" {
				return false
			}
		case "string":
			return p.Default
		}
	}
	if len(p.Enum) > 0 {
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
			if member.Required || member.Default != "" {
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
		switch k.Type {
		case "integer":
			var n int
			if _, err := fmt.Sscanf(k.Default, "%d", &n); err == nil {
				return n
			}
		case "number":
			var f float64
			if _, err := fmt.Sscanf(k.Default, "%f", &f); err == nil {
				return f
			}
		case "boolean":
			if k.Default == "true" {
				return true
			} else if k.Default == "false" {
				return false
			}
		case "string":
			return k.Default
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
