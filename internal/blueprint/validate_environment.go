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
