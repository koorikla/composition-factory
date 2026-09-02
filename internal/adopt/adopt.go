// Package adopt ingests existing Crossplane Composition (and optional XRD)
// YAML manifests into a structured, round-trippable Blueprint document.
package adopt

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// Options configures the adoption parser.
type Options struct {
	// DefaultProviderRef is used when resource provider sources cannot be
	// automatically inferred from the CRD group.
	DefaultProviderRef string
}

// Adopt parses Crossplane Composition (and optional XRD) YAML documents and
// produces a valid Blueprint.
func Adopt(manifest []byte, opts Options) (*blueprint.Blueprint, error) {
	docs, err := splitYAML(manifest)
	if err != nil {
		return nil, fmt.Errorf("split manifest yaml: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("manifest contains no YAML documents")
	}

	var compDoc map[string]any
	var xrdDoc map[string]any

	for _, d := range docs {
		kind, _ := d["kind"].(string)
		switch kind {
		case "Composition":
			compDoc = d
		case "CompositeResourceDefinition", "CustomResourceDefinition":
			xrdDoc = d
		}
	}

	if compDoc == nil {
		return nil, fmt.Errorf("no Composition document found in manifest")
	}

	bp := &blueprint.Blueprint{
		APIVersion: "compositionfactory.koorikla.io/v1alpha1",
		Kind:       "Blueprint",
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{
				Parameters: make(map[string]blueprint.Parameter),
			},
			Templates: make(map[string]string),
			Resources: []blueprint.Resource{},
		},
	}

	// 1. Metadata
	if meta, ok := compDoc["metadata"].(map[string]any); ok {
		if name, ok := meta["name"].(string); ok {
			bp.Metadata.Name = name
		}
	}
	if bp.Metadata.Name == "" {
		bp.Metadata.Name = "adopted-composition"
	}

	// 2. XRD compositeTypeRef
	spec, _ := compDoc["spec"].(map[string]any)
	if spec == nil {
		return nil, fmt.Errorf("composition missing spec section")
	}

	if ctr, ok := spec["compositeTypeRef"].(map[string]any); ok {
		if k, ok := ctr["kind"].(string); ok {
			bp.Spec.XRD.Kind = k
		}
		if av, ok := ctr["apiVersion"].(string); ok {
			parts := strings.Split(av, "/")
			if len(parts) == 2 {
				bp.Spec.XRD.Group = parts[0]
				bp.Spec.XRD.Version = parts[1]
			} else {
				bp.Spec.XRD.Version = av
			}
		}
	}

	// 3. If XRD document is present, parse parameters & metadata
	if xrdDoc != nil {
		parseXRDDoc(xrdDoc, bp)
	}

	if bp.Spec.XRD.Kind == "" {
		bp.Spec.XRD.Kind = "XComposite"
	}
	if bp.Spec.XRD.Group == "" {
		bp.Spec.XRD.Group = "example.org"
	}
	if bp.Spec.XRD.Version == "" {
		bp.Spec.XRD.Version = "v1alpha1"
	}
	if bp.Spec.XRD.Plural == "" {
		bp.Spec.XRD.Plural = strings.ToLower(bp.Spec.XRD.Kind) + "s"
	}
	if bp.Spec.XRD.Scope == "" {
		bp.Spec.XRD.Scope = "Namespaced"
	}
	if bp.Spec.XRD.Scope == "Namespaced" {
		if _, ok := bp.Spec.XRD.Parameters["providerName"]; !ok {
			bp.Spec.XRD.Parameters["providerName"] = blueprint.Parameter{
				Type:        "string",
				Required:    true,
				Description: "Crossplane ProviderConfig name to use for managed resources",
			}
		}
	}

	// 4. Parse Pipeline or Classic Resources
	if pipeline, ok := spec["pipeline"].([]any); ok && len(pipeline) > 0 {
		if err := parsePipelineComposition(pipeline, bp, opts.DefaultProviderRef); err != nil {
			return nil, err
		}
	} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
		if err := parseClassicComposition(resources, bp, opts.DefaultProviderRef); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("composition has neither spec.pipeline nor spec.resources")
	}

	// 5. Deduplicate and collect provider sources
	collectSources(bp, opts.DefaultProviderRef)

	// Sort resources by name for deterministic ordering
	sort.Slice(bp.Spec.Resources, func(i, j int) bool {
		return bp.Spec.Resources[i].Name < bp.Spec.Resources[j].Name
	})

	if err := bp.Validate(); err != nil {
		return nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return bp, nil
}

