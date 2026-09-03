// Package adopt ingests existing Crossplane Composition (and optional XRD)
// YAML manifests into a structured, round-trippable Blueprint document.
package adopt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/catalogue"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
)

// Options configures the adoption parser.
type Options struct {
	// DefaultProviderRef is used when resource provider sources cannot be
	// automatically inferred from the CRD group.
	DefaultProviderRef string
	// CacheDir is the schema cache directory used for schema lookups.
	CacheDir string
	// FunctionPackages maps function names to pinned package references.
	FunctionPackages map[string]string
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

// ScrubCount returns the number of server-side metadata, status, or annotation fields scrubbed.
func (r *LossReport) ScrubCount() int {
	if r == nil {
		return 0
	}
	count := 0
	for _, d := range r.Drops {
		if strings.Contains(d.Reason, "scrubbed") {
			count++
		}
	}
	return count
}

// HasTrueLoss returns true if any non-scrubbed functional fields were dropped.
func (r *LossReport) HasTrueLoss() bool {
	if r == nil {
		return false
	}
	for _, d := range r.Drops {
		if !strings.Contains(d.Reason, "scrubbed") {
			return true
		}
	}
	return false
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
				if k == "templates" || k == "envelope" || k == "annotations" || k == "properties" || k == "environment" {
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

	docs = unwrapListDocs(docs)
	report := &LossReport{}
	ScrubDocuments(docs, report)

	if opts.FunctionPackages == nil {
		opts.FunctionPackages = make(map[string]string)
	}

	var compDoc map[string]any
	var xrdDoc map[string]any

	for _, d := range docs {
		kind, _ := d["kind"].(string)
		switch kind {
		case "Composition":
			compDoc = d
		case "CompositeResourceDefinition":
			xrdDoc = d
		case "Function":
			if meta, ok := d["metadata"].(map[string]any); ok {
				fnName, _ := meta["name"].(string)
				if fSpec, ok := d["spec"].(map[string]any); ok {
					if pkg, ok := fSpec["package"].(string); ok && fnName != "" {
						opts.FunctionPackages[fnName] = pkg
					}
				}
			}
		case "Configuration":
			if cSpec, ok := d["spec"].(map[string]any); ok {
				if deps, ok := cSpec["dependsOn"].([]any); ok {
					for _, depRaw := range deps {
						if dep, ok := depRaw.(map[string]any); ok {
							depKind, _ := dep["kind"].(string)
							depFn, _ := dep["function"].(string)
							depPkg, _ := dep["package"].(string)
							depVer, _ := dep["version"].(string)
							if depKind == "Function" || depFn != "" {
								name := depFn
								if name == "" {
									name = depPkg
								}
								pkg := depPkg
								if pkg == "" {
									pkg = depFn
								}
								cleanVer := strings.TrimPrefix(depVer, "=")
								cleanVer = strings.TrimLeft(cleanVer, ">=<~^ ")
								if cleanVer != "" && !strings.Contains(pkg, ":") && !strings.Contains(pkg, "@") {
									pkg = pkg + ":" + cleanVer
								}
								if name != "" && pkg != "" {
									opts.FunctionPackages[name] = pkg
								}
							}
						}
					}
				}
			}
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
		if anns, ok := meta["annotations"].(map[string]any); ok {
			if envKeysRaw, ok := anns[blueprint.EnvironmentKeysAnnotation].(string); ok && envKeysRaw != "" {
				var envKeys map[string]blueprint.EnvironmentKey
				if err := json.Unmarshal([]byte(envKeysRaw), &envKeys); err == nil && len(envKeys) > 0 {
					bp.Spec.Environment = envKeys
				}
			}
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
		if err := parsePipelineComposition(pipeline, bp, opts, report, nameMapping); err != nil {
			return nil, nil, err
		}
	} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
		if err := parseClassicComposition(resources, bp, opts, report, nameMapping); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("composition has neither spec.pipeline nor spec.resources")
	}

	// Rewrite status references with normalized names
	rewriteStatusReferences(bp, nameMapping)

	// 5. Deduplicate and collect provider sources
	collectSources(bp, opts.DefaultProviderRef)

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
	if scope, ok := spec["scope"].(string); ok && bp.Spec.XRD.Scope == "" {
		bp.Spec.XRD.Scope = scope
	}

	// Check unsupported XRD fields
	if _, ok := spec["claimNames"]; ok {
		report.Record("xrd.claimNames", "claimNames is not supported in blueprint")
	}
	if _, ok := spec["connectionSecretKeys"]; ok {
		report.Record("xrd.connectionSecretKeys", "connectionSecretKeys is not supported in blueprint")
	}

	if versions, ok := spec["versions"].([]any); ok && len(versions) > 0 {
		var matchedVersion map[string]any
		for _, v := range versions {
			if vMap, ok := v.(map[string]any); ok {
				vName, _ := vMap["name"].(string)
				if bp.Spec.XRD.Version != "" && vName == bp.Spec.XRD.Version {
					matchedVersion = vMap
					break
				}
				if matchedVersion == nil {
					matchedVersion = vMap
				}
			}
		}
		if matchedVersion != nil {
			if vName, ok := matchedVersion["name"].(string); ok && bp.Spec.XRD.Version == "" {
				bp.Spec.XRD.Version = vName
			}
			if schema, ok := matchedVersion["schema"].(map[string]any); ok {
				if openAPI, ok := schema["openAPIV3Schema"].(map[string]any); ok {
					parseOpenAPISpec(openAPI, bp, report)
				}
			}
		}
	} else if validation, ok := spec["validation"].(map[string]any); ok {
		if openAPI, ok := validation["openAPIV3Schema"].(map[string]any); ok {
			parseOpenAPISpec(openAPI, bp, report)
		}
	} else if schema, ok := spec["schema"].(map[string]any); ok {
		if openAPI, ok := schema["openAPIV3Schema"].(map[string]any); ok {
			parseOpenAPISpec(openAPI, bp, report)
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
	} else if reqList, ok := obj["required"].([]string); ok {
		for _, s := range reqList {
			reqSet[s] = true
		}
	}
}

func parseParameter(pName string, pObj map[string]any, isRequired bool, report *LossReport, path string) (blueprint.Parameter, bool) {
	if reqBool, ok := pObj["required"].(bool); ok && reqBool {
		isRequired = true
	}
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
	} else if enumRaw, ok := pObj["enum"].([]string); ok {
		for _, e := range enumRaw {
			if checkScalarClean(e) == nil {
				pEnum = append(pEnum, e)
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
		case int:
			defStr = strconv.Itoa(v)
		case int64:
			defStr = strconv.FormatInt(v, 10)
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
	reDefine             = regexp.MustCompile(`(?s)\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}(.*?)\{\{-?\s*end\s*-?\}\}`)
	reParamVar           = regexp.MustCompile(`\{\{-?\s*(?:\$spec|\.spec|\.observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+?)(?:\s*\|\s*quote)?\s*-?\}\}`)
	reEnvVar             = regexp.MustCompile(`\{\{-?\s*(?:default\s+(?:"[^"]*"|\S+)\s+)?(?:\$env\.([a-zA-Z0-9_.-]+?)|\(index\s+\$env\s+"([a-zA-Z0-9_.-]+?)"\)|index\s+\$env\s+"([a-zA-Z0-9_.-]+?)")(?:\s*\|\s*quote)?\s*-?\}\}`)
	reObservedStatus     = regexp.MustCompile(`\{\{-?\s*(?:\(index\s+(?:\$\.?observed(?:\.resources)?|\$observed)\s+"([^"]+)"\)|(?:\$\.?observed(?:\.resources)?|\$observed)\.([a-zA-Z0-9_-]+))\.resource\.(status(?:\.atProvider)?|metadata)\.([a-zA-Z0-9_.-]+?)(?:\s*\|\s*quote)?\s*-?\}\}`)
	reXRResourceRef      = regexp.MustCompile(`\{\{-?\s*\$xr\s*-?\}\}-([a-zA-Z0-9-]+)`)
	reWhenIfSimple       = regexp.MustCompile(`\{\{-?\s*if\s+\$spec\.([a-zA-Z0-9_.-]+)\s*-?\}\}`)
	reWhenIfEq           = regexp.MustCompile(`\{\{-?\s*if\s+eq\s+\$spec\.([a-zA-Z0-9_.-]+)\s+"([^"]+)"\s*-?\}\}`)
	reWhenIfNe           = regexp.MustCompile(`\{\{-?\s*if\s+ne\s+\$spec\.([a-zA-Z0-9_.-]+)\s+"([^"]+)"\s*-?\}\}`)
	reWhenIfEnvSimple    = regexp.MustCompile(`\{\{-?\s*if\s+(?:(?:and\s+\(hasKey\s+\$env\s+"[^"]+"\)\s+)?\$env\.([a-zA-Z0-9_.-]+)|default\s+(?:"[^"]*"|\S+)\s+\(index\s+\$env\s+"([a-zA-Z0-9_.-]+)"\))\s*-?\}\}`)
	reWhenIfEnvEq        = regexp.MustCompile(`\{\{-?\s*if\s+(?:(?:and\s+\(hasKey\s+\$env\s+"[^"]+"\)\s+)?\(?eq\s+\$env\.([a-zA-Z0-9_.-]+)\s+"?([^"]+?)"?\)?|eq\s+\(default\s+(?:"[^"]*"|\S+)\s+\(index\s+\$env\s+"([a-zA-Z0-9_.-]+)"\)\)\s+"?([^"]+?)"?)\s*-?\}\}`)
	reWhenIfEnvNe        = regexp.MustCompile(`\{\{-?\s*if\s+(?:(?:or\s+\(not\s+\(hasKey\s+\$env\s+"[^"]+"\)\)\s+)?\(?ne\s+\$env\.([a-zA-Z0-9_.-]+)\s+"?([^"]+?)"?\)?|ne\s+\(default\s+(?:"[^"]*"|\S+)\s+\(index\s+\$env\s+"([a-zA-Z0-9_.-]+)"\)\)\s+"?([^"]+?)"?)\s*-?\}\}`)
	reForEachLoop        = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s+\$spec\.([a-zA-Z0-9_.-]+)\)\s*-?\}\}`)
	reForEachEnvLoop     = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s+(?:\$env\.([a-zA-Z0-9_.-]+)|\(default\s+(?:"[^"]*"|\S+)\s+\(index\s+\$env\s+"([a-zA-Z0-9_.-]+)"\)\))\)\s*-?\}\}`)
	reMustacheExpr       = regexp.MustCompile(`\{\{.*?\}\}`)
	reDocSeparator       = regexp.MustCompile(`(?m)^\s*---\s*$`)
	reSetResourceNameAnn = regexp.MustCompile(`setResourceNameAnnotation\s+(?:\(printf\s+"([^"]+)"|"([^"]+)")`)
	rePrintfFormat       = regexp.MustCompile(`printf\s+"([^"]+)"`)
	reXRNameSuffix       = regexp.MustCompile(`\{\{-?\s*\$xr\s*-?\}\}-([a-zA-Z0-9_-]+)`)
	paramNameRE          = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	dnsInvalidRE         = regexp.MustCompile(`[^a-z0-9-]+`)
	yamlKeywords         = map[string]bool{
		"true": true, "false": true, "yes": true, "no": true,
		"on": true, "off": true, "null": true, "y": true, "n": true,
	}
)

func matchEnvVar(s string) string {
	if m := reEnvVar.FindStringSubmatch(s); len(m) > 1 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				return m[i]
			}
		}
	}
	return ""
}

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

