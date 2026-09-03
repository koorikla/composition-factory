package blueprint

import (
	"fmt"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

var templateVarRE = regexp.MustCompile(`\$[a-zA-Z_]`)

var templateFuncs = []string{
	"printf", "quote", "squote", "b64enc", "b64dec", "hasKey", "default",
	"coalesce", "ternary", "lower", "upper", "trim", "indent", "nindent",
	"int", "float64", "bool", "list", "dict", "until", "cat", "replace",
	"regexMatch", "toJson", "toYaml", "fromYaml", "fromJson", "include",
	"setResourceNameAnnotation", "dig",
}

// IsBareGoTemplateExpr reports whether s is a bare Go-template expression
// (e.g. `printf "%s-subnet-%d" $xr $i` or `$spec.region`) without `{{ ... }}` delimiters.
func IsBareGoTemplateExpr(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "{{") {
		return false
	}
	if templateVarRE.MatchString(s) {
		return true
	}
	for _, fn := range templateFuncs {
		if strings.HasPrefix(s, fn+" ") || strings.HasPrefix(s, fn+"(") || s == fn {
			return true
		}
	}
	return false
}

// NormalizeRawGoTemplate auto-wraps bare Go-template expressions with `{{ ... }}`.
func NormalizeRawGoTemplate(raw string) string {
	rawTrim := strings.TrimSpace(raw)
	if IsBareGoTemplateExpr(rawTrim) {
		return "{{ " + rawTrim + " }}"
	}
	return raw
}

// ParseFlowStyleMap parses a flow-style map literal (e.g. `{app: web}` or `{"app": "web", "env": "prod"}`).
// It returns false if s is empty, contains Go template tags {{ ... }}, or is not a valid map of scalar values.
func ParseFlowStyleMap(s string) (map[string]string, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") || strings.Contains(trimmed, "{{") {
		return nil, false
	}
	var rawMap map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &rawMap); err != nil || len(rawMap) == 0 {
		return nil, false
	}
	result := make(map[string]string, len(rawMap))
	for k, v := range rawMap {
		switch val := v.(type) {
		case string:
			result[k] = val
		case int, int64, float64, bool:
			result[k] = fmt.Sprint(val)
		default:
			return nil, false
		}
	}
	return result, true
}
