package emit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/koorikla/compositionfactory/internal/schema"
	"gopkg.in/yaml.v3"
)

// ValidateRendered validates the rendered composed resources in a multi-document
// YAML stream against the provided CRD schemas.
//
// Every composed resource (identified by the composition-resource-name annotation
// or matching a cached CRD) is checked for:
//   - Matching CRD by apiVersion and kind
//   - spec.forProvider (for managed resources) or object root (for native kinds)
//     against the CRD OpenAPI schema
//   - Unknown field paths (with nearest-match suggestions)
//   - Field type mismatches (string, integer, number, boolean, array, object/map)
//
// Diagnostics include line numbers from the rendered YAML, the resource name,
// kind, and exact field path.
func ValidateRendered(renderedStream []byte, crds []schema.CRD) error {
	if len(bytes.TrimSpace(renderedStream)) == 0 {
		return nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(renderedStream))
	var errs []string

	for {
		var docNode yaml.Node
		err := decoder.Decode(&docNode)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("render output does not parse as YAML: %w", err)
		}
		if docNode.Kind == yaml.DocumentNode && len(docNode.Content) == 0 {
			continue
		}
		root := docNode.Content[0]
		if root.Kind != yaml.MappingNode {
			continue
		}

		docErrs := validateRenderedDoc(root, crds)
		errs = append(errs, docErrs...)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

func validateRenderedDoc(root *yaml.Node, crds []schema.CRD) []string {
	var apiVersionNode, kindNode, metadataNode, specNode *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		k := root.Content[i].Value
		v := root.Content[i+1]
		switch k {
		case "apiVersion":
			apiVersionNode = v
		case "kind":
			kindNode = v
		case "metadata":
			metadataNode = v
		case "spec":
			specNode = v
		}
	}

	if apiVersionNode == nil || kindNode == nil {
		return nil
	}

	apiVersion := apiVersionNode.Value
	kind := kindNode.Value

	// Ignore internal pipeline result / context documents
	if strings.HasPrefix(apiVersion, "render.crossplane.io/") {
		return nil
	}

	resourceName := ""
	hasCompAnnotation := false
	if metadataNode != nil && metadataNode.Kind == yaml.MappingNode {
		for i := 0; i < len(metadataNode.Content); i += 2 {
			if metadataNode.Content[i].Value == "annotations" {
				annNode := metadataNode.Content[i+1]
				if annNode.Kind == yaml.MappingNode {
					for j := 0; j < len(annNode.Content); j += 2 {
						if annNode.Content[j].Value == "crossplane.io/composition-resource-name" {
							resourceName = annNode.Content[j+1].Value
							hasCompAnnotation = true
							break
						}
					}
				}
			}
			if resourceName == "" && metadataNode.Content[i].Value == "name" {
				resourceName = metadataNode.Content[i+1].Value
			}
		}
	}

	crd, ver, found := findCRDForRendered(crds, apiVersion, kind)
	if !found {
		if hasCompAnnotation {
			return []string{fmt.Sprintf("line %d: resource %q (%s): no matching CRD schema found for apiVersion %q and kind %q",
				kindNode.Line, resourceName, kind, apiVersion, kind)}
		}
		// If it's an unannotated doc (such as the composite XR), skip if not matching known CRD
		return nil
	}

	if resourceName == "" {
		resourceName = kind
	}

	var errs []string

	if crd.Native {
		where := "the native " + kind + " schema"
		for i := 0; i < len(root.Content); i += 2 {
			kNode := root.Content[i]
			vNode := root.Content[i+1]
			kName := kNode.Value

			switch kName {
			case "apiVersion", "kind", "status":
				continue
			case "metadata":
				errs = append(errs, validateObjectMeta(vNode, resourceName, kind)...)
			default:
				propSchema, ok := ver.Properties[kName].(map[string]any)
				if !ok {
					candidates := make([]string, 0, len(ver.Properties))
					for k := range ver.Properties {
						if k != "apiVersion" && k != "kind" && k != "status" {
							candidates = append(candidates, k)
						}
					}
					sort.Strings(candidates)
					s := closestPath(kName, candidates)
					if s != "" {
						errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s; did you mean %q?",
							kNode.Line, resourceName, kind, kName, where, s))
					} else {
						errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s",
							kNode.Line, resourceName, kind, kName, where))
					}
					continue
				}
				errs = append(errs, validateSchemaNode(vNode, propSchema, kName, resourceName, kind, where)...)
			}
		}
	} else {
		// Managed Resource
		where := kind + " spec.forProvider"
		specProp, _ := ver.Properties["spec"].(map[string]any)
		specInner, _ := specProp["properties"].(map[string]any)

		if specNode != nil {
			if specNode.Kind != yaml.MappingNode {
				errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): spec must be an object, got %s",
					specNode.Line, resourceName, kind, nodeTypeDescription(specNode)))
			} else {
				for i := 0; i < len(specNode.Content); i += 2 {
					kNode := specNode.Content[i]
					vNode := specNode.Content[i+1]
					kName := kNode.Value

					if kName == "forProvider" {
						fpSchema, _ := specInner["forProvider"].(map[string]any)
						if fpSchema == nil {
							errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): kind %q has no spec.forProvider properties in its CRD",
								kNode.Line, resourceName, kind, kind))
							continue
						}
						errs = append(errs, validateSchemaNode(vNode, fpSchema, "spec.forProvider", resourceName, kind, where)...)
						continue
					}

					// Validate other spec envelope fields (providerConfigRef, deletionPolicy, initProvider, etc.)
					if childSchema, ok := specInner[kName].(map[string]any); ok {
						envWhere := kind + " spec." + kName
						errs = append(errs, validateSchemaNode(vNode, childSchema, "spec."+kName, resourceName, kind, envWhere)...)
					} else if specInner != nil {
						candidates := make([]string, 0, len(specInner))
						for k := range specInner {
							candidates = append(candidates, k)
						}
						sort.Strings(candidates)
						s := closestPath(kName, candidates)
						if s != "" {
							errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s spec; did you mean %q?",
								kNode.Line, resourceName, kind, "spec."+kName, kind, s))
						} else {
							errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s spec",
								kNode.Line, resourceName, kind, "spec."+kName, kind))
						}
					}
				}
			}
		}
	}

	return errs
}