func inferProvider(apiVersion, kind string, defaultProvider string, cacheDir string, bp *blueprint.Blueprint) string {
	if strings.Contains(apiVersion, "k8s.io") || !strings.Contains(apiVersion, ".") {
		return blueprint.NativeProvider
	}

	// 1. Check if an existing source in bp matches
	if bp != nil {
		for _, s := range bp.Spec.Sources {
			if s.Provider != "" {
				pkgName := s.Provider
				if i := strings.LastIndex(pkgName, "/"); i >= 0 {
					pkgName = pkgName[i+1:]
				}
				if i := strings.Index(pkgName, ":"); i >= 0 {
					pkgName = pkgName[:i]
				}
				for _, k := range catalogue.Kinds(pkgName) {
					if strings.EqualFold(k, kind) {
						return s.Provider
					}
				}
			}
		}
	}

	// 2. Check local schema cache if available
	if cacheDir != "" {
		store := cache.New(cacheDir)
		if list, err := store.List(); err == nil && len(list) > 0 {
			for _, ref := range list {
				if crds, err := store.Load(ref); err == nil {
					for _, c := range crds {
						crdKind := c.Kind
						crdGroup := c.Group
						group := strings.Split(apiVersion, "/")[0]
						if (crdGroup == group || strings.TrimSuffix(crdGroup, ".m.upbound.io") == strings.TrimSuffix(group, ".upbound.io")) && strings.EqualFold(crdKind, kind) {
							return ref
						}
					}
				}
			}
		}
	}

	// 3. Infer from catalogue and group
	group := strings.Split(apiVersion, "/")[0]
	var candidatePkg string
	if strings.HasSuffix(group, ".upbound.io") {
		trimmed := strings.TrimSuffix(group, ".upbound.io")
		trimmed = strings.TrimSuffix(trimmed, ".m")
		parts := strings.Split(trimmed, ".")
		if len(parts) >= 2 {
			service := parts[0]
			cloud := parts[1]
			candidatePkg = fmt.Sprintf("provider-%s-%s", cloud, service)
		} else if len(parts) == 1 {
			candidatePkg = fmt.Sprintf("provider-%s", parts[0])
		}
	} else if strings.HasSuffix(group, ".crossplane.io") {
		svc := strings.TrimSuffix(group, ".crossplane.io")
		candidatePkg = fmt.Sprintf("provider-%s", svc)
	}

	if candidatePkg == "" {
		pkgs := catalogue.PackagesForKind(kind)
		if len(pkgs) > 0 {
			candidatePkg = pkgs[0]
		}
	}

	if candidatePkg != "" {
		if providers, err := catalogue.Load(); err == nil {
			for _, p := range providers {
				if p.Name == candidatePkg {
					if p.Ref != "" {
						return p.Ref
					}
					return p.Name
				}
			}
		}
		return candidatePkg
	}

	if defaultProvider != "" {
		return defaultProvider
	}

	return ""
}

