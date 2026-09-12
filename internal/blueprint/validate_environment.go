package blueprint

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// validEnvTypes are the types an environment key may declare: scalars only.
var validEnvTypes = map[string]bool{
	"string":  true,
	"integer": true,
	"number":  true,
	"boolean": true,
}

// validateEnvironment validates spec.environment declarations.
func (b *Blueprint) validateEnvironment() error {
	if len(b.Spec.Environment) == 0 {
		return nil
	}
	names := make([]string, 0, len(b.Spec.Environment))
	for n := range b.Spec.Environment {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		if !paramNameRE.MatchString(n) || yamlParamKeywords[strings.ToLower(n)] {
			return fmt.Errorf("spec.environment.%s: invalid environment key name "+
				"(must be camelCase, e.g. vpcId, and not a YAML keyword like yes/no/true/false)", n)
		}
		k := b.Spec.Environment[n]
		if !validEnvTypes[k.Type] {
			if k.Type == "" {
				return fmt.Errorf("spec.environment.%s: type is required (must be string, integer, number, or boolean)", n)
			}
			return fmt.Errorf("spec.environment.%s: unknown type %q (must be string, integer, number, or boolean)", n, k.Type)
		}
		if err := checkScalar(fmt.Sprintf("spec.environment.%s.description", n), k.Description); err != nil {
			return err
		}
		if k.Default != "" {
			if err := checkScalar(fmt.Sprintf("spec.environment.%s.default", n), k.Default); err != nil {
				return err
			}
			switch k.Type {
			case "boolean":
				switch strings.ToLower(k.Default) {
				case "true", "false":
				default:
					return fmt.Errorf("spec.environment.%s: default %q is not a valid boolean (use true or false)", n, k.Default)
				}
			case "integer":
				if _, err := strconv.ParseInt(k.Default, 10, 64); err != nil {
					return fmt.Errorf("spec.environment.%s: default %q is not a valid integer: %w", n, k.Default, err)
				}
			case "number":
				val, err := strconv.ParseFloat(k.Default, 64)
				if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
					return fmt.Errorf("spec.environment.%s: default %q is not a valid number (NaN and Inf are refused)", n, k.Default)
				}
			}
		}
	}
	return nil
}

// validateEnvironmentConfigs validates spec.environmentConfigs declarations.
func (b *Blueprint) validateEnvironmentConfigs() error {
	if len(b.Spec.EnvironmentConfigs) == 0 {
		return nil
	}
	if len(b.Spec.Environment) == 0 {
		return fmt.Errorf("spec.environmentConfigs declared without spec.environment keys: declare environment keys first")
	}
	effConfigs := b.EffectiveEnvironmentConfigs()
	seenNames := make(map[string]int, len(effConfigs))
	for i, cfg := range b.Spec.EnvironmentConfigs {
		if cfg.Name != "" {
			if err := checkScalar(fmt.Sprintf("spec.environmentConfigs[%d].name", i), cfg.Name); err != nil {
				return err
			}
			if !resourceNameRE.MatchString(cfg.Name) || yamlKeywords[strings.ToLower(cfg.Name)] {
				return fmt.Errorf("spec.environmentConfigs[%d].name: %q is not a valid config name (must be a DNS label, e.g. dev-env, and not a YAML keyword like yes/no/on/off)", i, cfg.Name)
			}
		}
		if cfg.Selector != nil {
			if len(cfg.Selector.MatchLabels) == 0 {
				return fmt.Errorf("spec.environmentConfigs[%d]: selector declared with empty matchLabels", i)
			}
			keys := make([]string, 0, len(cfg.Selector.MatchLabels))
			for k := range cfg.Selector.MatchLabels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := cfg.Selector.MatchLabels[k]
				if err := checkScalar(fmt.Sprintf("spec.environmentConfigs[%d].selector.matchLabels key %q", i, k), k); err != nil {
					return err
				}
				if err := checkScalar(fmt.Sprintf("spec.environmentConfigs[%d].selector.matchLabels[%s]", i, k), v); err != nil {
					return err
				}
			}
		}
		effName := effConfigs[i].Name
		if prev, ok := seenNames[effName]; ok {
			return fmt.Errorf("spec.environmentConfigs[%d]: duplicate config name %q (previously defined at index %d)", i, effName, prev)
		}
		seenNames[effName] = i

		data := cfg.EffectiveData()
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := data[k]
			envKey, ok := b.Spec.Environment[k]
			if !ok {
				return fmt.Errorf("spec.environmentConfigs[%d]: unknown environment key %q", i, k)
			}
			if val != "" {
				switch envKey.Type {
				case "boolean":
					switch strings.ToLower(val) {
					case "true", "false":
					default:
						return fmt.Errorf("spec.environmentConfigs[%d].data.%s: value %q is not a valid boolean (use true or false)", i, k, val)
					}
				case "integer":
					if _, err := strconv.ParseInt(val, 10, 64); err != nil {
						return fmt.Errorf("spec.environmentConfigs[%d].data.%s: value %q is not a valid integer: %w", i, k, val, err)
					}
				case "number":
					num, err := strconv.ParseFloat(val, 64)
					if err != nil || math.IsNaN(num) || math.IsInf(num, 0) {
						return fmt.Errorf("spec.environmentConfigs[%d].data.%s: value %q is not a valid number", i, k, val)
					}
				}
			}
		}
	}
	return nil
}
