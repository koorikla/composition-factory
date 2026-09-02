// Package adopt ingests existing Crossplane Composition (and optional XRD)
// YAML manifests into a structured, round-trippable Blueprint document.
package adopt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// Options configures the adoption parser.
type Options struct {
	// DefaultProviderRef is used when resource provider sources cannot be
	// automatically inferred from the CRD group.
	DefaultProviderRef string
	// CacheDir is the schema cache directory used for schema lookups.
	CacheDir string
}

// LossReport records any dropped fields, unsupported patches, or schema discrepancies.
type LossReport struct {
	Drops []Drop `json:"drops,omitempty"`
}

// Drop represents one dropped item during adoption.
type Drop struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// IsLossy returns true if any fields or actions were dropped.
func (r *LossReport) IsLossy() bool {
	return r != nil && len(r.Drops) > 0
}

// Record appends a drop entry.
func (r *LossReport) Record(path, reason string) {
	if r == nil {
		return
	}
	r.Drops = append(r.Drops, Drop{Path: path, Reason: reason})
}

// String returns a human-readable summary of all dropped items.
func (r *LossReport) String() string {
	if !r.IsLossy() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Adopt loss report (%d dropped item(s)):\n", len(r.Drops)))
	for _, d := range r.Drops {
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", d.Path, d.Reason))
	}
	return sb.String()
}

// FormatAdoptedYAML marshals bp to clean YAML (omitting empty strings and null slices)
// and prepends "# adopt: dropped ..." comments if lossy.
func FormatAdoptedYAML(bp *blueprint.Blueprint, report *LossReport) ([]byte, error) {
	rawJSON, err := json.Marshal(bp)
	if err != nil {
		return nil, fmt.Errorf("marshal blueprint json: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(rawJSON, &root); err != nil {
		return nil, fmt.Errorf("unmarshal blueprint json: %w", err)
	}

	cleaned := cleanAdoptedMap(root, true)

	outBytes, err := yaml.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("marshal blueprint yaml: %w", err)
	}

	if report != nil && report.IsLossy() {
		var comments strings.Builder
		for _, d := range report.Drops {
			comments.WriteString(fmt.Sprintf("# adopt: dropped %s (%s)\n", d.Path, d.Reason))
		}
		outBytes = append([]byte(comments.String()), outBytes...)
	}
	return outBytes, nil
}

func cleanAdoptedMap(v any, isRoot bool) any {
	switch val := v.(type) {
	case map[string]any:
		cleaned := make(map[string]any)
		for k, child := range val {
			if child == nil {
				continue
			}
			if s, ok := child.(string); ok && s == "" {
				if k == "from" || k == "value" || k == "raw" || k == "template" ||
					k == "forEach" || k == "when" || k == "default" || k == "description" ||
					k == "templateSource" || k == "engine" || k == "match" {
					continue
				}
			}
			if slice, ok := child.([]any); ok && len(slice) == 0 {
				if k == "conventions" || k == "pipeline" || k == "enum" {
					continue
				}
			}
			if childMap, ok := child.(map[string]any); ok && len(childMap) == 0 {
				if k == "templates" || k == "envelope" || k == "annotations" || k == "properties" {
					continue
				}
			}
			cleanedChild := cleanAdoptedMap(child, false)
			if cleanedChild != nil {
				cleaned[k] = cleanedChild
			}
		}
		return cleaned
	case []any:
		var cleaned []any
		for _, elem := range val {
			if c := cleanAdoptedMap(elem, false); c != nil {
				cleaned = append(cleaned, c)
			}
		}
		return cleaned
	default:
		return val
	}
}