func extractResourceName(m map[string]any, kind string, placeholders []string) string {
	meta, _ := m["metadata"].(map[string]any)
	name := ""
	if meta != nil {
		if anns, ok := meta["annotations"].(map[string]any); ok {
			if annName, ok := anns["crossplane.io/composition-resource-name"].(string); ok && annName != "" {
				unmasked := unmaskString(annName, placeholders)
				if clean := extractCleanName(unmasked); clean != "" {
					return clean
				}
				name = annName
			}
			for k, v := range anns {
				unmaskedK := unmaskString(fmt.Sprint(k), placeholders)
				unmaskedV := unmaskString(fmt.Sprint(v), placeholders)
				if m := reSetResourceNameAnn.FindStringSubmatch(unmaskedK); len(m) >= 2 {
					candidate := m[1]
					if candidate == "" && len(m) >= 3 {
						candidate = m[2]
					}
					if clean := extractCleanName(candidate); clean != "" {
						return clean
					}
				}
				if m := reSetResourceNameAnn.FindStringSubmatch(unmaskedV); len(m) >= 2 {
					candidate := m[1]
					if candidate == "" && len(m) >= 3 {
						candidate = m[2]
					}
					if clean := extractCleanName(candidate); clean != "" {
						return clean
					}
				}
			}
		}
		if rawName, ok := meta["name"].(string); ok && rawName != "" {
			unmasked := unmaskString(rawName, placeholders)
			if clean := extractCleanName(unmasked); clean != "" {
				return clean
			}
			name = unmasked
		}
	}
	if name != "" {
		if clean := extractCleanName(name); clean != "" {
			return clean
		}
	}
	return strings.ToLower(kind)
}