// splitYAML splits a multi-document YAML stream into individual maps.
func splitYAML(data []byte) ([]map[string]any, error) {
	rawDocs := blueprint.SplitDocs(data)
	var docs []map[string]any
	for _, chunk := range rawDocs {
		var doc map[string]any
		if err := yaml.Unmarshal(chunk, &doc); err != nil {
			return nil, fmt.Errorf("unmarshal document: %w", err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func parseXRDDoc(xrdDoc map[string]any, bp *blueprint.Blueprint) {
	spec, ok := xrdDoc["spec"].(map[string]any)
	if !ok {
		return
	}
	if group, ok := spec["group"].(string); ok && bp.Spec.XRD.Group == "" {
		bp.Spec.XRD.Group = group
	}
	if names, ok := spec["names"].(map[string]any); ok {
		if k, ok := names["kind"].(string); ok && bp.Spec.XRD.Kind == "" {
			bp.Spec.XRD.Kind = k
		}
		if p, ok := names["plural"].(string); ok && bp.Spec.XRD.Plural == "" {
			bp.Spec.XRD.Plural = p
		}
	}
	if versions, ok := spec["versions"].([]any); ok && len(versions) > 0 {
		if v0, ok := versions[0].(map[string]any); ok {
			if vName, ok := v0["name"].(string); ok && bp.Spec.XRD.Version == "" {
				bp.Spec.XRD.Version = vName
			}
			if schema, ok := v0["schema"].(map[string]any); ok {
				if openAPI, ok := schema["openAPIV3Schema"].(map[string]any); ok {
					parseOpenAPISpec(openAPI, bp)
				}
			}
		}
	}
}

func parseOpenAPISpec(openAPI map[string]any, bp *blueprint.Blueprint) {
	props, ok := openAPI["properties"].(map[string]any)
	if !ok {
		return
	}
	specProp, ok := props["spec"].(map[string]any)
	if !ok {
		return
	}
	specSubProps, ok := specProp["properties"].(map[string]any)
	if !ok {
		return
	}
	paramsProp, ok := specSubProps["parameters"].(map[string]any)
	if !ok {
		paramsProp = specSubProps
	}

	paramMap, ok := paramsProp["properties"].(map[string]any)
	if !ok {
		return
	}
	reqList, _ := paramsProp["required"].([]any)
	reqSet := make(map[string]bool)
	for _, r := range reqList {
		if s, ok := r.(string); ok {
			reqSet[s] = true
		}
	}

	for pName, pVal := range paramMap {
		pObj, ok := pVal.(map[string]any)
		if !ok {
			continue
		}
		pType, _ := pObj["type"].(string)
		if pType == "" {
			pType = "string"
		}
		pDesc, _ := pObj["description"].(string)

		var pEnum []string
		if enumRaw, ok := pObj["enum"].([]any); ok {
			for _, e := range enumRaw {
				pEnum = append(pEnum, fmt.Sprint(e))
			}
		}

		var defStr string
		if defVal, ok := pObj["default"]; ok {
			defStr = fmt.Sprint(defVal)
		}

		bp.Spec.XRD.Parameters[pName] = blueprint.Parameter{
			Type:        pType,
			Required:    reqSet[pName],
			Description: pDesc,
			Default:     defStr,
			Enum:        pEnum,
		}
	}
}

var (
	reDefine         = regexp.MustCompile(`(?s)\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}(.*?)\{\{-?\s*end\s*-?\}\}`)
	reParamVar       = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reObservedStatus = regexp.MustCompile(`\{\{-?\s*\(index\s+\$observed\s+"([^"]+)"\)\.resource\.status\.atProvider\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reMustacheExpr   = regexp.MustCompile(`\{\{.*?\}\}`)
)

func parsePipelineComposition(pipeline []any, bp *blueprint.Blueprint, defaultProvider string) error {
	var otherSteps []blueprint.PipelineStep

	for _, stepRaw := range pipeline {
		step, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		stepName, _ := step["step"].(string)
		if stepName == "" {
			stepName, _ = step["name"].(string)
		}
		fnRef, _ := step["functionRef"].(map[string]any)
		fnName, _ := fnRef["name"].(string)
		if fnName == "" {
			fnName, _ = step["function"].(string)
		}

		if fnName == "function-go-templating" || strings.Contains(fnName, "gotemplating") {
			input, _ := step["input"].(map[string]any)
			inline, _ := input["inline"].(map[string]any)
			tmpl, _ := inline["template"].(string)
			if tmpl != "" {
				if err := parseGoTemplateBody(tmpl, bp, defaultProvider); err != nil {
					return fmt.Errorf("parse go template: %w", err)
				}
			}
		} else {
			var pkg string
			if input, ok := step["input"].(map[string]any); ok {
				if p, ok := input["package"].(string); ok {
					pkg = p
				}
			}
			if pkg == "" {
				if fnName == "function-auto-ready" {
					pkg = "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
				} else {
					pkg = "xpkg.crossplane.io/crossplane-contrib/" + fnName + ":v0.1.0"
				}
			}
			otherSteps = append(otherSteps, blueprint.PipelineStep{
				Name:        stepName,
				FunctionRef: fnName,
				Package:     pkg,
			})
		}
	}

	bp.Spec.Pipeline = otherSteps
	return nil
}

func parseGoTemplateBody(tmpl string, bp *blueprint.Blueprint, defaultProvider string) error {
	// 1. Extract defines
	defines := reDefine.FindAllStringSubmatch(tmpl, -1)
	for _, m := range defines {
		if len(m) >= 3 {
			defName := m[1]
			defBody := strings.TrimSpace(m[2])
			bp.Spec.Templates[defName] = defBody
		}
	}
	cleanTmpl := reDefine.ReplaceAllString(tmpl, "")

	// 2. Discover parameter references
	paramMatches := reParamVar.FindAllStringSubmatch(cleanTmpl, -1)
	for _, m := range paramMatches {
		if len(m) >= 2 {
			pName := m[1]
			if _, exists := bp.Spec.XRD.Parameters[pName]; !exists {
				bp.Spec.XRD.Parameters[pName] = blueprint.Parameter{
					Type:     "string",
					Required: false,
				}
			}
		}
	}

	// 3. Mask template expressions to make YAML strictly parseable
	var placeholderTable []string
	maskedTmpl := reMustacheExpr.ReplaceAllStringFunc(cleanTmpl, func(match string) string {
		idx := len(placeholderTable)
		placeholderTable = append(placeholderTable, match)
		return fmt.Sprintf(`"__CF_EXPR_%d__"`, idx)
	})

	// 4. Parse YAML documents embedded in masked template
	docs, err := splitYAML([]byte(maskedTmpl))
	if err != nil {
		return fmt.Errorf("split masked template yaml: %w", err)
	}
	for _, doc := range docs {
		res, err := resourceFromMap(doc, defaultProvider, placeholderTable)
		if err != nil || res == nil {
			continue
		}
		bp.Spec.Resources = append(bp.Spec.Resources, *res)
	}

	return nil
}

func parseClassicComposition(resources []any, bp *blueprint.Blueprint, defaultProvider string) error {
	for _, resRaw := range resources {
		resMap, ok := resRaw.(map[string]any)
		if !ok {
			continue
		}
		resName, _ := resMap["name"].(string)
		base, _ := resMap["base"].(map[string]any)
		if base == nil {
			continue
		}

		res, err := resourceFromMap(base, defaultProvider, nil)
		if err != nil || res == nil {
			continue
		}
		if resName != "" {
			res.Name = resName
		}

		// Apply patches
		if patches, ok := resMap["patches"].([]any); ok {
			for _, pRaw := range patches {
				pMap, ok := pRaw.(map[string]any)
				if !ok {
					continue
				}
				pType, _ := pMap["type"].(string)
				fromPath, _ := pMap["fromFieldPath"].(string)
				toPath, _ := pMap["toFieldPath"].(string)

				if pType == "FromCompositeFieldPath" || pType == "" {
					paramName := strings.TrimPrefix(fromPath, "spec.parameters.")
					paramName = strings.TrimPrefix(paramName, "spec.")
					targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
					targetField = strings.TrimPrefix(targetField, "spec.")

					if paramName != "" && targetField != "" {
						if res.Fields == nil {
							res.Fields = make(map[string]blueprint.Field)
						}
						res.Fields[targetField] = blueprint.Field{
							From: "params." + paramName,
						}
						if _, exists := bp.Spec.XRD.Parameters[paramName]; !exists {
							bp.Spec.XRD.Parameters[paramName] = blueprint.Parameter{
								Type: "string",
							}
						}
					}
				}
			}
		}

		bp.Spec.Resources = append(bp.Spec.Resources, *res)
	}
	return nil
}

func resourceFromMap(m map[string]any, defaultProvider string, placeholders []string) (*blueprint.Resource, error) {
	kind, _ := m["kind"].(string)
	if kind == "" {
		return nil, nil
	}
	apiVersion, _ := m["apiVersion"].(string)

	meta, _ := m["metadata"].(map[string]any)
	name := ""
	if meta != nil {
		name, _ = meta["name"].(string)
	}
	if name == "" {
		name = strings.ToLower(kind)
	}

	provider := defaultProvider
	if strings.Contains(apiVersion, "k8s.io") || !strings.Contains(apiVersion, ".") {
		provider = blueprint.NativeProvider
	}

	res := &blueprint.Resource{
		Name:        name,
		Kind:        kind,
		Provider:    provider,
		Fields:      make(map[string]blueprint.Field),
		Annotations: make(map[string]blueprint.Field),
	}

	// Extract annotations
	if meta != nil {
		if anns, ok := meta["annotations"].(map[string]any); ok {
			for k, v := range anns {
				rawStr := unmaskString(fmt.Sprint(v), placeholders)
				if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
					res.Annotations[k] = blueprint.Field{From: "params." + m[1]}
				} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 3 {
					res.Annotations[k] = blueprint.Field{From: "resources." + m[1] + ".status." + m[2]}
				} else {
					res.Annotations[k] = blueprint.Field{Value: rawStr}
				}
			}
		}
	}

	// Extract spec fields
	if spec, ok := m["spec"].(map[string]any); ok {
		var targetProps map[string]any
		if forProvider, ok := spec["forProvider"].(map[string]any); ok {
			targetProps = forProvider
		} else {
			targetProps = spec
		}

		extractFields("", targetProps, res.Fields, placeholders)
	}

	return res, nil
}

func extractFields(prefix string, obj map[string]any, out map[string]blueprint.Field, placeholders []string) {
	for k, v := range obj {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			extractFields(path, val, out, placeholders)
		case string:
			rawStr := unmaskString(val, placeholders)
			if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
				out[path] = blueprint.Field{From: "params." + m[1]}
			} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 3 {
				out[path] = blueprint.Field{From: "resources." + m[1] + ".status." + m[2]}
			} else {
				out[path] = blueprint.Field{Value: rawStr}
			}
		default:
			rawStr := unmaskString(fmt.Sprint(val), placeholders)
			out[path] = blueprint.Field{Value: rawStr}
		}
	}
}

func unmaskString(s string, placeholders []string) string {
	if len(placeholders) == 0 {
		return s
	}
	var idx int
	if n, _ := fmt.Sscanf(s, "__CF_EXPR_%d__", &idx); n == 1 && idx >= 0 && idx < len(placeholders) {
		return placeholders[idx]
	}
	return s
}

func collectSources(bp *blueprint.Blueprint, defaultRef string) {
	seen := make(map[string]bool)
	for _, r := range bp.Spec.Resources {
		if r.Provider == "" || r.Provider == blueprint.NativeProvider {
			continue
		}
		if !seen[r.Provider] {
			seen[r.Provider] = true
			bp.Spec.Sources = append(bp.Spec.Sources, blueprint.Source{
				Provider: r.Provider,
			})
		}
	}
	if len(bp.Spec.Sources) == 0 && defaultRef != "" {
		bp.Spec.Sources = append(bp.Spec.Sources, blueprint.Source{
			Provider: defaultRef,
		})
	}
}
