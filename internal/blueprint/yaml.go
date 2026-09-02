package blueprint

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// yamlToJSON converts YAML bytes to JSON bytes using YAML 1.2 semantics (via yaml.v3),
// preserving keyword-like parameter names like "n", "y", "yes", "no" as strings rather
// than coercing them into boolean types.
func yamlToJSON(body []byte) ([]byte, error) {
	var raw any
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	clean := cleanYAMLValue(raw)
	return json.Marshal(clean)
}

func cleanYAMLValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			res[k] = cleanYAMLValue(item)
		}
		return res
	case map[any]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			res[fmt.Sprint(k)] = cleanYAMLValue(item)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = cleanYAMLValue(item)
		}
		return res
	default:
		return val
	}
}