func extractCleanName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "__CF_EXPR_") || strings.HasPrefix(raw, "cf-expr-") || strings.HasPrefix(raw, "__cf_expr_") {
		return ""
	}

	if m := rePrintfFormat.FindStringSubmatch(raw); len(m) >= 2 {
		fmtStr := m[1]
		clean := cleanFormatString(fmtStr)
		if clean != "" {
			return clean
		}
	}

	if m := reXRNameSuffix.FindStringSubmatch(raw); len(m) >= 2 {
		clean := m[1]
		clean = strings.TrimSuffix(clean, "-{{ $i }}")
		clean = strings.TrimSuffix(clean, "-$i")
		return clean
	}

	if strings.Contains(raw, "%") {
		clean := cleanFormatString(raw)
		if clean != "" {
			return clean
		}
	}

	cleaned := reMustacheExpr.ReplaceAllString(raw, "")
	cleaned = strings.Trim(cleaned, "-_ ")
	if strings.HasPrefix(cleaned, "$xr-") || strings.HasPrefix(cleaned, "$xr_") {
		cleaned = cleaned[4:]
	}
	if cleaned != "" && !strings.Contains(cleaned, "%") && !strings.HasPrefix(cleaned, "__") {
		return cleaned
	}

	if !strings.Contains(raw, "{{") && !strings.Contains(raw, " ") {
		return raw
	}
	return ""
}

func cleanFormatString(fmtStr string) string {
	s := fmtStr
	s = strings.TrimPrefix(s, "%s-")
	s = strings.TrimPrefix(s, "%s_")
	s = strings.TrimPrefix(s, "$xr-")
	s = strings.TrimPrefix(s, "$xr_")
	s = strings.TrimSuffix(s, "-%d")
	s = strings.TrimSuffix(s, "_%d")
	s = strings.TrimSuffix(s, "-%s")
	s = strings.TrimSuffix(s, "_%s")
	s = strings.TrimSuffix(s, "-%i")
	s = strings.TrimSuffix(s, "_%i")
	s = strings.Trim(s, "-_ ")
	if s != "" && s != "%s" && s != "%d" && s != "%i" && !strings.Contains(s, "%") {
		return s
	}
	return ""
}