// Adopt parses Crossplane Composition (and optional XRD) YAML documents and
// produces a valid Blueprint along with a LossReport.
func Adopt(manifest []byte, opts Options) (*blueprint.Blueprint, *LossReport, error) {
	docs, err := splitYAML(manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("split manifest yaml: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil, fmt.Errorf("manifest contains no YAML documents")
	}

	report := &LossReport{}
	var compDoc map[string]any
	var xrdDoc map[string]any

	for _, d := range docs {
		kind, _ := d["kind"].(string)
		switch kind {
		case "Composition":
			compDoc = d
		case "CompositeResourceDefinition":
			xrdDoc = d
		}
	}

	if compDoc == nil {
		return nil, nil, fmt.Errorf("no Composition document found in manifest")
	}

	bp := &blueprint.Blueprint{
		APIVersion: blueprint.APIVersion,
		Kind:       blueprint.Kind,
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
		return nil, nil, fmt.Errorf("composition missing spec section")
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
		parseXRDDoc(xrdDoc, bp, report)
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
	nameMapping := make(map[string]string)
	if pipeline, ok := spec["pipeline"].([]any); ok && len(pipeline) > 0 {
		if err := parsePipelineComposition(pipeline, bp, opts.DefaultProviderRef, report, nameMapping); err != nil {
			return nil, nil, err
		}
	} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
		if err := parseClassicComposition(resources, bp, opts.DefaultProviderRef, report, nameMapping); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("composition has neither spec.pipeline nor spec.resources")
	}

	// Rewrite status references with normalized names
	rewriteStatusReferences(bp, nameMapping)

	// 5. Deduplicate and collect provider sources
	collectSources(bp, opts.DefaultProviderRef)

	// Sort resources by name for deterministic ordering
	sort.Slice(bp.Spec.Resources, func(i, j int) bool {
		return bp.Spec.Resources[i].Name < bp.Spec.Resources[j].Name
	})

	if err := bp.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return bp, report, nil
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

func parseXRDDoc(xrdDoc map[string]any, bp *blueprint.Blueprint, report *LossReport) {
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

	// Check unsupported XRD fields
	if _, ok := spec["claimNames"]; ok {
		report.Record("xrd.claimNames", "claimNames is not supported in blueprint")
	}
	if _, ok := spec["connectionSecretKeys"]; ok {
		report.Record("xrd.connectionSecretKeys", "connectionSecretKeys is not supported in blueprint")
	}

	if versions, ok := spec["versions"].([]any); ok && len(versions) > 0 {
		if v0, ok := versions[0].(map[string]any); ok {
			if vName, ok := v0["name"].(string); ok && bp.Spec.XRD.Version == "" {
				bp.Spec.XRD.Version = vName
			}
			if schema, ok := v0["schema"].(map[string]any); ok {
				if openAPI, ok := schema["openAPIV3Schema"].(map[string]any); ok {
					parseOpenAPISpec(openAPI, bp, report)
				}
			}
		}
	}
}

func parseOpenAPISpec(openAPI map[string]any, bp *blueprint.Blueprint, report *LossReport) {
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

	reqSet := make(map[string]bool)
	collectRequired(specProp, reqSet)

	var paramMap map[string]any
	if paramsObj, ok := specSubProps["parameters"].(map[string]any); ok && paramsObj["properties"] != nil {
		// Classic style: spec.properties.parameters.properties
		collectRequired(paramsObj, reqSet)
		paramMap, _ = paramsObj["properties"].(map[string]any)
	} else {
		// Flat style (Crossplane v2 / cf style): spec.properties
		paramMap = specSubProps
	}

	if paramMap == nil {
		return
	}

	for pName, pVal := range paramMap {
		if pName == "parameters" {
			continue
		}
		pObj, ok := pVal.(map[string]any)
		if !ok {
			continue
		}
		if !isValidParamIdentifier(pName) {
			report.Record("xrd.parameters."+pName, "invalid parameter name (must be camelCase and not a YAML keyword)")
			continue
		}
		param, ok := parseParameter(pName, pObj, reqSet[pName], report, "xrd.parameters."+pName)
		if ok {
			bp.Spec.XRD.Parameters[pName] = param
		}
	}
}

func collectRequired(obj map[string]any, reqSet map[string]bool) {
	if reqList, ok := obj["required"].([]any); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
	}
}

func parseParameter(pName string, pObj map[string]any, isRequired bool, report *LossReport, path string) (blueprint.Parameter, bool) {
	pType, _ := pObj["type"].(string)
	if pType == "" {
		if _, hasProps := pObj["properties"].(map[string]any); hasProps {
			pType = "object"
		} else {
			pType = "string"
		}
	}

	if pType == "array" {
		report.Record(path, "array parameter is not supported in blueprint")
		return blueprint.Parameter{}, false
	}

	if pType == "object" {
		props, hasProps := pObj["properties"].(map[string]any)
		if hasProps && len(props) > 0 {
			childReqSet := make(map[string]bool)
			collectRequired(pObj, childReqSet)
			childParams := make(map[string]blueprint.Parameter)

			for childName, childVal := range props {
				childObj, ok := childVal.(map[string]any)
				if !ok {
					continue
				}
				childPath := path + ".properties." + childName
				if !isValidParamIdentifier(childName) {
					report.Record(childPath, "invalid member name (must be camelCase and not a YAML keyword)")
					continue
				}
				cp, ok := parseParameter(childName, childObj, childReqSet[childName], report, childPath)
				if ok {
					childParams[childName] = cp
				}
			}

			pDesc, _ := pObj["description"].(string)
			if err := checkScalarClean(pDesc); err != nil {
				report.Record(path+".description", "contains control characters")
				pDesc = ""
			}

			return blueprint.Parameter{
				Type:        "object",
				Required:    isRequired,
				Description: pDesc,
				Properties:  childParams,
			}, true
		}
		// Free-form object
		pDesc, _ := pObj["description"].(string)
		return blueprint.Parameter{
			Type:        "object",
			Required:    isRequired,
			Description: pDesc,
		}, true
	}

	pDesc, _ := pObj["description"].(string)
	if err := checkScalarClean(pDesc); err != nil {
		report.Record(path+".description", "contains control characters")
		pDesc = ""
	}

	var pEnum []string
	if enumRaw, ok := pObj["enum"].([]any); ok {
		for _, e := range enumRaw {
			s := fmt.Sprint(e)
			if checkScalarClean(s) == nil {
				pEnum = append(pEnum, s)
			}
		}
	}

	var defStr string
	if defVal, ok := pObj["default"]; ok {
		switch v := defVal.(type) {
		case bool:
			if v {
				defStr = "true"
			} else {
				defStr = "false"
			}
		case float64:
			if v == float64(int64(v)) {
				defStr = strconv.FormatInt(int64(v), 10)
			} else {
				defStr = strconv.FormatFloat(v, 'f', -1, 64)
			}
		default:
			defStr = fmt.Sprint(defVal)
		}
		if err := checkScalarClean(defStr); err != nil {
			report.Record(path+".default", "contains control characters")
			defStr = ""
		}
	}

	return blueprint.Parameter{
		Type:        pType,
		Required:    isRequired,
		Description: pDesc,
		Default:     defStr,
		Enum:        pEnum,
	}, true
}

var (
	reDefine         = regexp.MustCompile(`(?s)\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}(.*?)\{\{-?\s*end\s*-?\}\}`)
	reParamVar       = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reObservedStatus = regexp.MustCompile(`\{\{-?\s*\(index\s+\$observed\s+"([^"]+)"\)\.resource\.status\.atProvider\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reMustacheExpr   = regexp.MustCompile(`\{\{.*?\}\}`)
	paramNameRE      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	dnsInvalidRE     = regexp.MustCompile(`[^a-z0-9-]+`)
	yamlKeywords     = map[string]bool{
		"true": true, "false": true, "yes": true, "no": true,
		"on": true, "off": true, "null": true, "y": true, "n": true,
	}
)

func isReservedCompositeField(name string) bool {
	root := strings.Split(name, ".")[0]
	switch root {
	case "claimRef", "resourceRefs", "resourceRef", "compositionRef", "compositionSelector",
		"compositionRevisionRef", "compositionRevisionSelector", "compositionUpdatePolicy",
		"writeConnectionSecretToRef", "publishConnectionDetailsTo":
		return true
	default:
		return false
	}
}

func isValidParamIdentifier(name string) bool {
	parts := strings.Split(name, ".")
	for _, p := range parts {
		if !paramNameRE.MatchString(p) || yamlKeywords[strings.ToLower(p)] {
			return false
		}
	}
	return true
}

func normalizeDNSLabel(name string) string {
	s := strings.ToLower(name)
	s = dnsInvalidRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	if s == "" || !unicode.IsLetter(rune(s[0])) && !unicode.IsDigit(rune(s[0])) {
		s = "res-" + s
		s = strings.Trim(s, "-")
	}
	if s == "res" || s == "" {
		s = "res-1"
	}
	return s
}

func parsePipelineComposition(pipeline []any, bp *blueprint.Blueprint, defaultProvider string, report *LossReport, nameMapping map[string]string) error {
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
				if err := parseGoTemplateBody(tmpl, bp, defaultProvider, report, nameMapping); err != nil {
					return fmt.Errorf("parse go template: %w", err)
				}
			}
		} else if fnName == "function-patch-and-transform" || strings.Contains(fnName, "patch-and-transform") {
			input, _ := step["input"].(map[string]any)
			if input != nil {
				if resources, ok := input["resources"].([]any); ok {
					if err := parseClassicComposition(resources, bp, defaultProvider, report, nameMapping); err != nil {
						return fmt.Errorf("parse patch-and-transform resources: %w", err)
					}
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

func parseGoTemplateBody(tmpl string, bp *blueprint.Blueprint, defaultProvider string, report *LossReport, nameMapping map[string]string) error {
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
			if isValidParamIdentifier(pName) {
				ensureParamDeclared(bp, pName)
			} else {
				report.Record("template.param."+pName, "invalid parameter identifier")
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
		res := resourceFromMap(doc, defaultProvider, placeholderTable, report, nameMapping)
		if res == nil {
			continue
		}
		bp.Spec.Resources = append(bp.Spec.Resources, *res)
	}

	return nil
}

func parseClassicComposition(resources []any, bp *blueprint.Blueprint, defaultProvider string, report *LossReport, nameMapping map[string]string) error {
	for resIdx, resRaw := range resources {
		resMap, ok := resRaw.(map[string]any)
		if !ok {
			continue
		}
		resName, _ := resMap["name"].(string)
		base, _ := resMap["base"].(map[string]any)
		if base == nil {
			continue
		}

		res := resourceFromMap(base, defaultProvider, nil, report, nameMapping)
		if res == nil {
			continue
		}
		if resName != "" {
			normName := normalizeDNSLabel(resName)
			if normName != resName {
				nameMapping[resName] = normName
			}
			res.Name = normName
		}

		// Ensure unique name
		uniqueName(bp, res)

		// Apply patches
		if patches, ok := resMap["patches"].([]any); ok {
			for patchIdx, pRaw := range patches {
				pMap, ok := pRaw.(map[string]any)
				if !ok {
					continue
				}
				pType, _ := pMap["type"].(string)
				fromPath, _ := pMap["fromFieldPath"].(string)
				toPath, _ := pMap["toFieldPath"].(string)

				if pType == "FromCompositeFieldPath" || pType == "" {
					var isParamPatch bool
					var paramName string
					if strings.HasPrefix(fromPath, "spec.parameters.") {
						paramName = strings.TrimPrefix(fromPath, "spec.parameters.")
						isParamPatch = true
					} else if strings.HasPrefix(fromPath, "spec.") {
						paramName = strings.TrimPrefix(fromPath, "spec.")
						isParamPatch = true
					}

					targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
					targetField = strings.TrimPrefix(targetField, "spec.")

					if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
						if res.Fields == nil {
							res.Fields = make(map[string]blueprint.Field)
						}
						res.Fields[targetField] = blueprint.Field{
							From: "params." + paramName,
						}
						ensureParamDeclared(bp, paramName)
					} else {
						report.Record(fmt.Sprintf("resource.%s.patches[%d]", res.Name, patchIdx),
							fmt.Sprintf("unsupported fromFieldPath %q in patch", fromPath))
					}
				} else {
					report.Record(fmt.Sprintf("resource.%s.patches[%d]", res.Name, patchIdx),
						fmt.Sprintf("patch type %q is not supported in blueprint", pType))
				}
			}
		}

		// Check readinessChecks and connectionDetails
		if _, ok := resMap["readinessChecks"]; ok {
			report.Record(fmt.Sprintf("resource.%s.readinessChecks", res.Name), "readinessChecks are not supported in blueprint")
		}
		if _, ok := resMap["connectionDetails"]; ok {
			report.Record(fmt.Sprintf("resource.%s.connectionDetails", res.Name), "connectionDetails are not supported in blueprint")
		}

		bp.Spec.Resources = append(bp.Spec.Resources, *res)
		_ = resIdx
	}
	return nil
}

func uniqueName(bp *blueprint.Blueprint, res *blueprint.Resource) {
	original := res.Name
	counter := 2
	for bp.ResourceNamed(res.Name) != nil {
		res.Name = fmt.Sprintf("%s-%d", original, counter)
		counter++
	}
}

func ensureParamDeclared(bp *blueprint.Blueprint, paramPath string) {
	parts := strings.Split(paramPath, ".")
	root := parts[0]
	if len(parts) == 1 {
		if _, exists := bp.Spec.XRD.Parameters[root]; !exists {
			bp.Spec.XRD.Parameters[root] = blueprint.Parameter{
				Type:     "string",
				Required: false,
			}
		}
		return
	}

	// Nested object member
	rootParam, exists := bp.Spec.XRD.Parameters[root]
	if !exists {
		rootParam = blueprint.Parameter{
			Type:       "object",
			Properties: make(map[string]blueprint.Parameter),
		}
	} else if rootParam.Type != "object" {
		rootParam.Type = "object"
		if rootParam.Properties == nil {
			rootParam.Properties = make(map[string]blueprint.Parameter)
		}
	} else if rootParam.Properties == nil {
		rootParam.Properties = make(map[string]blueprint.Parameter)
	}

	member := parts[1]
	if _, mExists := rootParam.Properties[member]; !mExists {
		rootParam.Properties[member] = blueprint.Parameter{
			Type: "string",
		}
	}
	bp.Spec.XRD.Parameters[root] = rootParam
}

func resourceFromMap(m map[string]any, defaultProvider string, placeholders []string, report *LossReport, nameMapping map[string]string) *blueprint.Resource {
	kind, _ := m["kind"].(string)
	if kind == "" {
		return nil
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
	normName := normalizeDNSLabel(name)
	if normName != name && nameMapping != nil {
		nameMapping[name] = normName
	}

	provider := defaultProvider
	if strings.Contains(apiVersion, "k8s.io") || !strings.Contains(apiVersion, ".") {
		provider = blueprint.NativeProvider
	}

	res := &blueprint.Resource{
		Name:        normName,
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
				if err := checkScalarClean(rawStr); err != nil {
					report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, k), "contains newlines or control characters")
					continue
				}
				if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
					if isValidParamIdentifier(m[1]) {
						res.Annotations[k] = blueprint.Field{From: "params." + m[1]}
					} else {
						report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, k), "invalid parameter reference")
					}
				} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 3 {
					srcRes := m[1]
					if nameMapping != nil && nameMapping[srcRes] != "" {
						srcRes = nameMapping[srcRes]
					} else {
						srcRes = normalizeDNSLabel(srcRes)
					}
					res.Annotations[k] = blueprint.Field{From: "resources." + srcRes + ".status." + m[2]}
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

		extractFields("", targetProps, res.Fields, placeholders, res.Name, report, nameMapping)
	}

	return res
}

func extractFields(prefix string, obj map[string]any, out map[string]blueprint.Field, placeholders []string, resName string, report *LossReport, nameMapping map[string]string) {
	for k, v := range obj {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			extractFields(path, val, out, placeholders, resName, report, nameMapping)
		case string:
			rawStr := unmaskString(val, placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path),
					"multi-line scalar contains newlines, which is not supported in blueprint values")
				continue
			}
			if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
				if isValidParamIdentifier(m[1]) {
					out[path] = blueprint.Field{From: "params." + m[1]}
				} else {
					report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "invalid parameter reference")
				}
			} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 3 {
				srcRes := m[1]
				if nameMapping != nil && nameMapping[srcRes] != "" {
					srcRes = nameMapping[srcRes]
				} else {
					srcRes = normalizeDNSLabel(srcRes)
				}
				out[path] = blueprint.Field{From: "resources." + srcRes + ".status." + m[2]}
			} else if strings.Contains(rawStr, "{{") {
				out[path] = blueprint.Field{Raw: rawStr}
			} else {
				out[path] = blueprint.Field{Value: rawStr}
			}
		case []any:
			report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "array field values cannot be represented as scalar fields")
		default:
			rawStr := unmaskString(fmt.Sprint(val), placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path),
					"contains newlines or control characters")
				continue
			}
			out[path] = blueprint.Field{Value: rawStr}
		}
	}
}

func checkScalarClean(s string) error {
	for i, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("control character at %d", i)
		}
	}
	return nil
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

func rewriteStatusReferences(bp *blueprint.Blueprint, nameMapping map[string]string) {
	if len(nameMapping) == 0 {
		return
	}
	for i := range bp.Spec.Resources {
		r := &bp.Spec.Resources[i]
		for fName, f := range r.Fields {
			if f.From != "" {
				f.From = rewriteFromWire(f.From, nameMapping)
				r.Fields[fName] = f
			}
		}
		for aName, a := range r.Annotations {
			if a.From != "" {
				a.From = rewriteFromWire(a.From, nameMapping)
				r.Annotations[aName] = a
			}
		}
	}
}

func rewriteFromWire(wire string, nameMapping map[string]string) string {
	if !strings.HasPrefix(wire, "resources.") {
		return wire
	}
	rest := strings.TrimPrefix(wire, "resources.")
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) >= 3 && parts[1] == "status" {
		origName := parts[0]
		if newName, ok := nameMapping[origName]; ok {
			return fmt.Sprintf("resources.%s.status.%s", newName, parts[2])
		}
	}
	return wire
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
