package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// EnvironmentConfig renders one EnvironmentConfig resource.
func EnvironmentConfig(b *blueprint.Blueprint, cfg blueprint.EnvironmentConfig) ([]byte, error) {
	d := NewDoc()
	header(d, blueprintSource(b))
	d.Line(0, "apiVersion: apiextensions.crossplane.io/v1beta1")
	d.Line(0, "kind: EnvironmentConfig")
	d.Line(0, "metadata:")
	name := cfg.Name
	if name == "" {
		name = "default"
	}
	d.Line(1, "name: %s", name)
	if cfg.Selector != nil && len(cfg.Selector.MatchLabels) > 0 {
		d.Line(1, "labels:")
		labelKeys := make([]string, 0, len(cfg.Selector.MatchLabels))
		for k := range cfg.Selector.MatchLabels {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)
		for _, k := range labelKeys {
			v := cfg.Selector.MatchLabels[k]
			d.Line(2, "%s: %s", k, formatLabelValue(v))
		}
	}
	d.Line(0, "data:")
	if len(b.Spec.Environment) > 0 {
		envKeys := make([]string, 0, len(b.Spec.Environment))
		for k := range b.Spec.Environment {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		data := cfg.EffectiveData()
		for _, k := range envKeys {
			envKey := b.Spec.Environment[k]
			if val, ok := data[k]; ok {
				d.Line(1, "%s: %s", k, formatEnvVal(envKey, val))
			} else if envKey.Default != "" {
				d.Line(1, "%s: %s", k, formatEnvDefault(envKey))
			} else {
				d.Line(1, "%s: %s", k, formatEnvVal(envKey, ""))
			}
		}
	}
	return d.Bytes(), nil
}

func formatLabelValue(v string) string {
	if v == "" {
		return `""`
	}
	if yamlKeywords[strings.ToLower(v)] || strings.ContainsAny(v, ":#\"'{}[]!*?") || strings.HasPrefix(v, "-") {
		return fmt.Sprintf("%q", v)
	}
	return v
}

func formatEnvVal(k blueprint.EnvironmentKey, val string) string {
	if val == "" {
		switch k.Type {
		case "integer":
			return "0"
		case "number":
			return "0.0"
		case "boolean":
			return "false"
		default:
			return `""`
		}
	}
	switch k.Type {
	case "integer", "number", "boolean":
		return val
	default:
		return fmt.Sprintf("%q", val)
	}
}