func parsePipelineComposition(pipeline []any, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string) error {
	var otherSteps []blueprint.PipelineStep
	seenEngineStep := false

	for _, stepRaw := range pipeline {
		step, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		fnRef, _ := step["functionRef"].(map[string]any)
		fnName, _ := fnRef["name"].(string)
		if fnName == "" {
			fnName, _ = step["function"].(string)
		}
		stepName, _ := step["step"].(string)
		if stepName == "" {
			stepName, _ = step["name"].(string)
		}

		if fnName == "function-go-templating" || strings.Contains(fnName, "gotemplating") {
			seenEngineStep = true
			input, _ := step["input"].(map[string]any)
			inline, _ := input["inline"].(map[string]any)
			tmpl, _ := inline["template"].(string)
			if tmpl != "" {
				if err := parseGoTemplateBody(tmpl, bp, opts, report, nameMapping); err != nil {
					return fmt.Errorf("parse go template: %w", err)
				}
			}
		} else if fnName == "function-patch-and-transform" || strings.Contains(fnName, "patch-and-transform") {
			seenEngineStep = true
			input, _ := step["input"].(map[string]any)
			if input != nil {
				if resources, ok := input["resources"].([]any); ok {
					if err := parseClassicComposition(resources, bp, opts, report, nameMapping); err != nil {
						return fmt.Errorf("parse patch-and-transform resources: %w", err)
					}
				}
			}
		} else {
			var pkg string
			var inputYAML string
			if input, ok := step["input"].(map[string]any); ok {
				if p, ok := input["package"].(string); ok {
					pkg = p
				}
				inputCopy := make(map[string]any)
				for k, v := range input {
					if k != "package" {
						inputCopy[k] = v
					}
				}
				if len(inputCopy) > 0 {
					if yBytes, err := yaml.Marshal(inputCopy); err == nil {
						inputYAML = strings.TrimSpace(string(yBytes))
					}
				}
			}
			if pkg == "" && opts.FunctionPackages != nil {
				if p, ok := opts.FunctionPackages[fnName]; ok && p != "" {
					pkg = p
				} else if p, ok := opts.FunctionPackages[stepName]; ok && p != "" {
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
			pos := "after"
			if !seenEngineStep {
				pos = "before"
			}
			otherSteps = append(otherSteps, blueprint.PipelineStep{
				Name:        stepName,
				FunctionRef: fnName,
				Package:     pkg,
				Input:       inputYAML,
				Position:    pos,
			})
		}
	}

	var finalSteps []blueprint.PipelineStep
	for _, s := range otherSteps {
		if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName && len(bp.Spec.Environment) > 0 {
			if s.Input == "" || strings.TrimSpace(s.Input) == strings.TrimSpace(blueprint.DefaultEnvironmentConfigsInput) {
				continue
			}
		}
		finalSteps = append(finalSteps, s)
	}
	bp.Spec.Pipeline = finalSteps
	return nil
}

func parseGoTemplateBody(tmpl string, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string) error {
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

	// 2. Discover parameter and environment references
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
	envMatches := reEnvVar.FindAllString(cleanTmpl, -1)
	for _, raw := range envMatches {
		key := matchEnvVar(raw)
		if key != "" && isValidParamIdentifier(key) {
			ensureEnvDeclared(bp, key, "string")
		}
	}

	// 3. Process documents per chunk to capture when / forEach guards and resources
	chunks := reDocSeparator.Split(cleanTmpl, -1)
	var nextWhen, nextForEach string
	for _, chunk := range chunks {
		trimmedChunk := strings.TrimSpace(chunk)
		if trimmedChunk == "" {
			continue
		}

		when := nextWhen
		forEach := nextForEach
		nextWhen = ""
		nextForEach = ""

		if m := reWhenIfEnvEq.FindStringSubmatch(chunk); len(m) >= 3 {
			key, lit := m[1], m[2]
			if key == "" && len(m) >= 5 {
				key, lit = m[3], m[4]
			}
			nextWhen = fmt.Sprintf("env.%s == %s", key, lit)
			ensureEnvDeclared(bp, key, "string")
		} else if m := reWhenIfEnvNe.FindStringSubmatch(chunk); len(m) >= 3 {
			key, lit := m[1], m[2]
			if key == "" && len(m) >= 5 {
				key, lit = m[3], m[4]
			}
			nextWhen = fmt.Sprintf("env.%s != %s", key, lit)
			ensureEnvDeclared(bp, key, "string")
		} else if m := reWhenIfEnvSimple.FindStringSubmatch(chunk); len(m) >= 2 {
			key := m[1]
			if key == "" && len(m) >= 3 {
				key = m[2]
			}
			nextWhen = fmt.Sprintf("env.%s", key)
			ensureEnvDeclared(bp, key, "boolean")
		} else if m := reWhenIfEq.FindStringSubmatch(chunk); len(m) >= 3 {
			nextWhen = fmt.Sprintf("params.%s == %s", m[1], m[2])
			ensureParamDeclared(bp, m[1])
		} else if m := reWhenIfNe.FindStringSubmatch(chunk); len(m) >= 3 {
			nextWhen = fmt.Sprintf("params.%s != %s", m[1], m[2])
			ensureParamDeclared(bp, m[1])
		} else if m := reWhenIfSimple.FindStringSubmatch(chunk); len(m) >= 2 {
			nextWhen = fmt.Sprintf("params.%s", m[1])
			ensureParamDeclared(bp, m[1])
		}

		if m := reForEachEnvLoop.FindStringSubmatch(chunk); len(m) >= 2 {
			key := m[1]
			if key == "" && len(m) >= 3 {
				key = m[2]
			}
			nextForEach = fmt.Sprintf("env.%s", key)
			ensureEnvDeclared(bp, key, "integer")
		} else if m := reForEachLoop.FindStringSubmatch(chunk); len(m) >= 2 {
			nextForEach = fmt.Sprintf("params.%s", m[1])
			ensureParamDeclared(bp, m[1])
		}

		lines := strings.Split(chunk, "\n")
		var filteredLines []string
		skipNextEmptyBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if m := reSetResourceNameAnn.FindStringSubmatch(trimmed); len(m) >= 2 {
				annVal := m[1]
				if annVal == "" && len(m) >= 3 {
					annVal = m[2]
				}
				filteredLines = append(filteredLines, strings.Replace(line, trimmed, fmt.Sprintf(`"crossplane.io/composition-resource-name": "%s"`, annVal), 1))
				continue
			}
			if strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}") {
				inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{{"), "}}"))
				inner = strings.TrimPrefix(inner, "-")
				inner = strings.TrimSuffix(inner, "-")
				inner = strings.TrimSpace(inner)
				if strings.HasPrefix(inner, "$") && strings.Contains(inner, ":=") {
					continue
				}
				if strings.HasPrefix(inner, "if ") || strings.HasPrefix(inner, "else") ||
					strings.HasPrefix(inner, "end") || strings.HasPrefix(inner, "range ") {
					if strings.HasPrefix(inner, "else") {
						skipNextEmptyBlock = true
					} else {
						skipNextEmptyBlock = false
					}
					continue
				}
			}
			if skipNextEmptyBlock && (trimmed == "{}" || trimmed == "[]") {
				skipNextEmptyBlock = false
				continue
			}
			skipNextEmptyBlock = false
			filteredLines = append(filteredLines, line)
		}
		cleanYAML := strings.Join(filteredLines, "\n")
		if strings.TrimSpace(cleanYAML) == "" {
			continue
		}

		var placeholderTable []string
		maskedYAML := reMustacheExpr.ReplaceAllStringFunc(cleanYAML, func(match string) string {
			idx := len(placeholderTable)
			placeholderTable = append(placeholderTable, match)
			return fmt.Sprintf(`__CF_EXPR_%d__`, idx)
		})

		docs, err := splitYAML([]byte(maskedYAML))
		if err != nil {
			continue
		}
		docs = unwrapListDocs(docs)
		for _, doc := range docs {
			ScrubDocument(doc, "", report)
			res := resourceFromMap(doc, opts, placeholderTable, report, nameMapping, bp)
			if res == nil {
				continue
			}
			if when != "" {
				res.When = when
			}
			if forEach != "" {
				res.ForEach = forEach
			}
			bp.Spec.Resources = append(bp.Spec.Resources, *res)
		}
	}

	return nil
}