func findCRDForRendered(crds []schema.CRD, apiVersion, kind string) (schema.CRD, schema.Version, bool) {
	group, version := "", apiVersion
	if i := strings.Index(apiVersion, "/"); i != -1 {
		group = apiVersion[:i]
		version = apiVersion[i+1:]
	}

	for _, c := range crds {
		if c.Kind == kind && c.Group == group {
			for _, v := range c.Versions {
				if v.Name == version {
					return c, v, true
				}
			}
			if pref, err := c.Preferred(); err == nil {
				return c, pref, true
			}
		}
	}

	// Fallback for native kinds where group might be empty or match core
	if group == "" || group == "core" {
		for _, c := range crds {
			if c.Native && c.Kind == kind {
				if pref, err := c.Preferred(); err == nil {
					return c, pref, true
				}
			}
		}
	}

	return schema.CRD{}, schema.Version{}, false
}

func validateObjectMeta(metaNode *yaml.Node, resourceName, kind string) []string {
	if metaNode.Kind != yaml.MappingNode {
		return []string{fmt.Sprintf("line %d: resource %q (%s): metadata must be an object, got %s",
			metaNode.Line, resourceName, kind, nodeTypeDescription(metaNode))}
	}
	var errs []string
	for i := 0; i < len(metaNode.Content); i += 2 {
		kNode := metaNode.Content[i]
		vNode := metaNode.Content[i+1]
		switch kNode.Value {
		case "labels", "annotations":
			if vNode.Kind != yaml.MappingNode {
				errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): metadata.%s must be an object (map of strings), got %s",
					vNode.Line, resourceName, kind, kNode.Value, nodeTypeDescription(vNode)))
			} else {
				for j := 0; j < len(vNode.Content); j += 2 {
					val := vNode.Content[j+1]
					if val.Kind != yaml.ScalarNode || (val.Tag != "!!str" && val.Tag != "") {
						errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): metadata.%s[%q] must be a string, got %s",
							val.Line, resourceName, kind, kNode.Value, vNode.Content[j].Value, nodeTypeDescription(val)))
					}
				}
			}
		case "name", "namespace", "generateName", "uid", "resourceVersion":
			if vNode.Kind != yaml.ScalarNode || (vNode.Tag != "!!str" && vNode.Tag != "") {
				errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): metadata.%s must be a string, got %s",
					vNode.Line, resourceName, kind, kNode.Value, nodeTypeDescription(vNode)))
			}
		}
	}
	return errs
}