func parseClassicComposition(resources []any, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string) error {
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

		res := resourceFromMap(base, opts, nil, report, nameMapping, bp)
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

					if strings.HasPrefix(toPath, "spec.forProvider.") {
						targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
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
					} else if strings.HasPrefix(toPath, "spec.") {
						targetField := strings.TrimPrefix(toPath, "spec.")
						if isParamPatch && paramName != "" && targetField != "" && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
							if res.Envelope == nil {
								res.Envelope = make(map[string]blueprint.Field)
							}
							res.Envelope[targetField] = blueprint.Field{
								From: "params." + paramName,
							}
							ensureParamDeclared(bp, paramName)
						} else {
							report.Record(fmt.Sprintf("resource.%s.patches[%d]", res.Name, patchIdx),
								fmt.Sprintf("unsupported fromFieldPath %q in patch", fromPath))
						}
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

func ensureEnvDeclared(bp *blueprint.Blueprint, envKey, typ string) {
	if bp.Spec.Environment == nil {
		bp.Spec.Environment = make(map[string]blueprint.EnvironmentKey)
	}
	if typ == "" {
		typ = "string"
	}
	existing, exists := bp.Spec.Environment[envKey]
	if !exists {
		bp.Spec.Environment[envKey] = blueprint.EnvironmentKey{
			Type: typ,
		}
		return
	}
	if existing.Type == "string" && typ != "string" {
		existing.Type = typ
		bp.Spec.Environment[envKey] = existing
	}
}

func resourceFromMap(m map[string]any, opts Options, placeholders []string, report *LossReport, nameMapping map[string]string, bp *blueprint.Blueprint) *blueprint.Resource {
	kind, _ := m["kind"].(string)
	if kind == "" {
		return nil
	}
	apiVersion, _ := m["apiVersion"].(string)

	name := extractResourceName(m, kind, placeholders)
	normName := normalizeDNSLabel(name)
	if normName != name && nameMapping != nil {
		nameMapping[name] = normName
	}

	provider := inferProvider(apiVersion, kind, opts.DefaultProviderRef, opts.CacheDir, bp)

	res := &blueprint.Resource{
		Name:        normName,
		Kind:        kind,
		Provider:    provider,
		Fields:      make(map[string]blueprint.Field),
		Annotations: make(map[string]blueprint.Field),
		Envelope:    make(map[string]blueprint.Field),
	}

	meta, _ := m["metadata"].(map[string]any)
	// Extract annotations
	if meta != nil {
		if anns, ok := meta["annotations"].(map[string]any); ok {
			for k, v := range anns {
				rawK := unmaskString(fmt.Sprint(k), placeholders)
				rawStr := unmaskString(fmt.Sprint(v), placeholders)
				if k == "crossplane.io/composition-resource-name" || strings.Contains(rawK, "setResourceNameAnnotation") || strings.Contains(rawStr, "setResourceNameAnnotation") {
					continue
				}
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
				} else if key := matchEnvVar(rawStr); key != "" {
					if isValidParamIdentifier(key) {
						res.Annotations[k] = blueprint.Field{From: "env." + key}
						if bp != nil {
							ensureEnvDeclared(bp, key, "string")
						}
					} else {
						report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, k), "invalid environment reference")
					}
				} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 5 {
					srcRes := m[1]
					if srcRes == "" {
						srcRes = m[2]
					}
					if nameMapping != nil && nameMapping[srcRes] != "" {
						srcRes = nameMapping[srcRes]
					} else {
						srcRes = normalizeDNSLabel(srcRes)
					}
					targetKind := m[3]
					targetField := m[4]
					var fromPath string
					if strings.HasPrefix(targetKind, "status") {
						field := targetField
						if !strings.HasPrefix(field, "atProvider.") && (strings.HasSuffix(targetKind, "atProvider") || targetKind == "status.atProvider") {
							field = "atProvider." + field
						}
						fromPath = "resources." + srcRes + ".status." + field
					} else {
						fromPath = "resources." + srcRes + ".metadata." + targetField
					}
					res.Annotations[k] = blueprint.Field{From: fromPath}
				} else if m := reXRResourceRef.FindStringSubmatch(rawStr); len(m) >= 2 {
					srcRes := m[1]
					if nameMapping != nil && nameMapping[srcRes] != "" {
						srcRes = nameMapping[srcRes]
					} else {
						srcRes = normalizeDNSLabel(srcRes)
					}
					res.Annotations[k] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
				} else {
					res.Annotations[k] = blueprint.Field{Value: rawStr}
				}
			}
		}
	}

	// Extract other top-level fields (e.g. data in ConfigMap, automountServiceAccountToken in ServiceAccount)
	for k, v := range m {
		if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
			continue
		}
		if mapVal, ok := v.(map[string]any); ok {
			extractFields(k, mapVal, res.Fields, placeholders, res.Name, report, nameMapping, bp)
		} else {
			rawStr := unmaskString(fmt.Sprint(v), placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				report.Record(fmt.Sprintf("resource.%s.fields.%s", res.Name, k), "contains newlines or control characters")
				continue
			}
			res.Fields[k] = blueprint.Field{Value: rawStr}
		}
	}

	// Extract spec fields
	if spec, ok := m["spec"].(map[string]any); ok {
		if forProvider, ok := spec["forProvider"].(map[string]any); ok {
			extractFields("", forProvider, res.Fields, placeholders, res.Name, report, nameMapping, bp)
			for k, v := range spec {
				if k == "forProvider" || k == "initProvider" {
					continue
				}
				if k == "providerConfigRef" {
					if pcrMap, ok := v.(map[string]any); ok {
						pcrName, _ := pcrMap["name"].(string)
						pcrKind, _ := pcrMap["kind"].(string)
						pcrNameUnmasked := unmaskString(pcrName, placeholders)
						if (pcrNameUnmasked == "{{ $spec.providerName }}" || pcrNameUnmasked == "{{ .spec.providerName }}") &&
							(pcrKind == "ClusterProviderConfig" || pcrKind == "") {
							continue
						}
					}
				}
				extractFields("", map[string]any{k: v}, res.Envelope, placeholders, res.Name, report, nameMapping, bp)
			}
		} else {
			extractFields("spec", spec, res.Fields, placeholders, res.Name, report, nameMapping, bp)
		}
	}

	return res
}

func isMapFieldPrefix(prefix, nextKey string) bool {
	if prefix == "data" || prefix == "stringData" || prefix == "binaryData" || prefix == "tags" {
		return true
	}
	if prefix == "spec.selector" {
		if nextKey == "matchLabels" || nextKey == "matchExpressions" {
			return false
		}
		return true
	}
	if strings.HasSuffix(prefix, "Labels") || strings.HasSuffix(prefix, "labels") || strings.HasSuffix(prefix, "annotations") || strings.HasSuffix(prefix, "tags") || strings.HasSuffix(prefix, "matchLabels") || strings.HasSuffix(prefix, "nodeSelector") {
		return true
	}
	return false
}

func extractFields(prefix string, obj map[string]any, out map[string]blueprint.Field, placeholders []string, resName string, report *LossReport, nameMapping map[string]string, bp *blueprint.Blueprint) {
	for k, v := range obj {
		path := k
		if prefix != "" {
			if isMapFieldPrefix(prefix, k) {
				path = fmt.Sprintf("%s[%s]", prefix, k)
			} else {
				path = prefix + "." + k
			}
		}
		switch val := v.(type) {
		case map[string]any:
			extractFields(path, val, out, placeholders, resName, report, nameMapping, bp)
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
			} else if key := matchEnvVar(rawStr); key != "" {
				if isValidParamIdentifier(key) {
					out[path] = blueprint.Field{From: "env." + key}
					if bp != nil {
						ensureEnvDeclared(bp, key, "string")
					}
				} else {
					report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "invalid environment reference")
				}
			} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 5 {
				srcRes := m[1]
				if srcRes == "" {
					srcRes = m[2]
				}
				if nameMapping != nil && nameMapping[srcRes] != "" {
					srcRes = nameMapping[srcRes]
				} else {
					srcRes = normalizeDNSLabel(srcRes)
				}
				targetKind := m[3]
				targetField := m[4]
				out[path] = blueprint.Field{From: "resources." + srcRes + "." + targetKind + "." + targetField}
			} else if m := reXRResourceRef.FindStringSubmatch(rawStr); len(m) >= 2 {
				srcRes := m[1]
				if nameMapping != nil && nameMapping[srcRes] != "" {
					srcRes = nameMapping[srcRes]
				} else {
					srcRes = normalizeDNSLabel(srcRes)
				}
				out[path] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
			} else if strings.Contains(rawStr, "{{") {
				out[path] = blueprint.Field{Raw: rawStr}
			} else {
				out[path] = blueprint.Field{Value: rawStr}
			}
		case []any:
			for elemIdx, item := range val {
				elemPath := fmt.Sprintf("%s[%d]", path, elemIdx)
				switch elemVal := item.(type) {
				case map[string]any:
					extractFields(elemPath, elemVal, out, placeholders, resName, report, nameMapping, bp)
				case string:
					rawStr := unmaskString(elemVal, placeholders)
					if err := checkScalarClean(rawStr); err != nil {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath),
							"multi-line scalar contains newlines, which is not supported in blueprint values")
						continue
					}
					if m := reParamVar.FindStringSubmatch(rawStr); len(m) >= 2 {
						if isValidParamIdentifier(m[1]) {
							out[elemPath] = blueprint.Field{From: "params." + m[1]}
						} else {
							report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath), "invalid parameter reference")
						}
					} else if key := matchEnvVar(rawStr); key != "" {
						if isValidParamIdentifier(key) {
							out[elemPath] = blueprint.Field{From: "env." + key}
							if bp != nil {
								ensureEnvDeclared(bp, key, "string")
							}
						} else {
							report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath), "invalid environment reference")
						}
					} else if m := reObservedStatus.FindStringSubmatch(rawStr); len(m) >= 5 {
						srcRes := m[1]
						if srcRes == "" {
							srcRes = m[2]
						}
						if nameMapping != nil && nameMapping[srcRes] != "" {
							srcRes = nameMapping[srcRes]
						} else {
							srcRes = normalizeDNSLabel(srcRes)
						}
						targetKind := m[3]
						targetField := m[4]
						out[elemPath] = blueprint.Field{From: "resources." + srcRes + "." + targetKind + "." + targetField}
					} else if m := reXRResourceRef.FindStringSubmatch(rawStr); len(m) >= 2 {
						srcRes := m[1]
						if nameMapping != nil && nameMapping[srcRes] != "" {
							srcRes = nameMapping[srcRes]
						} else {
							srcRes = normalizeDNSLabel(srcRes)
						}
						out[elemPath] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
					} else if strings.Contains(rawStr, "{{") {
						out[elemPath] = blueprint.Field{Raw: rawStr}
					} else {
						out[elemPath] = blueprint.Field{Value: rawStr}
					}
				default:
					rawStr := unmaskString(fmt.Sprint(elemVal), placeholders)
					if err := checkScalarClean(rawStr); err != nil {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath),
							"contains newlines or control characters")
						continue
					}
					out[elemPath] = blueprint.Field{Value: rawStr}
				}
			}
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