func validateSchemaNode(valNode *yaml.Node, propSchema map[string]any, path string, resourceName, kind, where string) []string {
	if propSchema == nil {
		return nil
	}

	// Null value handling
	if valNode.Kind == yaml.ScalarNode && (valNode.Tag == "!!null" || valNode.Value == "null") {
		return nil
	}

	// int-or-string special handling
	if isIntOrString(propSchema) {
		if valNode.Kind != yaml.ScalarNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string or integer, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		if valNode.Tag != "!!str" && valNode.Tag != "!!int" && valNode.Tag != "" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string or integer, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		return nil
	}

	schemaType, _ := propSchema["type"].(string)
	if schemaType == "" {
		if propSchema["properties"] != nil {
			schemaType = "object"
		} else if propSchema["items"] != nil {
			schemaType = "array"
		} else if propSchema["additionalProperties"] != nil {
			schemaType = "map"
		}
	}

	var errs []string

	switch schemaType {
	case "string":
		if valNode.Kind != yaml.ScalarNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		if valNode.Tag == "!!int" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string, got integer %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!float" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string, got number %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!bool" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected string, got boolean %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if enumRaw, ok := propSchema["enum"].([]any); ok && len(enumRaw) > 0 {
			matched := false
			var allowed []string
			for _, e := range enumRaw {
				s := fmt.Sprint(e)
				allowed = append(allowed, fmt.Sprintf("%q", s))
				if s == valNode.Value {
					matched = true
				}
			}
			if !matched {
				errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q: invalid value %q: supported values: %s",
					valNode.Line, resourceName, kind, path, valNode.Value, strings.Join(allowed, ", ")))
			}
		}

	case "integer":
		if valNode.Kind != yaml.ScalarNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected integer, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		if valNode.Tag == "!!str" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected integer, got string %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!bool" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected integer, got boolean %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!float" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected integer, got number %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "" && !isIntegerStr(valNode.Value) {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected integer, got %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}

	case "number":
		if valNode.Kind != yaml.ScalarNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected number, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		if valNode.Tag == "!!str" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected number, got string %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!bool" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected number, got boolean %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "" && !isNumberStr(valNode.Value) {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected number, got %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}

	case "boolean":
		if valNode.Kind != yaml.ScalarNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected boolean, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		if valNode.Tag == "!!str" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected boolean, got string %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "!!int" || valNode.Tag == "!!float" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected boolean, got number %s",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}
		if valNode.Tag == "" && valNode.Value != "true" && valNode.Value != "false" {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected boolean, got %q",
				valNode.Line, resourceName, kind, path, valNode.Value)}
		}

	case "array":
		if valNode.Kind != yaml.SequenceNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected array, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}
		itemsSchema, _ := propSchema["items"].(map[string]any)
		if itemsSchema != nil {
			for idx, elemNode := range valNode.Content {
				elemPath := fmt.Sprintf("%s[%d]", path, idx)
				errs = append(errs, validateSchemaNode(elemNode, itemsSchema, elemPath, resourceName, kind, where)...)
			}
		}

	case "object", "map":
		if valNode.Kind != yaml.MappingNode {
			return []string{fmt.Sprintf("line %d: resource %q (%s): field %q: invalid type: expected object, got %s",
				valNode.Line, resourceName, kind, path, nodeTypeDescription(valNode))}
		}

		if preserve, _ := propSchema["x-kubernetes-preserve-unknown-fields"].(bool); preserve {
			return nil
		}

		if addProps, ok := propSchema["additionalProperties"]; ok && addProps != false && addProps != nil {
			if addPropsMap, isMap := addProps.(map[string]any); isMap {
				for i := 0; i < len(valNode.Content); i += 2 {
					kNode := valNode.Content[i]
					vNode := valNode.Content[i+1]
					childPath := fmt.Sprintf("%s[%s]", path, kNode.Value)
					errs = append(errs, validateSchemaNode(vNode, addPropsMap, childPath, resourceName, kind, where)...)
				}
			}
			return errs
		}

		properties, _ := propSchema["properties"].(map[string]any)
		knownKeys := make([]string, 0, len(properties))
		for k := range properties {
			knownKeys = append(knownKeys, k)
		}
		sort.Strings(knownKeys)

		if reqRaw, ok := propSchema["required"].([]any); ok {
			for _, rItem := range reqRaw {
				reqName, _ := rItem.(string)
				if reqName == "" {
					continue
				}
				found := false
				for i := 0; i < len(valNode.Content); i += 2 {
					if valNode.Content[i].Value == reqName {
						found = true
						break
					}
				}
				if !found {
					reqPath := reqName
					if path != "" {
						reqPath = path + "." + reqName
					}
					errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): missing required field %q in %s",
						valNode.Line, resourceName, kind, reqPath, where))
				}
			}
		}

		for i := 0; i < len(valNode.Content); i += 2 {
			kNode := valNode.Content[i]
			vNode := valNode.Content[i+1]
			kName := kNode.Value
			childPath := kName
			if path != "" {
				childPath = path + "." + kName
			}

			if childSchema, ok := properties[kName].(map[string]any); ok {
				errs = append(errs, validateSchemaNode(vNode, childSchema, childPath, resourceName, kind, where)...)
			} else if properties != nil {
				s := closestPath(kName, knownKeys)
				if s != "" {
					errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s; did you mean %q?",
						kNode.Line, resourceName, kind, childPath, where, s))
				} else {
					errs = append(errs, fmt.Sprintf("line %d: resource %q (%s): field %q is not in %s",
						kNode.Line, resourceName, kind, childPath, where))
				}
			}
		}
	}

	return errs
}