var rePlaceholder = regexp.MustCompile(`__CF_EXPR_(\d+)__`)

func unmaskString(s string, placeholders []string) string {
	if len(placeholders) == 0 {
		return s
	}
	return rePlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		var idx int
		if n, _ := fmt.Sscanf(m, "__CF_EXPR_%d__", &idx); n == 1 && idx >= 0 && idx < len(placeholders) {
			return placeholders[idx]
		}
		return m
	})
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
		for eName, e := range r.Envelope {
			if e.From != "" {
				e.From = rewriteFromWire(e.From, nameMapping)
				r.Envelope[eName] = e
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
	if len(parts) >= 3 && (parts[1] == "status" || parts[1] == "metadata") {
		origName := parts[0]
		if newName, ok := nameMapping[origName]; ok {
			return fmt.Sprintf("resources.%s.%s.%s", newName, parts[1], parts[2])
		}
	}
	return wire
}

func collectSources(bp *blueprint.Blueprint, defaultRef string) {
	seen := make(map[string]bool)
	var newSources []blueprint.Source
	for _, s := range bp.Spec.Sources {
		if s.Provider != "" {
			if !seen[s.Provider] {
				seen[s.Provider] = true
				newSources = append(newSources, s)
			}
		} else if s.CRDs != "" {
			newSources = append(newSources, s)
		}
	}
	for _, r := range bp.Spec.Resources {
		if r.Provider == "" || r.Provider == blueprint.NativeProvider {
			continue
		}
		if !seen[r.Provider] {
			seen[r.Provider] = true
			newSources = append(newSources, blueprint.Source{
				Provider: r.Provider,
			})
		}
	}
	if len(newSources) == 0 && defaultRef != "" {
		newSources = append(newSources, blueprint.Source{
			Provider: defaultRef,
		})
	}
	bp.Spec.Sources = newSources
}