func isIntOrString(schema map[string]any) bool {
	if intOrStr, _ := schema["x-kubernetes-int-or-string"].(bool); intOrStr {
		return true
	}
	if oneOf, ok := schema["oneOf"].([]any); ok {
		hasStr, hasInt := false, false
		for _, o := range oneOf {
			if om, ok := o.(map[string]any); ok {
				if om["type"] == "string" {
					hasStr = true
				}
				if om["type"] == "integer" {
					hasInt = true
				}
			}
		}
		if hasStr && hasInt {
			return true
		}
	}
	return false
}

func nodeTypeDescription(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "array"
	case yaml.MappingNode:
		return "object"
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!int":
			return "integer"
		case "!!float":
			return "number"
		case "!!bool":
			return "boolean"
		case "!!null":
			return "null"
		case "!!str":
			return "string"
		default:
			if n.Value == "true" || n.Value == "false" {
				return "boolean"
			}
			if isIntegerStr(n.Value) {
				return "integer"
			}
			if isNumberStr(n.Value) {
				return "number"
			}
			return "string"
		}
	default:
		return "unknown"
	}
}

var intRE = regexp.MustCompile(`^-?\d+$`)

func isIntegerStr(s string) bool {
	return intRE.MatchString(s)
}

func isNumberStr(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
