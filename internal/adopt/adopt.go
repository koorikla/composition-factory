// Package adopt ingests existing Crossplane Composition (and optional XRD)
// YAML manifests into a structured, round-trippable Blueprint document.
package adopt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/catalogue"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Options configures the adoption parser.
type Options struct {
	// DefaultProviderRef is used when resource provider sources cannot be
	// automatically inferred from the CRD group.
	DefaultProviderRef string
	// CacheDir is the schema cache directory used for schema lookups.
	CacheDir string
	// Store is an optional shared schema store. If nil and CacheDir is set,
	// a Store will be initialized once and reused across adoption passes.
	Store *cache.Store
	// FunctionPackages maps function names to pinned package references.
	FunctionPackages map[string]string
	// BaseBlueprint is the optional pre-existing blueprint being replaced or updated.
	BaseBlueprint *blueprint.Blueprint
	// TargetComposition specifies which Composition to adopt when multiple are present.
	TargetComposition string
	// CompositionName is an alias for TargetComposition.
	CompositionName string
	// SourceDir is the root directory or directory of the source manifest for resolving relative paths like fileSystem templates.
	SourceDir string
}

func (o Options) targetComposition() string {
	if o.CompositionName != "" {
		return o.CompositionName
	}
	return o.TargetComposition
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

// Lossy returns true if any fields or actions were dropped.
func (r *LossReport) Lossy() bool {
	return r.IsLossy()
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
				if k == "conventions" || k == "pipeline" || k == "enum" || k == "environmentConfigs" {
					continue
				}
			}
			if childMap, ok := child.(map[string]any); ok && len(childMap) == 0 {
				if k == "templates" || k == "envelope" || k == "annotations" || k == "properties" || k == "environment" ||
					k == "data" || k == "values" || k == "matchLabels" || k == "selector" {
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

	if opts.Store == nil && opts.CacheDir != "" {
		opts.Store = cache.New(opts.CacheDir)
	}

	var compDocs []map[string]any
	var xrdDocs []map[string]any
	var envConfigDocs []map[string]any
	var configDocs []map[string]any

	for _, d := range docs {
		kind, _ := d["kind"].(string)
		switch kind {
		case "Composition":
			compDocs = append(compDocs, d)
		case "CompositeResourceDefinition":
			xrdDocs = append(xrdDocs, d)
		case "EnvironmentConfig":
			envConfigDocs = append(envConfigDocs, d)
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
			configDocs = append(configDocs, d)
			if cSpec, ok := d["spec"].(map[string]any); ok {
				if deps, ok := cSpec["dependsOn"].([]any); ok {
					for _, depRaw := range deps {
						if dep, ok := depRaw.(map[string]any); ok {
							depKind, _ := dep["kind"].(string)
							pkg, _ := dep["package"].(string)
							if pkg == "" {
								if p, ok := dep["provider"].(string); ok && p != "" {
									pkg = p
									depKind = "Provider"
								}
							}
							if pkg == "" {
								if f, ok := dep["function"].(string); ok && f != "" {
									pkg = f
									depKind = "Function"
								}
							}
							if depKind == "" {
								last := pkg
								if i := strings.LastIndex(last, "/"); i >= 0 {
									last = last[i+1:]
								}
								if strings.HasPrefix(last, "function-") {
									depKind = "Function"
								} else if strings.HasPrefix(last, "provider-") {
									depKind = "Provider"
								}
							}
							ver, _ := dep["version"].(string)
							if depKind == "Function" || (depKind == "" && dep["function"] != nil) {
								fnName, _ := dep["function"].(string)
								if fnName == "" {
									fnName = pkg
								}
								fnPkg := pkg
								if fnPkg == "" {
									fnPkg = fnName
								}
								cleanVer := cleanDependencyVersion(ver)
								if cleanVer != "" && !strings.Contains(fnPkg, ":") && !strings.Contains(fnPkg, "@") {
									fnPkg = fnPkg + ":" + cleanVer
								}
								if fnName != "" && fnPkg != "" {
									opts.FunctionPackages[fnName] = fnPkg
									clean := fnName
									if i := strings.LastIndex(clean, "/"); i >= 0 {
										clean = clean[i+1:]
									}
									opts.FunctionPackages[clean] = fnPkg
								}
							}
						}
					}
				}
			}
		default:
			if len(d) == 0 {
				continue
			}
			name := ""
			if meta, ok := d["metadata"].(map[string]any); ok {
				name, _ = meta["name"].(string)
			}
			target := "manifest"
			if kind != "" && name != "" {
				target = fmt.Sprintf("manifest.%s/%s", kind, name)
			} else if kind != "" {
				target = fmt.Sprintf("manifest.%s", kind)
			} else if name != "" {
				target = fmt.Sprintf("manifest/%s", name)
			}
			report.Record(target, "unhandled resource kind omitted from blueprint adoption")
		}
	}

	if len(compDocs) == 0 {
		if len(xrdDocs) > 0 && opts.BaseBlueprint != nil {
			if _, ok := xrdDocs[0]["spec"].(map[string]any); ok {
				return adoptXRDComplement(xrdDocs[0], opts, report)
			}
		}
		return nil, nil, fmt.Errorf("no Composition document found in manifest")
	}

	var compNames []string
	seenNames := make(map[string]bool)
	for _, cd := range compDocs {
		name := ""
		if meta, ok := cd["metadata"].(map[string]any); ok {
			name, _ = meta["name"].(string)
		}
		if name == "" {
			name = "<unnamed>"
		}
		if !seenNames[name] {
			seenNames[name] = true
			compNames = append(compNames, name)
		}
	}
	sort.Strings(compNames)

	var compDoc map[string]any
	targetComp := opts.targetComposition()
	if targetComp != "" {
		selectedIdx := -1
		for i, cd := range compDocs {
			name := ""
			if meta, ok := cd["metadata"].(map[string]any); ok {
				name, _ = meta["name"].(string)
			}
			if name == targetComp {
				selectedIdx = i
				compDoc = cd
				break
			}
		}
		if selectedIdx == -1 {
			return nil, nil, fmt.Errorf("composition %q not found in manifest (available: %s)", targetComp, strings.Join(compNames, ", "))
		}
		for i, cd := range compDocs {
			if i == selectedIdx {
				continue
			}
			name := ""
			if meta, ok := cd["metadata"].(map[string]any); ok {
				name, _ = meta["name"].(string)
			}
			target := "manifest.Composition"
			if name != "" && name != "<unnamed>" {
				target = fmt.Sprintf("manifest.Composition/%s", name)
			}
			report.Record(target, "unselected composition omitted from blueprint adoption")
		}
	} else {
		if len(compDocs) > 1 {
			return nil, nil, fmt.Errorf("ambiguous multi-composition input: found %d Compositions in manifest (%s); specify a target composition to adopt", len(compDocs), strings.Join(compNames, ", "))
		}
		compDoc = compDocs[0]
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
	if opts.BaseBlueprint != nil {
		bp.Spec.Sources = append(bp.Spec.Sources, opts.BaseBlueprint.Spec.Sources...)
	}

	// 1. Metadata
	if m := srcCommentRE.FindSubmatch(manifest); len(m) >= 2 && string(m[1]) != "blueprint" {
		candidate := string(m[1])
		if isValidMetadataName(candidate) {
			bp.Metadata.Name = candidate
		}
	}
	for _, cfgDoc := range configDocs {
		if meta, ok := cfgDoc["metadata"].(map[string]any); ok {
			if name, ok := meta["name"].(string); ok && name != "" {
				if bp.Metadata.Name == "" {
					bp.Metadata.Name = name
				}
			}
		}
		if spec, ok := cfgDoc["spec"].(map[string]any); ok {
			if dependsOn, ok := spec["dependsOn"].([]any); ok {
				for _, depRaw := range dependsOn {
					dep, ok := depRaw.(map[string]any)
					if !ok {
						continue
					}
					depKind, _ := dep["kind"].(string)
					pkg, _ := dep["package"].(string)
					if pkg == "" {
						if p, ok := dep["provider"].(string); ok && p != "" {
							pkg = p
							depKind = "Provider"
						}
					}
					if depKind == "" {
						last := pkg
						if i := strings.LastIndex(last, "/"); i >= 0 {
							last = last[i+1:]
						}
						if strings.HasPrefix(last, "function-") {
							depKind = "Function"
						} else if strings.HasPrefix(last, "provider-") {
							depKind = "Provider"
						}
					}
					ver, _ := dep["version"].(string)
					if depKind == "Provider" || (depKind == "" && pkg != "" && dep["function"] == nil) {
						providerRef := pkg
						if ver != "" && !strings.Contains(providerRef, ":") && !strings.Contains(providerRef, "@") {
							cleanVer := cleanDependencyVersion(ver)
							if cleanVer != "" {
								providerRef = pkg + ":" + cleanVer
							}
						}
						found := false
						for _, s := range bp.Spec.Sources {
							if s.Provider == providerRef {
								found = true
								break
							}
						}
						if !found && providerRef != "" {
							bp.Spec.Sources = append(bp.Spec.Sources, blueprint.Source{
								Provider: providerRef,
							})
						}
					}
				}
			}
		}
	}
	if meta, ok := compDoc["metadata"].(map[string]any); ok {
		if name, ok := meta["name"].(string); ok && name != "" {
			if targetComp != "" || bp.Metadata.Name == "" {
				bp.Metadata.Name = name
			}
		}
		if anns, ok := meta["annotations"].(map[string]any); ok {
			if envConfigsRaw, ok := anns[blueprint.EnvironmentConfigsAnnotation].(string); ok && envConfigsRaw != "" {
				var envConfigs []blueprint.EnvironmentConfig
				if err := json.Unmarshal([]byte(envConfigsRaw), &envConfigs); err == nil && len(envConfigs) > 0 {
					bp.Spec.EnvironmentConfigs = envConfigs
				}
			}
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

	checkCompositionSpecFields(spec, report)

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
		if p, ok := ctr["plural"].(string); ok && p != "" {
			bp.Spec.XRD.Plural = p
		}
	}

	// 3. Match CompositeResourceDefinition documents against the Composition's kind
	var xrdDoc map[string]any
	var unmatchedXRDs []map[string]any
	for _, xd := range xrdDocs {
		var xrdKind string
		if xSpec, ok := xd["spec"].(map[string]any); ok {
			if names, ok := xSpec["names"].(map[string]any); ok {
				xrdKind, _ = names["kind"].(string)
			}
		}
		if xrdDoc == nil && xrdKind != "" && bp.Spec.XRD.Kind != "" && (xrdKind == bp.Spec.XRD.Kind || strings.EqualFold(xrdKind, bp.Spec.XRD.Kind)) {
			xrdDoc = xd
		} else {
			unmatchedXRDs = append(unmatchedXRDs, xd)
		}
	}
	for _, xd := range unmatchedXRDs {
		name := ""
		if meta, ok := xd["metadata"].(map[string]any); ok {
			name, _ = meta["name"].(string)
		}
		target := "manifest.CompositeResourceDefinition"
		if name != "" {
			target = fmt.Sprintf("manifest.CompositeResourceDefinition/%s", name)
		}
		report.Record(target, "unmatched XRD omitted from blueprint adoption")
	}
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
	resolveXRDPlural(bp)
	if bp.Spec.XRD.Scope == "" {
		bp.Spec.XRD.Scope = "Namespaced"
	}
	synthesized := map[string]bool{}

	// 4. Process EnvironmentConfig documents
	parseEnvironmentConfigDocs(envConfigDocs, bp, report)

	// 4.5. Process Composition spec.environment
	if envMap, ok := spec["environment"].(map[string]any); ok {
		if envConfigs, ok := envMap["environmentConfigs"].([]any); ok && len(bp.Spec.EnvironmentConfigs) == 0 {
			var extracted []blueprint.EnvironmentConfig
			for _, item := range envConfigs {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				var cfg blueprint.EnvironmentConfig
				if selMap, ok := itemMap["selector"].(map[string]any); ok {
					if mlMap, ok := selMap["matchLabels"].(map[string]any); ok {
						ml := make(map[string]string)
						for k, v := range mlMap {
							if s, ok := v.(string); ok {
								ml[k] = s
							}
						}
						if len(ml) > 0 {
							cfg.Selector = &blueprint.EnvironmentConfigSelector{
								MatchLabels: ml,
							}
						}
					}
				}
				if refMap, ok := itemMap["ref"].(map[string]any); ok {
					if name, ok := refMap["name"].(string); ok && name != "" {
						cfg.Name = name
					}
				}
				if cfg.Name != "" || cfg.Selector != nil {
					extracted = append(extracted, cfg)
				}
			}
			if len(extracted) > 0 {
				bp.Spec.EnvironmentConfigs = extracted
			}
		}
	}

	// 5. Parse Pipeline or Classic Resources
	nameMapping := make(map[string]string)
	if pipeline, ok := spec["pipeline"].([]any); ok && len(pipeline) > 0 {
		if err := parsePipelineComposition(pipeline, bp, opts, report, nameMapping, xrdDoc != nil); err != nil {
			return nil, nil, err
		}
	} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
		patchSets, _ := spec["patchSets"].([]any)
		if err := parseClassicComposition(resources, patchSets, bp, opts, report, nameMapping); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("composition has neither spec.pipeline nor spec.resources")
	}

	// Rewrite status references with normalized names
	rewriteStatusReferences(bp, nameMapping)

	if bp.Spec.XRD.Scope == "Namespaced" && bp.HasManagedResources() {
		if xrdDoc == nil {
			bp.Spec.XRD.Parameters["providerName"] = blueprint.Parameter{
				Type:        "string",
				Required:    true,
				Description: "Crossplane ProviderConfig name to use for managed resources",
			}
			synthesized["providerName"] = true
		} else if _, ok := bp.Spec.XRD.Parameters["providerName"]; !ok {
			bp.Spec.XRD.Parameters["providerName"] = blueprint.Parameter{
				Type:        "string",
				Required:    true,
				Description: "Crossplane ProviderConfig name to use for managed resources",
			}
			synthesized["providerName"] = true
		}
	}

	// 6. Deduplicate and collect provider sources
	collectSources(bp, opts.DefaultProviderRef)

	// Prune unknown forProvider fields against CRD schema if store is available
	pruneUnknownForProviderFields(bp, opts, report)

	if xrdDoc == nil {
		pruneOrphanedParameters(bp, opts.BaseBlueprint, report)
	}

	// No XRD alongside: the parameters above were inferred from their uses.
	// Settle what the Composition proves and name the rest as lost.
	if xrdDoc == nil {
		applyXRDlessEvidence(bp, []map[string]any{compDoc}, synthesized, report, opts.BaseBlueprint, opts.Store)
	}

	// Ensure EnvironmentConfigs is not left declared without any environment keys
	if len(bp.Spec.Environment) == 0 && len(bp.Spec.EnvironmentConfigs) > 0 {
		for _, cfg := range bp.Spec.EnvironmentConfigs {
			report.Record(fmt.Sprintf("environmentConfig.%s", cfg.Name), "EnvironmentConfig declared without any environment keys")
		}
		bp.Spec.EnvironmentConfigs = nil
	}

	if err := bp.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return bp, report, nil
}

func adoptXRDComplement(xrdDoc map[string]any, opts Options, report *LossReport) (*blueprint.Blueprint, *LossReport, error) {
	if opts.BaseBlueprint == nil {
		return nil, nil, fmt.Errorf("no Composition document found in manifest (supply Composition and XRD together in one file or select both to adopt)")
	}

	spec, ok := xrdDoc["spec"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("XRD document has no spec")
	}

	names, _ := spec["names"].(map[string]any)
	xrdKind, _ := names["kind"].(string)
	if opts.BaseBlueprint.Spec.XRD.Kind != "" && xrdKind != "" && !strings.EqualFold(opts.BaseBlueprint.Spec.XRD.Kind, xrdKind) {
		return nil, nil, fmt.Errorf("no Composition document found in manifest")
	}

	bpData, err := json.Marshal(opts.BaseBlueprint)
	if err != nil {
		return nil, nil, fmt.Errorf("clone base blueprint: %w", err)
	}
	var bp blueprint.Blueprint
	if err := json.Unmarshal(bpData, &bp); err != nil {
		return nil, nil, fmt.Errorf("clone base blueprint: %w", err)
	}

	if bp.Spec.XRD.Parameters == nil {
		bp.Spec.XRD.Parameters = make(map[string]blueprint.Parameter)
	}

	if report == nil {
		report = &LossReport{}
	}

	if group, ok := spec["group"].(string); ok && group != "" {
		bp.Spec.XRD.Group = group
	}
	if xrdKind != "" {
		bp.Spec.XRD.Kind = xrdKind
	}
	if plural, ok := names["plural"].(string); ok && plural != "" {
		bp.Spec.XRD.Plural = plural
	}
	if scope, ok := spec["scope"].(string); ok && scope != "" {
		bp.Spec.XRD.Scope = scope
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
			if vName, ok := matchedVersion["name"].(string); ok && vName != "" {
				bp.Spec.XRD.Version = vName
			}
		}
	}

	parseXRDDoc(xrdDoc, &bp, report)

	resolveXRDPlural(&bp)
	if bp.Spec.XRD.Scope == "" {
		bp.Spec.XRD.Scope = "Namespaced"
	}

	if err := bp.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return &bp, report, nil
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

// checkCompositionSpecFields inspects Composition.spec for unsupported fields and records them in report.
func checkCompositionSpecFields(spec map[string]any, report *LossReport) {
	if report == nil || spec == nil {
		return
	}
	specKeys := make([]string, 0, len(spec))
	for k := range spec {
		specKeys = append(specKeys, k)
	}
	sort.Strings(specKeys)
	for _, k := range specKeys {
		if k == "compositeTypeRef" || k == "mode" || k == "pipeline" || k == "resources" || k == "patchSets" {
			continue
		}
		path := fmt.Sprintf("spec.%s", k)
		alreadyRecorded := false
		for _, d := range report.Drops {
			if d.Path == path {
				alreadyRecorded = true
				break
			}
		}
		if !alreadyRecorded {
			report.Record(path, fmt.Sprintf("%s is not supported in blueprint", k))
		}
	}
}

// parseEnvironmentConfigDocs ingests EnvironmentConfig documents into bp.Spec.Environment
// and bp.Spec.EnvironmentConfigs, recording any dropped or unconvertible fields in report.
func parseEnvironmentConfigDocs(envConfigDocs []map[string]any, bp *blueprint.Blueprint, report *LossReport) {
	for _, envDoc := range envConfigDocs {
		meta, _ := envDoc["metadata"].(map[string]any)
		cfgName, _ := meta["name"].(string)
		labels, _ := meta["labels"].(map[string]any)

		prefix := "environmentConfig"
		if cfgName != "" {
			prefix = fmt.Sprintf("environmentConfig.%s", cfgName)
		} else {
			report.Record("environmentConfig.metadata.name", "missing name in EnvironmentConfig metadata")
		}

		// 1. Check unsupported top-level fields
		topKeys := make([]string, 0, len(envDoc))
		for k := range envDoc {
			topKeys = append(topKeys, k)
		}
		sort.Strings(topKeys)
		for _, k := range topKeys {
			if k == "apiVersion" || k == "kind" || k == "metadata" || k == "data" {
				continue
			}
			report.Record(fmt.Sprintf("%s.%s", prefix, k), fmt.Sprintf("%s is not supported in EnvironmentConfig", k))
		}

		// 2. Check metadata fields
		if meta != nil {
			metaKeys := make([]string, 0, len(meta))
			for k := range meta {
				metaKeys = append(metaKeys, k)
			}
			sort.Strings(metaKeys)
			for _, k := range metaKeys {
				if k == "name" || k == "labels" {
					continue
				}
				if k == "annotations" {
					if anns, ok := meta["annotations"].(map[string]any); ok && len(anns) > 0 {
						annKeys := make([]string, 0, len(anns))
						for ak := range anns {
							annKeys = append(annKeys, ak)
						}
						sort.Strings(annKeys)
						for _, ak := range annKeys {
							report.Record(fmt.Sprintf("%s.metadata.annotations[%s]", prefix, ak), "annotations on EnvironmentConfig are not supported in blueprint")
						}
					}
					continue
				}
				report.Record(fmt.Sprintf("%s.metadata.%s", prefix, k), fmt.Sprintf("metadata.%s on EnvironmentConfig is not supported in blueprint", k))
			}
		}

		// 3. Process data
		validData := make(map[string]string)
		if rawData, hasData := envDoc["data"]; hasData && rawData != nil {
			data, isMap := rawData.(map[string]any)
			if !isMap {
				report.Record(fmt.Sprintf("%s.data", prefix), "data must be a map")
			} else if len(data) > 0 {
				dataKeys := make([]string, 0, len(data))
				for k := range data {
					dataKeys = append(dataKeys, k)
				}
				sort.Strings(dataKeys)

				for _, k := range dataKeys {
					v := data[k]
					if !paramNameRE.MatchString(k) || yamlKeywords[strings.ToLower(k)] {
						report.Record(fmt.Sprintf("%s.data.%s", prefix, k), "invalid environment key name (must be camelCase and not a YAML keyword)")
						continue
					}

					switch v.(type) {
					case map[string]any, []any:
						report.Record(fmt.Sprintf("%s.data.%s", prefix, k), "non-scalar environment data is not supported in blueprint")
						continue
					}

					var strVal string
					inferredType := "string"
					switch val := v.(type) {
					case bool:
						inferredType = "boolean"
						if val {
							strVal = "true"
						} else {
							strVal = "false"
						}
					case int:
						inferredType = "integer"
						strVal = strconv.Itoa(val)
					case int8:
						inferredType = "integer"
						strVal = strconv.FormatInt(int64(val), 10)
					case int16:
						inferredType = "integer"
						strVal = strconv.FormatInt(int64(val), 10)
					case int32:
						inferredType = "integer"
						strVal = strconv.FormatInt(int64(val), 10)
					case int64:
						inferredType = "integer"
						strVal = strconv.FormatInt(val, 10)
					case uint:
						inferredType = "integer"
						strVal = strconv.FormatUint(uint64(val), 10)
					case uint8:
						inferredType = "integer"
						strVal = strconv.FormatUint(uint64(val), 10)
					case uint16:
						inferredType = "integer"
						strVal = strconv.FormatUint(uint64(val), 10)
					case uint32:
						inferredType = "integer"
						strVal = strconv.FormatUint(uint64(val), 10)
					case uint64:
						inferredType = "integer"
						strVal = strconv.FormatUint(val, 10)
					case float32:
						if float64(val) == float64(int64(val)) {
							inferredType = "integer"
							strVal = strconv.FormatInt(int64(val), 10)
						} else {
							inferredType = "number"
							strVal = strconv.FormatFloat(float64(val), 'f', -1, 32)
						}
					case float64:
						if val == float64(int64(val)) {
							inferredType = "integer"
							strVal = strconv.FormatInt(int64(val), 10)
						} else {
							inferredType = "number"
							strVal = strconv.FormatFloat(val, 'f', -1, 64)
						}
					case string:
						inferredType = "string"
						strVal = val
					case nil:
						inferredType = "string"
						strVal = ""
					default:
						inferredType = "string"
						strVal = formatScalarValue(val)
					}

					ensureEnvDeclared(bp, k, inferredType)
					validData[k] = strVal
				}
			}
		}

		// 4. Update or append bp.Spec.EnvironmentConfigs
		if cfgName != "" {
			var targetCfg *blueprint.EnvironmentConfig
			for i := range bp.Spec.EnvironmentConfigs {
				if bp.Spec.EnvironmentConfigs[i].Name == cfgName {
					targetCfg = &bp.Spec.EnvironmentConfigs[i]
					break
				}
			}
			if targetCfg == nil {
				var sel *blueprint.EnvironmentConfigSelector
				if len(labels) > 0 {
					labelKeys := make([]string, 0, len(labels))
					for lk := range labels {
						labelKeys = append(labelKeys, lk)
					}
					sort.Strings(labelKeys)
					matchLabels := make(map[string]string, len(labels))
					for _, lk := range labelKeys {
						matchLabels[lk] = formatScalarValue(labels[lk])
					}
					sel = &blueprint.EnvironmentConfigSelector{MatchLabels: matchLabels}
				}
				bp.Spec.EnvironmentConfigs = append(bp.Spec.EnvironmentConfigs, blueprint.EnvironmentConfig{
					Name:     cfgName,
					Selector: sel,
				})
				targetCfg = &bp.Spec.EnvironmentConfigs[len(bp.Spec.EnvironmentConfigs)-1]
			} else if targetCfg.Selector == nil && len(labels) > 0 {
				labelKeys := make([]string, 0, len(labels))
				for lk := range labels {
					labelKeys = append(labelKeys, lk)
				}
				sort.Strings(labelKeys)
				matchLabels := make(map[string]string, len(labels))
				for _, lk := range labelKeys {
					matchLabels[lk] = formatScalarValue(labels[lk])
				}
				targetCfg.Selector = &blueprint.EnvironmentConfigSelector{MatchLabels: matchLabels}
			}

			if len(validData) > 0 {
				if targetCfg.Data == nil {
					targetCfg.Data = make(map[string]string, len(validData))
				}
				for k, v := range validData {
					targetCfg.Data[k] = v
				}
			}
		}
	}
}

func parseXRDDoc(xrdDoc map[string]any, bp *blueprint.Blueprint, report *LossReport) {
	spec, ok := xrdDoc["spec"].(map[string]any)
	if !ok {
		return
	}
	if names, ok := spec["names"].(map[string]any); ok {
		if k, ok := names["kind"].(string); ok && k != "" {
			if bp.Spec.XRD.Kind != "" && !strings.EqualFold(k, bp.Spec.XRD.Kind) {
				return
			}
			if bp.Spec.XRD.Kind == "" {
				bp.Spec.XRD.Kind = k
			}
		}
		if p, ok := names["plural"].(string); ok && bp.Spec.XRD.Plural == "" {
			bp.Spec.XRD.Plural = p
		}
	}
	if group, ok := spec["group"].(string); ok && bp.Spec.XRD.Group == "" {
		bp.Spec.XRD.Group = group
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
			s := formatScalarValue(e)
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
		defStr = formatScalarValue(defVal)
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
	reParamVar           = regexp.MustCompile(`\{\{-?\s*\(?\s*(?:default\s+(?:\([^)]+\)|["'][^"']*["']|\S+)\s+)?\(?\s*(?:(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+?)|index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+?)["'])\s*\)?(?:\s*\|\s*default\s+(?:\([^)]+\)|["'][^"']*["']|\S+))?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*\)?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*-?\}\}`)
	reEvidenceIndexSpec  = regexp.MustCompile(`\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']`)
	reEnvVar             = regexp.MustCompile(`\{\{-?\s*\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*(?:\$env\.([a-zA-Z0-9_.-]+?)|\(index\s+\$env\s+["']([a-zA-Z0-9_.-]+?)["']\)|index\s+\$env\s+["']([a-zA-Z0-9_.-]+?)["'])\s*\)?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*\)?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*-?\}\}`)
	reObservedStatus     = regexp.MustCompile(`\{\{-?\s*\(?\s*(?:\(index\s+(?:\$?[.]?observed(?:\.resources)?|\$observed)\s+["']([^"']+)["']\)|(?:\$?[.]?observed(?:\.resources)?|\$observed)\.([a-zA-Z0-9_-]+)|\(+\s*getComposedResource\s+(?:(?:\([^)]+\)|[^\s"'\x60\)]+)\s+["'\x60]([^"'\x60]+)["'\x60]|["'\x60]([^"'\x60]+)["'\x60]\s+(?:\([^)]+\)|[^\s"'\x60\)]+))\s*\)+)(?:\.resource)?\.(status(?:\.atProvider)?|metadata)\.([a-zA-Z0-9_.-]+?)\s*\)?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*\)?(?:\s*\|\s*b64enc)?(?:\s*\|\s*quote)?\s*-?\}\}`)
	reXRResourceRef      = regexp.MustCompile(`\{\{-?\s*(?:\$xr|\$?[.]observed\.composite\.resource\.metadata\.name)\s*-?\}\}-([a-zA-Z0-9_-]+)`)
	reWhenIfSimple       = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?(?:(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)|\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\)*\s*-?\}\}`)
	reWhenIfBoolEq       = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?eq\s+(?:\(?\s*(?:default\s+false\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*(?:default\s+false\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\s+true\)*\s*-?\}\}`)
	reWhenIfBoolEqRev    = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?eq\s+true\s+(?:\(?\s*(?:default\s+false\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*(?:default\s+false\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\)*\s*-?\}\}`)
	reWhenIfBoolNe       = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?ne\s+(?:\(?\s*(?:default\s+false\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*(?:default\s+false\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\s+false\)*\s*-?\}\}`)
	reWhenIfBoolNeRev    = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?ne\s+false\s+(?:\(?\s*(?:default\s+false\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*(?:default\s+false\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\)*\s*-?\}\}`)
	reWhenIfDefault      = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?default\s+false\s+(?:\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\)*\s*-?\}\}`)
	reWhenIfEq           = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?eq\s+(?:(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?)|(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?))\s+["']([^"']*)["']\)*\s*-?\}\}`)
	reWhenIfNe           = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?ne\s+(?:(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?)|(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?))\s+["']([^"']*)["']\)*\s*-?\}\}`)
	reWhenIfEqRev        = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?eq\s+["']([^"']*)["']\s+(?:(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?)|(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?))\)*\s*-?\}\}`)
	reWhenIfNeRev        = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)|or\s+\(\s*not\s+\(\s*hasKey\s+(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s+["'][^"']+["']\s*\)\s*\))\s+)?\(?ne\s+["']([^"']*)["']\s+(?:(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?)|(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?))\)*\s*-?\}\}`)
	reWhenIfEnvSimple    = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?\$env\.([a-zA-Z0-9_.-]+)|(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*index\s+\$env\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?\s*\)?)))\)*\s*-?\}\}`)
	reWhenIfEnvEq        = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?\(?eq\s+\$env\.([a-zA-Z0-9_.-]+)\s+["']?([^"']*?)["']?\)?|(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?\(?eq\s+(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*index\s+\$env\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?\s*\)?)\s+["']?([^"']*?)["']?\)?))\)*\s*-?\}\}`)
	reWhenIfEnvNe        = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:or\s+\(not\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\)\s+)?\(?ne\s+\$env\.([a-zA-Z0-9_.-]+)\s+["']?([^"']*?)["']?\)?|(?:(?:or\s+\(not\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\)\s+)?\(?ne\s+(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*index\s+\$env\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?\s*\)?)\s+["']?([^"']*?)["']?\)?))\)*\s*-?\}\}`)
	reWhenIfEnvEqRev     = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?\(?eq\s+["']?([^"']*?)["']?\s+\$env\.([a-zA-Z0-9_.-]+)\)?|(?:(?:and\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\s+)?\(?eq\s+["']?([^"']*?)["']?\s+(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*index\s+\$env\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?\s*\)?)\)?))\)*\s*-?\}\}`)
	reWhenIfEnvNeRev     = regexp.MustCompile(`\{\{-?\s*if\s+\(?(?:(?:or\s+\(not\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\)\s+)?\(?ne\s+["']?([^"']*?)["']?\s+\$env\.([a-zA-Z0-9_.-]+)\)?|(?:(?:or\s+\(not\s+\(hasKey\s+\$env\s+["'][^"']+["']\)\)\s+)?\(?ne\s+["']?([^"']*?)["']?\s+(?:\(?\s*(?:default\s+(?:["'][^"']*["']|\S+)\s+)?\(?\s*index\s+\$env\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?\s*\)?)\)?))\)*\s*-?\}\}`)
	reForEachLoop        = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s*(?:\(?\s*default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\s*(?:\|\s*default\s+(?:["'][^"']*["']|\S+)\s*)?\)?\s*\)\s*-?\}\}`)
	reForEachDefault     = regexp.MustCompile(`(?:default\s+(?:["']([^"']*)["']|([^\s)]+))|\|\s*default\s+(?:["']([^"']*)["']|([^\s)]+)))`)
	reForEachEnvLoop     = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s*(?:\(?\s*default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\(?\s*\$env\.([a-zA-Z0-9_.-]+)\s*\)?|\(?\s*index\s+\(?\s*\$env\s*\)?\s+["']([a-zA-Z0-9_.-]+)["']\s*\)?)\s*(?:\|\s*default\s+(?:["'][^"']*["']|\S+)\s*)?\)?\s*\)\s*-?\}\}`)
	reForEachStatusLoop  = regexp.MustCompile(`\{\{-?\s*range\s+\$i\s*:=\s*until\s+\(int\s*(?:\(?\s*default\s+(?:["'][^"']*["']|\S+)\s+)?(?:\(*\s*index\s+\$?[.]?observed\.resources\s+["']([^"']+)["']\s*\)(?:\.resource)?\.status\.([a-zA-Z0-9_.-]+)|\(*\s*\$?[.]?observed\.resources\.([a-zA-Z0-9_-]+)(?:\.resource)?\.status\.([a-zA-Z0-9_.-]+)|\(*\s*\(+\s*getComposedResource\s+(?:(?:\([^)]+\)|[^\s"'\x60\)]+)\s+["'\x60]([^"'\x60]+)["'\x60]|["'\x60]([^"'\x60]+)["'\x60]\s+(?:\([^)]+\)|[^\s"'\x60\)]+))(?:\s*\))+\s*(?:\.resource)?\.status\.([a-zA-Z0-9_.-]+))\s*(?:\|\s*default\s+(?:["'][^"']*["']|\S+)\s*)?(?:\s*\))*\s*\)\s*-?\}\}`)
	reMustacheExpr       = regexp.MustCompile(`\{\{.*?\}\}`)
	reTemplateInclude    = regexp.MustCompile(`^\{\{-?\s*include\s+["']([^"']+)["'](?:\s+[^}]*)?-?\}\}$`)
	reDocSeparator       = regexp.MustCompile(`(?m)^---\s*$`)
	reSetResourceNameAnn = regexp.MustCompile(`setResourceNameAnnotation\s+(?:\(printf\s+["']([^"']+)["']|["']([^"']+)["'])`)
	reChunkResNameAnn    = regexp.MustCompile(`["']?(?:crossplane\.io|gotemplating\.fn\.crossplane\.io)/composition-resource-name["']?\s*:\s*["']?([a-zA-Z0-9._-]+)["']?`)
	reChunkKind          = regexp.MustCompile(`(?m)^\s*kind:\s*["']?([a-zA-Z0-9]+)["']?`)
	reChunkName          = regexp.MustCompile(`(?m)^\s*name:\s*["']?([a-zA-Z0-9._-]+)["']?`)
	rePrintfFormat       = regexp.MustCompile(`printf\s+["']([^"']+)["']`)
	reXRNameSuffix       = regexp.MustCompile(`\{\{-?\s*(?:\$xr|\$?[.]observed\.composite\.resource\.metadata\.name)\s*-?\}\}-([a-zA-Z0-9_-]+)`)
	srcCommentRE         = regexp.MustCompile(`(?m)^# Source:\s*([^\s]+)`)
	dnsSubdomainRE       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	paramNameRE          = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	pluralRE             = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	dnsInvalidRE         = regexp.MustCompile(`[^a-z0-9-]+`)
	reBracketQuoted      = regexp.MustCompile(`\[\s*['"](.*?)['"]\s*\]`)
	yamlKeywords         = map[string]bool{
		"true": true, "false": true, "null": true,
	}
)

func isValidMetadataName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	if !dnsSubdomainRE.MatchString(name) {
		return false
	}
	switch strings.ToLower(name) {
	case "true", "false", "yes", "no", "on", "off", "null", "y", "n":
		return false
	}
	return true
}

func matchTemplateInclude(s string) string {
	m := reTemplateInclude.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

func templateExists(bp *blueprint.Blueprint, name string) bool {
	if bp == nil || bp.Spec.Templates == nil {
		return false
	}
	_, ok := bp.Spec.Templates[name]
	return ok
}

func matchParamVar(s string) string {
	trimmed := strings.TrimSpace(s)
	if m := reParamVar.FindStringSubmatch(trimmed); len(m) > 1 && m[0] == trimmed {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				return m[i]
			}
		}
	}
	return ""
}

func matchEnvVar(s string) string {
	trimmed := strings.TrimSpace(s)
	if m := reEnvVar.FindStringSubmatch(trimmed); len(m) > 1 && m[0] == trimmed {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				return m[i]
			}
		}
	}
	return ""
}

func matchObservedStatus(s string) (srcRes, targetKind, targetField string, ok bool) {
	trimmed := strings.TrimSpace(s)
	m := reObservedStatus.FindStringSubmatch(trimmed)
	if len(m) < 7 || m[0] != trimmed {
		return "", "", "", false
	}
	for i := 1; i <= 4; i++ {
		if m[i] != "" {
			srcRes = m[i]
			break
		}
	}
	targetKind = m[5]
	targetField = m[6]
	return srcRes, targetKind, targetField, true
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

func isFlatParamIdentifier(name string) bool {
	return !strings.Contains(name, ".") && paramNameRE.MatchString(name) && !yamlKeywords[strings.ToLower(name)]
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

func inferProvider(apiVersion, kind string, defaultProvider string, store *cache.Store, bp *blueprint.Blueprint) string {
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
				if i := strings.Index(pkgName, "@"); i >= 0 {
					pkgName = pkgName[:i]
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
	if store != nil {
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

func extractCompositionResourceName(m map[string]any, placeholders []string) string {
	meta, _ := m["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	anns, ok := meta["annotations"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"crossplane.io/composition-resource-name", "gotemplating.fn.crossplane.io/composition-resource-name"} {
		if annName, ok := anns[key].(string); ok && annName != "" {
			unmasked := unmaskString(annName, placeholders)
			if clean := extractCleanName(unmasked); clean != "" {
				return clean
			}
			return unmasked
		}
	}
	for k, v := range anns {
		unmaskedK := unmaskString(fmt.Sprint(k), placeholders)
		unmaskedV := unmaskString(formatScalarValue(v), placeholders)
		if m := reSetResourceNameAnn.FindStringSubmatch(unmaskedK); len(m) >= 2 {
			candidate := m[1]
			if candidate == "" && len(m) >= 3 {
				candidate = m[2]
			}
			if clean := extractCleanName(candidate); clean != "" {
				return clean
			}
			if candidate != "" {
				return candidate
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
			if candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func extractResourceName(m map[string]any, kind string, placeholders []string) string {
	meta, _ := m["metadata"].(map[string]any)
	name := ""
	if meta != nil {
		if anns, ok := meta["annotations"].(map[string]any); ok {
			for _, key := range []string{"crossplane.io/composition-resource-name", "gotemplating.fn.crossplane.io/composition-resource-name"} {
				if annName, ok := anns[key].(string); ok && annName != "" {
					unmasked := unmaskString(annName, placeholders)
					if clean := extractCleanName(unmasked); clean != "" {
						return clean
					}
					if name == "" {
						name = annName
					}
				}
			}
			for k, v := range anns {
				unmaskedK := unmaskString(fmt.Sprint(k), placeholders)
				unmaskedV := unmaskString(formatScalarValue(v), placeholders)
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

// resolveXRDPlural determines the XRD plural name from composite metadata name,
// group suffix, or infers it from the kind.
func resolveXRDPlural(bp *blueprint.Blueprint) {
	if bp.Spec.XRD.Plural == "" {
		if bp.Spec.XRD.Group != "" && strings.HasSuffix(bp.Metadata.Name, "."+bp.Spec.XRD.Group) {
			candidate := strings.TrimSuffix(bp.Metadata.Name, "."+bp.Spec.XRD.Group)
			if pluralRE.MatchString(candidate) && !yamlKeywords[strings.ToLower(candidate)] {
				bp.Spec.XRD.Plural = candidate
			}
		}
		if bp.Spec.XRD.Plural == "" && strings.Contains(bp.Metadata.Name, ".") {
			candidate := bp.Metadata.Name[:strings.Index(bp.Metadata.Name, ".")]
			if pluralRE.MatchString(candidate) && !yamlKeywords[strings.ToLower(candidate)] {
				bp.Spec.XRD.Plural = candidate
			}
		}
	}
	if bp.Spec.XRD.Plural == "" {
		bp.Spec.XRD.Plural = inferPlural(bp.Spec.XRD.Kind)
	}
}

func inferPlural(kind string) string {
	lower := strings.ToLower(kind)
	if lower == "" {
		return ""
	}
	if strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") || strings.HasSuffix(lower, "z") ||
		strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "sh") {
		return lower + "es"
	}
	if strings.HasSuffix(lower, "y") && len(lower) > 1 {
		lastConsonant := lower[len(lower)-2]
		if lastConsonant != 'a' && lastConsonant != 'e' && lastConsonant != 'i' && lastConsonant != 'o' && lastConsonant != 'u' {
			return lower[:len(lower)-1] + "ies"
		}
	}
	return lower + "s"
}

func isDefaultMetadataName(rawName, resName, normName string) bool {
	rawName = strings.TrimSpace(rawName)
	if rawName == "" || rawName == resName || rawName == normName {
		return true
	}
	clean := extractCleanName(rawName)
	if clean == resName || clean == normName {
		return true
	}
	return false
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

func loadFileSystemTemplates(fsDir string, opts Options) (string, error) {
	var bases []string
	if opts.SourceDir != "" {
		bases = append(bases, opts.SourceDir)
		parent := filepath.Dir(opts.SourceDir)
		if parent != opts.SourceDir && parent != "." && parent != "/" {
			bases = append(bases, parent)
		}
	}
	bases = append(bases, ".")

	cleanDir := filepath.Clean(strings.TrimPrefix(fsDir, "/"))
	cleanDir = strings.TrimPrefix(cleanDir, "."+string(filepath.Separator))
	cleanDir = strings.TrimPrefix(cleanDir, "./")
	if cleanDir == "." || cleanDir == "/" {
		cleanDir = "templates"
	}

	var subpaths []string
	if cleanDir != "" {
		subpaths = append(subpaths, cleanDir)
		baseName := filepath.Base(cleanDir)
		if strings.Contains(cleanDir, "/") || strings.Contains(cleanDir, string(filepath.Separator)) {
			subpaths = append(subpaths, filepath.Join("templates", baseName))
			subpaths = append(subpaths, baseName)
		} else {
			subpaths = append(subpaths, filepath.Join("templates", cleanDir))
		}
	}
	subpaths = append(subpaths, "templates")

	var candidates []string
	if filepath.IsAbs(fsDir) && fsDir != "/" {
		candidates = append(candidates, fsDir)
	}
	for _, base := range bases {
		for _, sub := range subpaths {
			candidates = append(candidates, filepath.Join(base, sub))
		}
	}

	seen := make(map[string]bool)
	for _, cand := range candidates {
		cand = filepath.Clean(cand)
		if seen[cand] {
			continue
		}
		seen[cand] = true

		st, err := os.Stat(cand)
		if err != nil || !st.IsDir() {
			continue
		}

		var files []string
		err = filepath.Walk(cand, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if path != cand && strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasPrefix(info.Name(), ".") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil || len(files) == 0 {
			continue
		}

		sort.Strings(files)
		var bodies []string
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return "", fmt.Errorf("reading template file %s: %w", f, err)
			}
			bodies = append(bodies, string(data))
		}
		return strings.Join(bodies, "\n---\n"), nil
	}

	return "", os.ErrNotExist
}

func isGoTemplatingStep(fnName, stepName string, step map[string]any, opts Options) bool {
	lowerFn := strings.ToLower(fnName)
	if lowerFn == "function-go-templating" || strings.Contains(lowerFn, "go-templating") || strings.Contains(lowerFn, "gotemplating") {
		return true
	}
	if opts.FunctionPackages != nil {
		if pkg, ok := opts.FunctionPackages[fnName]; ok {
			lowerPkg := strings.ToLower(pkg)
			if strings.Contains(lowerPkg, "function-go-templating") || strings.Contains(lowerPkg, "go-templating") || strings.Contains(lowerPkg, "gotemplating") {
				return true
			}
		}
		if stepName != "" {
			if pkg, ok := opts.FunctionPackages[stepName]; ok {
				lowerPkg := strings.ToLower(pkg)
				if strings.Contains(lowerPkg, "function-go-templating") || strings.Contains(lowerPkg, "go-templating") || strings.Contains(lowerPkg, "gotemplating") {
					return true
				}
			}
		}
	}
	if input, ok := step["input"].(map[string]any); ok && input != nil {
		kind, _ := input["kind"].(string)
		if strings.EqualFold(kind, "GoTemplate") {
			return true
		}
		apiVer, _ := input["apiVersion"].(string)
		if strings.HasPrefix(strings.ToLower(apiVer), "gotemplating.fn.crossplane.io") {
			return true
		}
	}
	return false
}

func parsePipelineComposition(pipeline []any, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string, hasXRD bool) error {
	type parsedStep struct {
		step       blueprint.PipelineStep
		pkgAssumed bool
	}
	var otherSteps []parsedStep
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

		if isGoTemplatingStep(fnName, stepName, step, opts) {
			seenEngineStep = true
			input, _ := step["input"].(map[string]any)
			inline, _ := input["inline"].(map[string]any)
			tmpl, _ := inline["template"].(string)
			source, _ := input["source"].(string)
			isFileSystem := strings.EqualFold(source, "FileSystem") || input["fileSystem"] != nil

			if isFileSystem {
				if bp.Spec.Emit == nil {
					bp.Spec.Emit = &blueprint.Emit{}
				}
				bp.Spec.Emit.TemplateSource = blueprint.TemplateSourceFileSystem
			}

			if tmpl != "" {
				if err := parseGoTemplateBody(tmpl, bp, opts, report, nameMapping); err != nil {
					return fmt.Errorf("parse go template: %w", err)
				}
			} else if isFileSystem {
				var fsDir string
				if fsMap, ok := input["fileSystem"].(map[string]any); ok {
					if dp, ok := fsMap["dirPath"].(string); ok && dp != "" {
						fsDir = dp
					} else if d, ok := fsMap["dir"].(string); ok && d != "" {
						fsDir = d
					}
				} else if s, ok := input["fileSystem"].(string); ok {
					fsDir = s
				}

				combinedTmpl, err := loadFileSystemTemplates(fsDir, opts)
				if err != nil {
					reason := "resources defined in fileSystem.dir could not be adopted"
					if fsDir != "" {
						reason = fmt.Sprintf("resources defined in fileSystem.dir could not be adopted: %s", fsDir)
					}
					report.Record("fileSystem.dir", reason)
				} else {
					if err := parseGoTemplateBody(combinedTmpl, bp, opts, report, nameMapping); err != nil {
						return fmt.Errorf("parse go template: %w", err)
					}
				}
			}
		} else if fnName == "function-patch-and-transform" || strings.Contains(fnName, "patch-and-transform") {
			seenEngineStep = true
			input, _ := step["input"].(map[string]any)
			if input != nil {
				patchSets, _ := input["patchSets"].([]any)
				if resources, ok := input["resources"].([]any); ok {
					if err := parseClassicComposition(resources, patchSets, bp, opts, report, nameMapping); err != nil {
						return fmt.Errorf("parse patch-and-transform resources: %w", err)
					}
				}
			}
		} else if fnName == "function-kcl" || strings.Contains(fnName, "kcl") || fnName == "function-python" || strings.Contains(fnName, "python") || stepName == "render-resources" {
			return fmt.Errorf("cannot adopt composition with function %q: cf adopt supports function-go-templating and function-patch-and-transform", fnName)
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
			pkgAssumed := false
			if pkg == "" {
				pkgAssumed = true
				if fnName == "function-auto-ready" {
					pkg = "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
				} else {
					pkg = "xpkg.crossplane.io/crossplane-contrib/" + fnName + ":v0.1.0"
				}
			}
			pos := "after"
			if !seenEngineStep || fnName == blueprint.EnvironmentConfigsFunctionName || fnName == "function-environment-configs" {
				pos = "before"
			}
			otherSteps = append(otherSteps, parsedStep{
				step: blueprint.PipelineStep{
					Name:        stepName,
					FunctionRef: fnName,
					Package:     pkg,
					Input:       inputYAML,
					Position:    pos,
				},
				pkgAssumed: pkgAssumed,
			})
		}
	}

	for _, ps := range otherSteps {
		s := ps.step
		if s.FunctionRef == blueprint.EnvironmentConfigsFunctionName && len(bp.Spec.EnvironmentConfigs) == 0 && s.Input != "" {
			type envConfigEntry struct {
				Type string `json:"type"`
				Ref  *struct {
					Name string `json:"name"`
				} `json:"ref"`
				Selector *struct {
					MatchLabels map[string]string `json:"matchLabels"`
				} `json:"selector"`
			}
			type envConfigDoc struct {
				Spec struct {
					EnvironmentConfigs []envConfigEntry `json:"environmentConfigs"`
				} `json:"spec"`
			}
			var doc envConfigDoc
			if err := yaml.Unmarshal([]byte(s.Input), &doc); err == nil && len(doc.Spec.EnvironmentConfigs) > 0 {
				isDefault := len(doc.Spec.EnvironmentConfigs) == 1 &&
					doc.Spec.EnvironmentConfigs[0].Selector == nil &&
					(doc.Spec.EnvironmentConfigs[0].Ref == nil || doc.Spec.EnvironmentConfigs[0].Ref.Name == "" || doc.Spec.EnvironmentConfigs[0].Ref.Name == "default")
				if !isDefault {
					var extracted []blueprint.EnvironmentConfig
					for _, e := range doc.Spec.EnvironmentConfigs {
						var cfg blueprint.EnvironmentConfig
						if e.Selector != nil && len(e.Selector.MatchLabels) > 0 {
							cfg.Selector = &blueprint.EnvironmentConfigSelector{
								MatchLabels: e.Selector.MatchLabels,
							}
						}
						if e.Ref != nil && e.Ref.Name != "" {
							cfg.Name = e.Ref.Name
						}
						extracted = append(extracted, cfg)
					}
					if len(extracted) > 0 {
						bp.Spec.EnvironmentConfigs = extracted
					}
				}
			}
		}
	}

	isEnvConfigsStep := func(s blueprint.PipelineStep) bool {
		if s.FunctionRef != blueprint.EnvironmentConfigsFunctionName {
			return false
		}
		if len(bp.Spec.Environment) == 0 && len(bp.Spec.EnvironmentConfigs) == 0 {
			return false
		}
		trimmed := strings.TrimSpace(s.Input)
		if trimmed == "" || trimmed == strings.TrimSpace(blueprint.DefaultEnvironmentConfigsInput) || trimmed == strings.TrimSpace(bp.EnvironmentConfigsInput()) {
			return true
		}
		var stepDoc struct {
			Spec struct {
				EnvironmentConfigs []struct {
					Type string `json:"type"`
					Ref  *struct {
						Name string `json:"name"`
					} `json:"ref"`
					Selector *struct {
						MatchLabels map[string]string `json:"matchLabels"`
					} `json:"selector"`
				} `json:"environmentConfigs"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(s.Input), &stepDoc); err == nil {
			cfgs := stepDoc.Spec.EnvironmentConfigs
			if len(cfgs) == 1 && (cfgs[0].Type == "Reference" || cfgs[0].Type == "") &&
				(cfgs[0].Ref == nil || cfgs[0].Ref.Name == "" || cfgs[0].Ref.Name == "default") {
				if len(bp.Spec.EnvironmentConfigs) == 0 ||
					(len(bp.Spec.EnvironmentConfigs) == 1 &&
						bp.Spec.EnvironmentConfigs[0].Selector == nil &&
						(bp.Spec.EnvironmentConfigs[0].Name == "" || bp.Spec.EnvironmentConfigs[0].Name == "default")) {
					return true
				}
			}
			if len(cfgs) == len(bp.Spec.EnvironmentConfigs) && len(cfgs) > 0 {
				allMatch := true
				for i, e := range cfgs {
					bCfg := bp.Spec.EnvironmentConfigs[i]
					if e.Selector != nil && len(e.Selector.MatchLabels) > 0 {
						if bCfg.Selector == nil || len(bCfg.Selector.MatchLabels) != len(e.Selector.MatchLabels) {
							allMatch = false
							break
						}
						for k, v := range e.Selector.MatchLabels {
							if bCfg.Selector.MatchLabels[k] != v {
								allMatch = false
								break
							}
						}
						if !allMatch {
							break
						}
					} else {
						refName := ""
						if e.Ref != nil {
							refName = e.Ref.Name
						}
						bName := bCfg.Name
						if bCfg.Selector != nil || (bName != refName && !(bName == "" && refName == "default") && !(bName == "default" && refName == "")) {
							allMatch = false
							break
						}
					}
				}
				if allMatch {
					return true
				}
			}
		}
		return false
	}

	hasOtherCustomSteps := false
	for _, ps := range otherSteps {
		s := ps.step
		if isEnvConfigsStep(s) {
			continue
		}
		if (s.FunctionRef == "function-auto-ready" || s.Name == "auto-ready") && s.Input == "" &&
			(s.Package == "" || s.Package == "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0") {
			continue
		}
		hasOtherCustomSteps = true
		break
	}

	var finalSteps []blueprint.PipelineStep
	for _, ps := range otherSteps {
		s := ps.step
		if isEnvConfigsStep(s) {
			continue
		}
		if ps.pkgAssumed && report != nil {
			isAutoReadyStep := (s.FunctionRef == "function-auto-ready" || s.Name == "auto-ready")
			if !isAutoReadyStep || !hasXRD {
				stepID := s.Name
				if stepID == "" {
					stepID = s.FunctionRef
				}
				if stepID == "" {
					stepID = "step"
				}
				report.Record("pipeline."+stepID, fmt.Sprintf("without functions.yaml, function package could not be recovered (assumed default %s)", s.Package))
			}
		}
		if !hasOtherCustomSteps && (s.FunctionRef == "function-auto-ready" || s.Name == "auto-ready") && s.Input == "" &&
			(s.Package == "" || s.Package == "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0") {
			continue
		}
		finalSteps = append(finalSteps, s)
	}
	bp.Spec.Pipeline = finalSteps
	return nil
}

func identifyChunkTarget(chunk string) string {
	if m := reSetResourceNameAnn.FindStringSubmatch(chunk); len(m) >= 2 {
		annVal := m[1]
		if annVal == "" && len(m) >= 3 {
			annVal = m[2]
		}
		if annVal != "" {
			return fmt.Sprintf("template.resource.%s", normalizeDNSLabel(annVal))
		}
	}
	if m := reChunkResNameAnn.FindStringSubmatch(chunk); len(m) >= 2 && m[1] != "" {
		return fmt.Sprintf("template.resource.%s", normalizeDNSLabel(m[1]))
	}
	if m := reChunkName.FindStringSubmatch(chunk); len(m) >= 2 && m[1] != "" {
		return fmt.Sprintf("template.resource.%s", normalizeDNSLabel(m[1]))
	}
	if m := reChunkKind.FindStringSubmatch(chunk); len(m) >= 2 && m[1] != "" {
		return fmt.Sprintf("template.resource.%s", m[1])
	}
	return "template.chunk"
}

func validateGoTemplate(tmpl string) error {
	idx := 0
	for {
		start := strings.Index(tmpl[idx:], "{{")
		if start == -1 {
			break
		}
		start += idx
		end := strings.Index(tmpl[start+2:], "}}")
		if end == -1 {
			return fmt.Errorf("unclosed template action: missing '}}'")
		}
		idx = start + 2 + end + 2
	}
	return nil
}

func extractWhenGuard(text string, bp *blueprint.Blueprint, report *LossReport) string {
	if text == "" {
		return ""
	}
	if m := reWhenIfEnvEq.FindStringSubmatch(text); len(m) >= 3 {
		key, lit := m[1], m[2]
		if key == "" && len(m) >= 5 {
			key, lit = m[3], m[4]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested environment variable %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "string")
			return fmt.Sprintf("env.%s == %q", key, lit)
		}
	} else if m := reWhenIfEnvNe.FindStringSubmatch(text); len(m) >= 3 {
		key, lit := m[1], m[2]
		if key == "" && len(m) >= 5 {
			key, lit = m[3], m[4]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested environment variable %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "string")
			return fmt.Sprintf("env.%s != %q", key, lit)
		}
	} else if m := reWhenIfEnvEqRev.FindStringSubmatch(text); len(m) >= 3 {
		lit, key := m[1], m[2]
		if key == "" && len(m) >= 5 {
			lit, key = m[3], m[4]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested environment variable %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "string")
			return fmt.Sprintf("env.%s == %q", key, lit)
		}
	} else if m := reWhenIfEnvNeRev.FindStringSubmatch(text); len(m) >= 3 {
		lit, key := m[1], m[2]
		if key == "" && len(m) >= 5 {
			lit, key = m[3], m[4]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested environment variable %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "string")
			return fmt.Sprintf("env.%s != %q", key, lit)
		}
	} else if m := reWhenIfEnvSimple.FindStringSubmatch(text); len(m) >= 2 {
		key := m[1]
		if key == "" && len(m) >= 3 {
			key = m[2]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested environment variable %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "boolean")
			return fmt.Sprintf("env.%s", key)
		}
	} else if m := reWhenIfEq.FindStringSubmatch(text); len(m) >= 3 {
		key := m[1]
		if key == "" && len(m) >= 3 {
			key = m[2]
		}
		lit := ""
		if len(m) >= 4 {
			lit = m[3]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested parameter %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureParamDeclaredTyped(bp, key, "string")
			return fmt.Sprintf("params.%s == %q", key, lit)
		}
	} else if m := reWhenIfNe.FindStringSubmatch(text); len(m) >= 3 {
		key := m[1]
		if key == "" && len(m) >= 3 {
			key = m[2]
		}
		lit := ""
		if len(m) >= 4 {
			lit = m[3]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested parameter %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureParamDeclaredTyped(bp, key, "string")
			return fmt.Sprintf("params.%s != %q", key, lit)
		}
	} else if m := reWhenIfEqRev.FindStringSubmatch(text); len(m) >= 3 {
		lit := m[1]
		key := m[2]
		if key == "" && len(m) >= 4 {
			key = m[3]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested parameter %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureParamDeclaredTyped(bp, key, "string")
			return fmt.Sprintf("params.%s == %q", key, lit)
		}
	} else if m := reWhenIfNeRev.FindStringSubmatch(text); len(m) >= 3 {
		lit := m[1]
		key := m[2]
		if key == "" && len(m) >= 4 {
			key = m[3]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.when", fmt.Sprintf("unsupported nested parameter %q in when condition (conditions reference top-level parameters only in v1)", key))
				}
				return ""
			}
			ensureParamDeclaredTyped(bp, key, "string")
			return fmt.Sprintf("params.%s != %q", key, lit)
		}
	}
	for _, re := range []*regexp.Regexp{
		reWhenIfBoolEq,
		reWhenIfBoolEqRev,
		reWhenIfBoolNe,
		reWhenIfBoolNeRev,
		reWhenIfDefault,
		reWhenIfSimple,
	} {
		if m := re.FindStringSubmatch(text); len(m) >= 2 {
			key := m[1]
			if key == "" && len(m) >= 3 {
				key = m[2]
			}
			if key != "" {
				if !isFlatParamIdentifier(key) {
					if report != nil {
						report.Record("template.when", fmt.Sprintf("unsupported nested parameter %q in when condition (conditions reference top-level parameters only in v1)", key))
					}
					return ""
				}
				ensureParamDeclaredTyped(bp, key, "boolean")
				return fmt.Sprintf("params.%s", key)
			}
		}
	}
	return ""
}

func extractForEachGuard(text string, bp *blueprint.Blueprint, report *LossReport) string {
	if text == "" {
		return ""
	}
	if m := reForEachEnvLoop.FindStringSubmatch(text); len(m) >= 2 {
		key := m[1]
		if key == "" && len(m) >= 3 {
			key = m[2]
		}
		if key != "" {
			if !isFlatParamIdentifier(key) {
				if report != nil {
					report.Record("template.forEach", fmt.Sprintf("unsupported nested environment variable %q in forEach loop (loop bounds stay top-level integer parameters in v1)", key))
				}
				return ""
			}
			ensureEnvDeclared(bp, key, "integer")
			return fmt.Sprintf("env.%s", key)
		}
	} else if m := reForEachStatusLoop.FindStringSubmatch(text); len(m) >= 3 {
		resName := m[1]
		statusPath := m[2]
		if resName == "" && len(m) >= 5 && m[3] != "" {
			resName = m[3]
			statusPath = m[4]
		}
		if resName == "" && len(m) >= 8 {
			if m[5] != "" {
				resName = m[5]
			} else if m[6] != "" {
				resName = m[6]
			}
			statusPath = m[7]
		}
		if resName != "" && statusPath != "" {
			return fmt.Sprintf("resources.%s.status.%s", resName, statusPath)
		}
	} else if m := reForEachLoop.FindStringSubmatch(text); len(m) >= 2 {
		pName := m[1]
		if pName == "" && len(m) >= 3 {
			pName = m[2]
		}
		if pName != "" {
			if !isFlatParamIdentifier(pName) {
				if report != nil {
					report.Record("template.forEach", fmt.Sprintf("unsupported nested parameter %q in forEach loop (loop bounds stay top-level integer parameters in v1)", pName))
				}
				return ""
			}
			ensureParamDeclaredTyped(bp, pName, "integer")
			if defVal := extractForEachDefault(m[0]); defVal != "" {
				ensureParamDefault(bp, pName, defVal)
			}
			return fmt.Sprintf("params.%s", pName)
		}
	}
	return ""
}

func parseGoTemplateBody(tmpl string, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string) error {
	// 0. Validate Go template syntax (actions must be balanced and well-formed)
	if err := validateGoTemplate(tmpl); err != nil {
		return fmt.Errorf("malformed go template: %w", err)
	}

	initialResourceCount := len(bp.Spec.Resources)

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

	// 2. Discover parameter and environment references (including within named templates / define blocks)
	for _, action := range reTemplateAction.FindAllStringSubmatch(tmpl, -1) {
		body := action[1]
		if strings.HasPrefix(strings.TrimSpace(body), "/*") {
			continue
		}
		for _, m := range reEvidenceAnySpec.FindAllStringSubmatch(body, -1) {
			pName := m[1]
			if isValidParamIdentifier(pName) {
				ensureParamDeclared(bp, pName)
			} else {
				report.Record("template.param."+pName, "invalid parameter identifier")
			}
		}
		for _, m := range reEvidenceIndexSpec.FindAllStringSubmatch(body, -1) {
			pName := m[1]
			if isValidParamIdentifier(pName) {
				ensureParamDeclared(bp, pName)
			} else {
				report.Record("template.param."+pName, "invalid parameter identifier")
			}
		}
		for _, m := range reEvidenceGuard.FindAllStringSubmatch(body, -1) {
			pName := m[1]
			if isValidParamIdentifier(pName) {
				ensureParamDeclared(bp, pName)
			} else {
				report.Record("template.param."+pName, "invalid parameter identifier")
			}
		}
		for _, re := range []*regexp.Regexp{
			reWhenIfBoolEq,
			reWhenIfBoolEqRev,
			reWhenIfBoolNe,
			reWhenIfBoolNeRev,
			reWhenIfDefault,
			reWhenIfSimple,
		} {
			if m := re.FindStringSubmatch(action[0]); len(m) >= 2 {
				pName := m[1]
				if pName == "" && len(m) >= 3 {
					pName = m[2]
				}
				if pName != "" && isFlatParamIdentifier(pName) {
					ensureParamDeclaredTyped(bp, pName, "boolean")
				}
			}
		}
		if m := reForEachLoop.FindStringSubmatch(action[0]); len(m) >= 2 {
			pName := m[1]
			if pName == "" && len(m) >= 3 {
				pName = m[2]
			}
			if pName != "" && isFlatParamIdentifier(pName) {
				ensureParamDeclaredTyped(bp, pName, "integer")
				if defVal := extractForEachDefault(m[0]); defVal != "" {
					ensureParamDefault(bp, pName, defVal)
				}
			}
		}
	}
	paramMatches := reParamVar.FindAllString(tmpl, -1)
	for _, raw := range paramMatches {
		pName := matchParamVar(raw)
		if pName != "" {
			if isValidParamIdentifier(pName) {
				ensureParamDeclared(bp, pName)
			} else {
				report.Record("template.param."+pName, "invalid parameter identifier")
			}
		}
	}
	envMatches := reEnvVar.FindAllString(tmpl, -1)
	for _, raw := range envMatches {
		key := matchEnvVar(raw)
		if key != "" && isFlatParamIdentifier(key) {
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

		lines := strings.Split(chunk, "\n")
		var filteredLines []string
		firstYAMLLine := -1
		lastYAMLLine := -1
		skipNextEmptyBlock := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if m := reSetResourceNameAnn.FindStringSubmatch(trimmed); len(m) >= 2 {
				annVal := m[1]
				if annVal == "" && len(m) >= 3 {
					annVal = m[2]
				}
				filteredLines = append(filteredLines, strings.Replace(line, trimmed, fmt.Sprintf(`"crossplane.io/composition-resource-name": "%s"`, annVal), 1))
				if firstYAMLLine == -1 {
					firstYAMLLine = i
				}
				lastYAMLLine = i
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
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if firstYAMLLine == -1 {
					firstYAMLLine = i
				}
				lastYAMLLine = i
			}
		}

		if firstYAMLLine == -1 {
			if w := extractWhenGuard(chunk, bp, report); w != "" {
				nextWhen = w
			}
			if f := extractForEachGuard(chunk, bp, report); f != "" {
				nextForEach = f
			}
			continue
		}

		headText := strings.Join(lines[:firstYAMLLine], "\n")
		tailText := strings.Join(lines[lastYAMLLine+1:], "\n")

		when := nextWhen
		nextWhen = ""
		if w := extractWhenGuard(headText, bp, report); w != "" {
			when = w
		}

		forEach := nextForEach
		nextForEach = ""
		if f := extractForEachGuard(headText, bp, report); f != "" {
			forEach = f
		}

		if w := extractWhenGuard(tailText, bp, report); w != "" {
			nextWhen = w
		}
		if f := extractForEachGuard(tailText, bp, report); f != "" {
			nextForEach = f
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
			target := identifyChunkTarget(chunk)
			report.Record(target, fmt.Sprintf("failed to parse chunk YAML: %v", err))
			continue
		}
		docs = unwrapListDocs(docs)
		for _, doc := range docs {
			ScrubDocument(doc, "", report)
			res := resourceFromMap(doc, opts, placeholderTable, report, nameMapping, bp)
			if res == nil {
				target := identifyChunkTarget(chunk)
				if docKind, _ := doc["kind"].(string); docKind != "" {
					target = fmt.Sprintf("template.resource.%s", docKind)
				}
				report.Record(target, "document could not be parsed as a resource")
				continue
			}
			if when != "" {
				res.When = when
			}
			if forEach != "" {
				res.ForEach = forEach
			}

			origName := res.Name
			uniqueName(bp, res)
			if res.Name != origName && nameMapping != nil {
				compResName := extractCompositionResourceName(doc, placeholderTable)
				rawName := extractResourceName(doc, res.Kind, placeholderTable)
				if compResName != "" {
					nameMapping[compResName] = res.Name
					if clean := extractCleanName(compResName); clean != "" && clean != res.Name {
						nameMapping[clean] = res.Name
					}
				}
				if rawName != "" && rawName != strings.ToLower(res.Kind) {
					nameMapping[rawName] = res.Name
					if clean := extractCleanName(rawName); clean != "" && clean != res.Name {
						nameMapping[clean] = res.Name
					}
				}
				for k, v := range nameMapping {
					if v == origName && (k == compResName || k == rawName) {
						nameMapping[k] = res.Name
					}
				}
			}

			bp.Spec.Resources = append(bp.Spec.Resources, *res)
		}
	}

	if len(bp.Spec.Resources) == initialResourceCount && strings.TrimSpace(cleanTmpl) != "" {
		report.Record("template.body", "no resources could be recovered from template")
	}

	return nil
}

func discoverObjectParamsFromPatches(resources []any, patchSetsMap map[string][]any, bp *blueprint.Blueprint) {
	scanPatch := func(pRaw any) {
		pMap, ok := pRaw.(map[string]any)
		if !ok {
			return
		}
		pType, _ := pMap["type"].(string)
		if pType == "FromEnvironmentFieldPath" {
			fromPath, _ := pMap["fromFieldPath"].(string)
			if fromPath != "" && isFlatParamIdentifier(fromPath) {
				ensureEnvDeclared(bp, fromPath, "string")
			}
			return
		}
		if pType != "FromCompositeFieldPath" && pType != "" {
			return
		}
		fromPath, _ := pMap["fromFieldPath"].(string)
		var paramName string
		if strings.HasPrefix(fromPath, "spec.parameters.") {
			paramName = strings.TrimPrefix(fromPath, "spec.parameters.")
		} else if strings.HasPrefix(fromPath, "spec.") {
			paramName = strings.TrimPrefix(fromPath, "spec.")
		}
		if paramName != "" && strings.Contains(paramName, ".") && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
			ensureParamDeclared(bp, paramName)
		}
	}

	for _, psPatches := range patchSetsMap {
		for _, pRaw := range psPatches {
			scanPatch(pRaw)
		}
	}

	for _, resRaw := range resources {
		resMap, ok := resRaw.(map[string]any)
		if !ok {
			continue
		}
		if patches, ok := resMap["patches"].([]any); ok {
			for _, pRaw := range patches {
				scanPatch(pRaw)
			}
		}
	}
}

func isWholeObjectParam(bp *blueprint.Blueprint, paramName string, targetField ...string) bool {
	tf := ""
	if len(targetField) > 0 {
		tf = targetField[0]
	}
	return isWholeObjectParamForFields(bp, paramName, tf, nil)
}

func isWholeObjectParamForResource(bp *blueprint.Blueprint, paramName string, targetField string, res *blueprint.Resource) bool {
	var fields map[string]blueprint.Field
	if res != nil {
		fields = res.Fields
	}
	return isWholeObjectParamForFields(bp, paramName, targetField, fields)
}

func isWholeObjectParamForFields(bp *blueprint.Blueprint, paramName string, targetField string, fields map[string]blueprint.Field) bool {
	if bp == nil || bp.Spec.XRD.Parameters == nil || paramName == "" {
		return false
	}
	parts := strings.Split(paramName, ".")
	p, exists := bp.Spec.XRD.Parameters[parts[0]]
	if !exists {
		return false
	}
	for i := 1; i < len(parts); i++ {
		next, ok := p.Properties[parts[i]]
		if !ok {
			return false
		}
		p = next
	}
	if len(p.Properties) > 0 {
		return true
	}
	if p.Type == "object" {
		if targetField != "" && isAdoptMapTargetWithFields(targetField, fields, bp) {
			return false
		}
		return true
	}
	return false
}

func isAdoptMapTargetWithFields(targetField string, fields map[string]blueprint.Field, bp *blueprint.Blueprint) bool {
	if isAdoptMapField(targetField) {
		return true
	}
	if strings.Contains(targetField, "[") {
		return false
	}
	prefix := targetField + "["
	for f := range fields {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	if bp != nil {
		for _, r := range bp.Spec.Resources {
			for f := range r.Fields {
				if strings.HasPrefix(f, prefix) {
					return true
				}
			}
		}
	}
	return false
}

func isAdoptMapField(fieldPath string) bool {
	if strings.Contains(fieldPath, "[") {
		return false
	}
	lower := strings.ToLower(fieldPath)
	return lower == "tags" || lower == "labels" || lower == "annotations" ||
		lower == "data" || lower == "stringdata" || lower == "binarydata" ||
		lower == "matchlabels" || lower == "nodeselector" ||
		strings.HasSuffix(lower, ".tags") || strings.HasSuffix(lower, ".labels") || strings.HasSuffix(lower, ".annotations") ||
		strings.HasSuffix(lower, ".data") || strings.HasSuffix(lower, ".stringdata") || strings.HasSuffix(lower, ".binarydata") ||
		strings.HasSuffix(lower, ".matchlabels") || strings.HasSuffix(lower, ".nodeselector")
}

func parseClassicComposition(resources []any, patchSets []any, bp *blueprint.Blueprint, opts Options, report *LossReport, nameMapping map[string]string) error {
	patchSetsMap := make(map[string][]any)
	for _, psRaw := range patchSets {
		if psMap, ok := psRaw.(map[string]any); ok {
			if name, ok := psMap["name"].(string); ok && name != "" {
				if patches, ok := psMap["patches"].([]any); ok {
					patchSetsMap[name] = patches
				} else {
					patchSetsMap[name] = nil
				}
			}
		}
	}

	discoverObjectParamsFromPatches(resources, patchSetsMap, bp)

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
				patchPath := fmt.Sprintf("resource.%s.patches[%d]", res.Name, patchIdx)
				applyPatch(pRaw, patchPath, res, bp, report, patchSetsMap, make(map[string]bool))
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

func applyPatch(pRaw any, patchPath string, res *blueprint.Resource, bp *blueprint.Blueprint, report *LossReport, patchSetsMap map[string][]any, visited map[string]bool) {
	pMap, ok := pRaw.(map[string]any)
	if !ok {
		return
	}
	pType, _ := pMap["type"].(string)
	fromPath, _ := pMap["fromFieldPath"].(string)
	toPath, _ := pMap["toFieldPath"].(string)

	if transforms, ok := pMap["transforms"].([]any); ok && len(transforms) > 0 {
		report.Record(fmt.Sprintf("%s.transforms", patchPath),
			"patch transforms are not supported in blueprint")
	}

	if pType == "PatchSet" {
		patchSetName, _ := pMap["patchSetName"].(string)
		psPatches, found := patchSetsMap[patchSetName]
		if !found {
			report.Record(patchPath, fmt.Sprintf("patchSet %q not found", patchSetName))
			return
		}
		if visited[patchSetName] {
			report.Record(patchPath, fmt.Sprintf("circular patchSet reference %q", patchSetName))
			return
		}
		visited[patchSetName] = true
		for _, psPatchRaw := range psPatches {
			applyPatch(psPatchRaw, patchPath, res, bp, report, patchSetsMap, visited)
		}
		delete(visited, patchSetName)
		return
	}

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
			targetField = normalizeMapFieldPath(targetField)
			if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
				report.Record(patchPath,
					fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
			} else if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = blueprint.Field{
					From: "params." + paramName,
				}
				if isAdoptMapField(targetField) {
					ensureParamDeclaredTyped(bp, paramName, "object")
				} else {
					ensureParamDeclared(bp, paramName)
				}
			} else {
				report.Record(patchPath,
					fmt.Sprintf("unsupported fromFieldPath %q in patch", fromPath))
			}
		} else if res.Provider != blueprint.NativeProvider && (toPath == "tags" || strings.HasPrefix(toPath, "tags.") || strings.HasPrefix(toPath, "tags[")) {
			targetField := normalizeMapFieldPath(toPath)
			if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
				report.Record(patchPath,
					fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
			} else if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = blueprint.Field{
					From: "params." + paramName,
				}
				if isAdoptMapField(targetField) {
					ensureParamDeclaredTyped(bp, paramName, "object")
				} else {
					ensureParamDeclared(bp, paramName)
				}
			} else {
				report.Record(patchPath,
					fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else if strings.HasPrefix(toPath, "spec.initProvider.") || toPath == "spec.initProvider" {
			report.Record(patchPath,
				fmt.Sprintf("unsupported toFieldPath %q in patch (initProvider is not supported in blueprint)", toPath))
		} else if strings.HasPrefix(toPath, "spec.") {
			if res.Provider == blueprint.NativeProvider {
				targetField := normalizeMapFieldPath(toPath)
				if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
					report.Record(patchPath,
						fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
				} else if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
					if res.Fields == nil {
						res.Fields = make(map[string]blueprint.Field)
					}
					res.Fields[targetField] = blueprint.Field{
						From: "params." + paramName,
					}
					if isAdoptMapField(targetField) {
						ensureParamDeclaredTyped(bp, paramName, "object")
					} else {
						ensureParamDeclared(bp, paramName)
					}
				} else {
					report.Record(patchPath,
						fmt.Sprintf("unsupported fromFieldPath %q in patch", fromPath))
				}
			} else {
				targetField := strings.TrimPrefix(toPath, "spec.")
				targetField = normalizeMapFieldPath(targetField)
				if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
					report.Record(patchPath,
						fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
				} else if isParamPatch && paramName != "" && targetField != "" && isValidParamIdentifier(paramName) {
					if res.Envelope == nil {
						res.Envelope = make(map[string]blueprint.Field)
					}
					res.Envelope[targetField] = blueprint.Field{
						From: "params." + paramName,
					}
					ensureParamDeclared(bp, paramName)
				} else {
					report.Record(patchPath,
						fmt.Sprintf("unsupported fromFieldPath %q in patch", fromPath))
				}
			}
		} else if strings.HasPrefix(toPath, "metadata.annotations.") || strings.HasPrefix(toPath, "metadata.annotations[") {
			annKey := strings.TrimPrefix(toPath, "metadata.annotations.")
			if strings.HasPrefix(toPath, "metadata.annotations[") {
				annKey = strings.TrimSuffix(strings.TrimPrefix(toPath, "metadata.annotations["), "]")
				annKey = strings.Trim(annKey, `"'`)
			}
			if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, annKey, res) {
				report.Record(patchPath,
					fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
			} else if isParamPatch && paramName != "" && annKey != "" && isValidParamIdentifier(paramName) {
				if res.Annotations == nil {
					res.Annotations = make(map[string]blueprint.Field)
				}
				res.Annotations[annKey] = blueprint.Field{
					From: "params." + paramName,
				}
				ensureParamDeclared(bp, paramName)
			} else {
				report.Record(patchPath,
					fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else if strings.HasPrefix(toPath, "metadata.") {
			if res.Provider != blueprint.NativeProvider {
				report.Record(patchPath,
					fmt.Sprintf("managed resource metadata field %q is not supported in blueprint", toPath))
			} else {
				targetField := normalizeMapFieldPath(toPath)
				if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
					report.Record(patchPath,
						fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
				} else if isParamPatch && paramName != "" && isValidParamIdentifier(paramName) {
					if res.Fields == nil {
						res.Fields = make(map[string]blueprint.Field)
					}
					res.Fields[targetField] = blueprint.Field{
						From: "params." + paramName,
					}
					if isAdoptMapField(targetField) {
						ensureParamDeclaredTyped(bp, paramName, "object")
					} else {
						ensureParamDeclared(bp, paramName)
					}
				} else {
					report.Record(patchPath,
						fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
				}
			}
		} else if res.Provider == blueprint.NativeProvider {
			targetField := normalizeMapFieldPath(toPath)
			if isParamPatch && !isReservedCompositeField(paramName) && isWholeObjectParamForResource(bp, paramName, targetField, res) {
				report.Record(patchPath,
					fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))
			} else if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = blueprint.Field{
					From: "params." + paramName,
				}
				if isAdoptMapField(targetField) {
					ensureParamDeclaredTyped(bp, paramName, "object")
				} else {
					ensureParamDeclared(bp, paramName)
				}
			} else {
				report.Record(patchPath,
					fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else {
			report.Record(patchPath,
				fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
		}
	} else if pType == "FromEnvironmentFieldPath" {
		envKey := fromPath
		if envKey == "" || !isFlatParamIdentifier(envKey) {
			if strings.Contains(fromPath, ".") {
				report.Record(patchPath, fmt.Sprintf("unsupported nested fromFieldPath %q in patch: environment keys must be flat camelCase identifiers", fromPath))
			} else {
				report.Record(patchPath, fmt.Sprintf("unsupported fromFieldPath %q in patch: environment keys must be flat camelCase identifiers", fromPath))
			}
			return
		}
		ensureEnvDeclared(bp, envKey, "string")

		wireField := blueprint.Field{
			From: "env." + envKey,
		}

		if strings.HasPrefix(toPath, "spec.forProvider.") {
			targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
			targetField = normalizeMapFieldPath(targetField)
			if targetField != "" {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = wireField
			} else {
				report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else if res.Provider != blueprint.NativeProvider && (toPath == "tags" || strings.HasPrefix(toPath, "tags.") || strings.HasPrefix(toPath, "tags[")) {
			targetField := normalizeMapFieldPath(toPath)
			if targetField != "" {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = wireField
			} else {
				report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else if strings.HasPrefix(toPath, "spec.initProvider.") || toPath == "spec.initProvider" {
			report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch (initProvider is not supported in blueprint)", toPath))
		} else if strings.HasPrefix(toPath, "spec.") {
			if res.Provider == blueprint.NativeProvider {
				targetField := normalizeMapFieldPath(toPath)
				if targetField != "" {
					if res.Fields == nil {
						res.Fields = make(map[string]blueprint.Field)
					}
					res.Fields[targetField] = wireField
				} else {
					report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
				}
			} else {
				targetField := strings.TrimPrefix(toPath, "spec.")
				targetField = normalizeMapFieldPath(targetField)
				if targetField != "" {
					if res.Envelope == nil {
						res.Envelope = make(map[string]blueprint.Field)
					}
					res.Envelope[targetField] = wireField
				} else {
					report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
				}
			}
		} else if strings.HasPrefix(toPath, "metadata.annotations.") || strings.HasPrefix(toPath, "metadata.annotations[") {
			annKey := strings.TrimPrefix(toPath, "metadata.annotations.")
			if strings.HasPrefix(toPath, "metadata.annotations[") {
				annKey = strings.TrimSuffix(strings.TrimPrefix(toPath, "metadata.annotations["), "]")
				annKey = strings.Trim(annKey, `"'`)
			}
			if annKey != "" {
				if res.Annotations == nil {
					res.Annotations = make(map[string]blueprint.Field)
				}
				res.Annotations[annKey] = wireField
			} else {
				report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else if strings.HasPrefix(toPath, "metadata.") {
			if res.Provider != blueprint.NativeProvider {
				report.Record(patchPath, fmt.Sprintf("managed resource metadata field %q is not supported in blueprint", toPath))
			} else {
				targetField := normalizeMapFieldPath(toPath)
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = wireField
			}
		} else if res.Provider == blueprint.NativeProvider {
			targetField := normalizeMapFieldPath(toPath)
			if targetField != "" {
				if res.Fields == nil {
					res.Fields = make(map[string]blueprint.Field)
				}
				res.Fields[targetField] = wireField
			} else {
				report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
			}
		} else {
			report.Record(patchPath, fmt.Sprintf("unsupported toFieldPath %q in patch", toPath))
		}
	} else {
		report.Record(patchPath,
			fmt.Sprintf("patch type %q is not supported in blueprint", pType))
	}
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
	ensureParamDeclaredTyped(bp, paramPath, "string")
}

func ensureParamDeclaredTyped(bp *blueprint.Blueprint, paramPath string, typ string) {
	if bp == nil {
		return
	}
	if bp.Spec.XRD.Parameters == nil {
		bp.Spec.XRD.Parameters = make(map[string]blueprint.Parameter)
	}
	if typ == "" {
		typ = "string"
	}
	parts := strings.Split(paramPath, ".")
	if len(parts) == 0 || parts[0] == "" {
		return
	}

	insertParamIntoMap(bp.Spec.XRD.Parameters, parts, typ)
}

func insertParamIntoMap(props map[string]blueprint.Parameter, parts []string, typ string) {
	head := parts[0]
	if len(parts) == 1 {
		p, exists := props[head]
		if !exists {
			props[head] = blueprint.Parameter{
				Type:     typ,
				Required: false,
			}
		} else if typ != "string" && p.Type == "string" {
			p.Type = typ
			props[head] = p
		}
		return
	}

	// Intermediate object node
	p, exists := props[head]
	if !exists {
		p = blueprint.Parameter{
			Type:       "object",
			Properties: make(map[string]blueprint.Parameter),
		}
	} else {
		if p.Type != "object" {
			p.Type = "object"
		}
		if p.Properties == nil {
			p.Properties = make(map[string]blueprint.Parameter)
		}
	}
	insertParamIntoMap(p.Properties, parts[1:], typ)
	props[head] = p
}

func extractForEachDefault(text string) string {
	dm := reForEachDefault.FindStringSubmatch(text)
	if len(dm) == 0 {
		return ""
	}
	for i := 1; i < len(dm); i++ {
		if dm[i] != "" {
			return dm[i]
		}
	}
	return ""
}

func ensureParamDefault(bp *blueprint.Blueprint, paramPath string, defVal string) {
	if bp == nil || bp.Spec.XRD.Parameters == nil || defVal == "" {
		return
	}
	parts := strings.Split(paramPath, ".")
	if len(parts) == 0 || parts[0] == "" {
		return
	}
	insertParamDefaultIntoMap(bp.Spec.XRD.Parameters, parts, defVal)
}

func insertParamDefaultIntoMap(props map[string]blueprint.Parameter, parts []string, defVal string) {
	head := parts[0]
	if len(parts) == 1 {
		p, exists := props[head]
		if exists && p.Default == "" {
			p.Default = defVal
			props[head] = p
		}
		return
	}
	p, exists := props[head]
	if exists && p.Properties != nil {
		insertParamDefaultIntoMap(p.Properties, parts[1:], defVal)
	}
}

func ensureEnvDeclared(bp *blueprint.Blueprint, envKey, typ string) {
	if !isFlatParamIdentifier(envKey) {
		return
	}
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

	store := opts.Store
	if store == nil && opts.CacheDir != "" {
		store = cache.New(opts.CacheDir)
	}
	provider := inferProvider(apiVersion, kind, opts.DefaultProviderRef, store, bp)

	res := &blueprint.Resource{
		Name:        normName,
		Kind:        kind,
		Provider:    provider,
		Fields:      make(map[string]blueprint.Field),
		Annotations: make(map[string]blueprint.Field),
	}
	if provider != blueprint.NativeProvider {
		res.Envelope = make(map[string]blueprint.Field)
	}
	isNative := res.Provider == blueprint.NativeProvider

	meta, _ := m["metadata"].(map[string]any)
	// Extract annotations
	if meta != nil {
		if anns, ok := meta["annotations"].(map[string]any); ok {
			annKeys := make([]string, 0, len(anns))
			for k := range anns {
				annKeys = append(annKeys, k)
			}
			sort.Strings(annKeys)
			for _, k := range annKeys {
				v := anns[k]
				rawK := unmaskString(fmt.Sprint(k), placeholders)
				rawStr := unmaskString(formatScalarValue(v), placeholders)
				if blueprint.ReservedAnnotationKey(k) || blueprint.ReservedAnnotationKey(rawK) || strings.Contains(rawK, "setResourceNameAnnotation") || strings.Contains(rawStr, "setResourceNameAnnotation") {
					continue
				}
				if rePlaceholder.MatchString(fmt.Sprint(k)) {
					report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK), "dynamic map key with template expression is not supported in blueprint")
					continue
				}
				if err := checkScalarClean(rawStr); err != nil {
					report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK), "contains newlines or control characters")
					continue
				}
				trimmed := strings.TrimSpace(rawStr)
				_ = trimmed
				if pName := matchParamVar(rawStr); pName != "" {
					if isWholeObjectParam(bp, pName) {
						if report != nil {
							report.Record("resource."+res.Name+".metadata.annotations."+rawK, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", pName))
						}
					} else if isValidParamIdentifier(pName) {
						res.Annotations[rawK] = blueprint.Field{From: "params." + pName}
					} else {
						report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK), "invalid parameter reference")
					}
				} else if key := matchEnvVar(rawStr); key != "" {
					if isFlatParamIdentifier(key) {
						res.Annotations[rawK] = blueprint.Field{From: "env." + key}
						if bp != nil {
							ensureEnvDeclared(bp, key, "string")
						}
					} else {
						if strings.Contains(key, ".") {
							report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK), fmt.Sprintf("unsupported nested environment variable %q: environment keys must be flat camelCase identifiers", key))
						} else {
							report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK), "invalid environment reference")
						}
					}
				} else if srcRes, targetKind, targetField, ok := matchObservedStatus(trimmed); ok {
					if nameMapping != nil && nameMapping[srcRes] != "" {
						srcRes = nameMapping[srcRes]
					} else {
						srcRes = normalizeDNSLabel(srcRes)
					}
					var fromPath string
					if strings.HasPrefix(targetKind, "status") {
						field := targetField
						if !strings.HasPrefix(field, "atProvider.") && (strings.HasSuffix(targetKind, "atProvider") || targetKind == "status.atProvider") {
							field = "atProvider." + field
						}
						fromPath = "resources." + srcRes + ".status." + field
						res.Annotations[rawK] = blueprint.Field{From: fromPath}
					} else if targetField == "name" {
						fromPath = "resources." + srcRes + ".metadata.name"
						res.Annotations[rawK] = blueprint.Field{From: fromPath}
					} else {
						res.Annotations[rawK] = blueprint.Field{Raw: rawStr}
					}
				} else if m := reXRResourceRef.FindStringSubmatch(trimmed); len(m) >= 2 && m[0] == trimmed {
					srcRes := m[1]
					if nameMapping != nil && nameMapping[srcRes] != "" {
						srcRes = nameMapping[srcRes]
					} else {
						srcRes = normalizeDNSLabel(srcRes)
					}
					res.Annotations[rawK] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
				} else if tmplName := matchTemplateInclude(rawStr); tmplName != "" {
					if templateExists(bp, tmplName) {
						res.Annotations[rawK] = blueprint.Field{Template: tmplName}
					} else {
						if report != nil {
							report.Record(fmt.Sprintf("resource.%s.annotations[%s]", res.Name, rawK),
								fmt.Sprintf("template %q referenced by include is not defined", tmplName))
						}
						res.Annotations[rawK] = blueprint.Field{Raw: rawStr}
					}
				} else if strings.Contains(rawStr, "{{") {
					res.Annotations[rawK] = blueprint.Field{Raw: rawStr}
				} else {
					res.Annotations[rawK] = blueprint.Field{Value: rawStr}
				}
			}
		}

		// Extract user-declared metadata fields (e.g. labels, namespace, custom name) into res.Fields
		otherMeta := make(map[string]any)
		for k, v := range meta {
			if k == "annotations" {
				continue
			}
			if k == "name" {
				rawName := unmaskString(formatScalarValue(v), placeholders)
				if isDefaultMetadataName(rawName, res.Name, normName) {
					continue
				}
			}
			otherMeta[k] = v
		}
		if len(otherMeta) > 0 {
			if isNative {
				extractFields("metadata", otherMeta, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
			} else {
				keys := make([]string, 0, len(otherMeta))
				for k := range otherMeta {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					rawK := unmaskString(k, placeholders)
					if subMap, ok := otherMeta[k].(map[string]any); ok {
						subKeys := make([]string, 0, len(subMap))
						for sk := range subMap {
							subKeys = append(subKeys, sk)
						}
						sort.Strings(subKeys)
						for _, sk := range subKeys {
							rawSK := unmaskString(sk, placeholders)
							report.Record(fmt.Sprintf("resource.%s.metadata.%s[%s]", res.Name, rawK, rawSK),
								fmt.Sprintf("managed resource metadata field %q is not supported in blueprint", rawK+"."+rawSK))
						}
					} else {
						report.Record(fmt.Sprintf("resource.%s.metadata.%s", res.Name, rawK),
							fmt.Sprintf("managed resource metadata field %q is not supported in blueprint", rawK))
					}
				}
			}
		}
	}

	// Extract other top-level fields (e.g. data in ConfigMap, automountServiceAccountToken in ServiceAccount)
	otherTop := make(map[string]any)
	for k, v := range m {
		if k == "apiVersion" || k == "kind" || k == "metadata" || k == "spec" || k == "status" {
			continue
		}
		otherTop[k] = v
	}
	if len(otherTop) > 0 {
		extractFields("", otherTop, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
	}

	// Extract spec fields
	if spec, ok := m["spec"].(map[string]any); ok {
		if !isNative {
			if forProvider, ok := spec["forProvider"].(map[string]any); ok {
				extractFields("", forProvider, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
			}
			if initProvider, ok := spec["initProvider"].(map[string]any); ok {
				initKeys := make([]string, 0, len(initProvider))
				for k := range initProvider {
					initKeys = append(initKeys, k)
				}
				sort.Strings(initKeys)
				for _, k := range initKeys {
					if report != nil {
						report.Record(fmt.Sprintf("resource.%s.initProvider.%s", res.Name, k), "initProvider is not supported in blueprint")
					}
				}
			} else if _, exists := spec["initProvider"]; exists {
				if report != nil {
					report.Record(fmt.Sprintf("resource.%s.initProvider", res.Name), "initProvider is not supported in blueprint")
				}
			}
			specKeys := make([]string, 0, len(spec))
			for k := range spec {
				specKeys = append(specKeys, k)
			}
			sort.Strings(specKeys)
			for _, k := range specKeys {
				v := spec[k]
				if k == "forProvider" || k == "initProvider" {
					continue
				}
				if rePlaceholder.MatchString(k) {
					rawK := unmaskString(k, placeholders)
					report.Record(fmt.Sprintf("resource.%s.envelope.%s", res.Name, rawK), "dynamic map key with template expression is not supported in blueprint")
					continue
				}
				if k == "providerConfigRef" {
					if pcrMap, ok := v.(map[string]any); ok {
						pcrName, _ := pcrMap["name"].(string)
						pcrKind, _ := pcrMap["kind"].(string)
						pcrNameUnmasked := unmaskString(pcrName, placeholders)
						if (pcrNameUnmasked == "{{ $spec.providerName }}" || pcrNameUnmasked == "{{ .spec.providerName }}" || matchParamVar(pcrNameUnmasked) == "providerName") &&
							(pcrKind == "ClusterProviderConfig" || pcrKind == "") {
							continue
						}
					}
				}
				extractEnvelopeFields("", map[string]any{k: v}, res.Envelope, placeholders, res.Name, report, nameMapping, bp)
			}
		} else {
			extractFields("spec", spec, res.Fields, placeholders, res.Name, report, nameMapping, bp, isNative)
		}
	}

	return res
}

func extractEnvelopeFields(prefix string, obj map[string]any, out map[string]blueprint.Field, placeholders []string, resName string, report *LossReport, nameMapping map[string]string, bp *blueprint.Blueprint) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := obj[k]
		if rePlaceholder.MatchString(k) {
			rawK := unmaskString(k, placeholders)
			path := rawK
			if prefix != "" {
				path = prefix + "." + rawK
			}
			report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path), "dynamic map key with template expression is not supported in blueprint")
			continue
		}
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			extractEnvelopeFields(path, val, out, placeholders, resName, report, nameMapping, bp)
		case []any, []string:
			var sliceItems []any
			if sList, ok := val.([]string); ok {
				sliceItems = make([]any, len(sList))
				for i, s := range sList {
					sliceItems[i] = s
				}
			} else {
				sliceItems = val.([]any)
			}

			if len(sliceItems) == 0 {
				out[path] = blueprint.Field{Raw: "[]"}
				continue
			}

			canUseValue := true
			items := make([]string, 0, len(sliceItems))
			for _, elem := range sliceItems {
				switch e := elem.(type) {
				case map[string]any, []any, []string:
					canUseValue = false
				case nil:
					canUseValue = false
				default:
					rawElem := unmaskString(formatScalarValue(e), placeholders)
					if strings.Contains(rawElem, ",") || strings.Contains(rawElem, "{{") || checkScalarClean(rawElem) != nil || strings.TrimSpace(rawElem) == "" {
						canUseValue = false
						break
					}
					items = append(items, strings.TrimSpace(rawElem))
				}
				if !canUseValue {
					break
				}
			}
			if canUseValue {
				out[path] = blueprint.Field{Value: strings.Join(items, ", ")}
			} else {
				jsonBytes, err := json.Marshal(sliceItems)
				if err != nil {
					if report != nil {
						report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path), "failed to serialize slice envelope")
					}
					continue
				}
				rawStr := unmaskString(string(jsonBytes), placeholders)
				if err := checkScalarClean(rawStr); err != nil {
					if report != nil {
						report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path),
							"multi-line scalar contains newlines, which is not supported in blueprint values")
					}
					continue
				}
				out[path] = blueprint.Field{Raw: rawStr}
			}
		case string:
			rawStr := unmaskString(val, placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				if report != nil {
					report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path),
						"multi-line scalar contains newlines, which is not supported in blueprint values")
				}
				continue
			}
			if pName := matchParamVar(rawStr); pName != "" {
				if isWholeObjectParamForFields(bp, pName, path, out) {
					if report != nil {
						report.Record("resource."+resName+".spec."+path, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", pName))
					}
				} else if isValidParamIdentifier(pName) {
					out[path] = blueprint.Field{From: "params." + pName}
				} else if report != nil {
					report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path), "invalid parameter reference")
				}
			} else if key := matchEnvVar(rawStr); key != "" {
				if isFlatParamIdentifier(key) {
					out[path] = blueprint.Field{From: "env." + key}
					if bp != nil {
						ensureEnvDeclared(bp, key, "string")
					}
				} else if report != nil {
					if strings.Contains(key, ".") {
						report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path), fmt.Sprintf("unsupported nested environment variable %q: environment keys must be flat camelCase identifiers", key))
					} else {
						report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path), "invalid environment reference")
					}
				}
			} else if strings.Contains(rawStr, "{{") {
				out[path] = blueprint.Field{Raw: rawStr}
			} else {
				out[path] = blueprint.Field{Value: rawStr}
			}
		case nil:
			continue
		default:
			rawStr := unmaskString(formatScalarValue(val), placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				if report != nil {
					report.Record(fmt.Sprintf("resource.%s.envelope.%s", resName, path),
						"contains newlines or control characters")
				}
				continue
			}
			out[path] = blueprint.Field{Value: rawStr}
		}
	}
}

func isMapFieldPrefix(prefix, nextKey string) bool {
	lower := strings.ToLower(prefix)
	if prefix == "data" || prefix == "stringData" || prefix == "binaryData" || lower == "tags" {
		return true
	}
	if prefix == "spec.selector" {
		if nextKey == "matchLabels" || nextKey == "matchExpressions" {
			return false
		}
		return true
	}
	if strings.HasSuffix(lower, "labels") || strings.HasSuffix(lower, "annotations") || strings.HasSuffix(lower, "tags") || strings.HasSuffix(lower, "matchlabels") || strings.HasSuffix(lower, "nodeselector") {
		return true
	}
	return false
}

func normalizeMapFieldPath(fieldPath string) string {
	fieldPath = reBracketQuoted.ReplaceAllString(fieldPath, "[$1]")
	if strings.HasSuffix(fieldPath, "]") {
		return fieldPath
	}
	idx := strings.LastIndex(fieldPath, ".")
	if idx == -1 {
		return fieldPath
	}
	prefix := fieldPath[:idx]
	key := fieldPath[idx+1:]
	if isMapFieldPrefix(prefix, key) {
		return fmt.Sprintf("%s[%s]", prefix, key)
	}
	return fieldPath
}

func isMapField(leaves []schema.Leaf, path string) bool {
	for _, l := range leaves {
		if l.Path == path && l.Node.Type == "map" {
			return true
		}
	}
	return false
}

func extractFields(prefix string, obj map[string]any, out map[string]blueprint.Field, placeholders []string, resName string, report *LossReport, nameMapping map[string]string, bp *blueprint.Blueprint, isNative bool) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := obj[k]
		if rePlaceholder.MatchString(k) {
			rawK := unmaskString(k, placeholders)
			path := rawK
			if prefix != "" {
				if isMapFieldPrefix(prefix, rawK) {
					path = fmt.Sprintf("%s[%s]", prefix, rawK)
				} else {
					path = prefix + "." + rawK
				}
			}
			report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "dynamic map key with template expression is not supported in blueprint")
			continue
		}
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
			extractFields(path, val, out, placeholders, resName, report, nameMapping, bp, isNative)
		case string:
			rawStr := unmaskString(val, placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path),
					"multi-line scalar contains newlines, which is not supported in blueprint values")
				continue
			}
			trimmed := strings.TrimSpace(rawStr)
			if pName := matchParamVar(rawStr); pName != "" {
				if isWholeObjectParamForFields(bp, pName, path, out) {
					if report != nil {
						report.Record("resource."+resName+".spec."+path, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", pName))
					}
				} else if isValidParamIdentifier(pName) {
					out[path] = blueprint.Field{From: "params." + pName}
					if isAdoptMapField(path) {
						ensureParamDeclaredTyped(bp, pName, "object")
					}
				} else {
					report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "invalid parameter reference")
				}
			} else if key := matchEnvVar(rawStr); key != "" {
				if isFlatParamIdentifier(key) {
					out[path] = blueprint.Field{From: "env." + key}
					if bp != nil {
						ensureEnvDeclared(bp, key, "string")
					}
				} else {
					if strings.Contains(key, ".") {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), fmt.Sprintf("unsupported nested environment variable %q: environment keys must be flat camelCase identifiers", key))
					} else {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path), "invalid environment reference")
					}
				}
			} else if srcRes, targetKind, targetField, ok := matchObservedStatus(trimmed); ok {
				if nameMapping != nil && nameMapping[srcRes] != "" {
					srcRes = nameMapping[srcRes]
				} else {
					srcRes = normalizeDNSLabel(srcRes)
				}
				if targetKind == "metadata" && targetField != "name" {
					out[path] = blueprint.Field{Raw: rawStr}
				} else {
					out[path] = blueprint.Field{From: "resources." + srcRes + "." + targetKind + "." + targetField}
				}
			} else if m := reXRResourceRef.FindStringSubmatch(trimmed); len(m) >= 2 && m[0] == trimmed {
				srcRes := m[1]
				if nameMapping != nil && nameMapping[srcRes] != "" {
					srcRes = nameMapping[srcRes]
				} else {
					srcRes = normalizeDNSLabel(srcRes)
				}
				out[path] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
			} else if tmplName := matchTemplateInclude(rawStr); tmplName != "" && !isNative {
				if templateExists(bp, tmplName) {
					out[path] = blueprint.Field{Template: tmplName}
				} else {
					if report != nil {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path),
							fmt.Sprintf("template %q referenced by include is not defined", tmplName))
					}
					out[path] = blueprint.Field{Raw: rawStr}
				}
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
					extractFields(elemPath, elemVal, out, placeholders, resName, report, nameMapping, bp, isNative)
				case string:
					rawStr := unmaskString(elemVal, placeholders)
					if err := checkScalarClean(rawStr); err != nil {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath),
							"multi-line scalar contains newlines, which is not supported in blueprint values")
						continue
					}
					trimmed := strings.TrimSpace(rawStr)
					if pName := matchParamVar(rawStr); pName != "" {
						if isWholeObjectParamForFields(bp, pName, elemPath, out) {
							if report != nil {
								report.Record("resource."+resName+".spec."+elemPath, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", pName))
							}
						} else if isValidParamIdentifier(pName) {
							out[elemPath] = blueprint.Field{From: "params." + pName}
						} else {
							report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath), "invalid parameter reference")
						}
					} else if key := matchEnvVar(rawStr); key != "" {
						if isFlatParamIdentifier(key) {
							out[elemPath] = blueprint.Field{From: "env." + key}
							if bp != nil {
								ensureEnvDeclared(bp, key, "string")
							}
						} else {
							if strings.Contains(key, ".") {
								report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath), fmt.Sprintf("unsupported nested environment variable %q: environment keys must be flat camelCase identifiers", key))
							} else {
								report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath), "invalid environment reference")
							}
						}
					} else if srcRes, targetKind, targetField, ok := matchObservedStatus(trimmed); ok {
						if nameMapping != nil && nameMapping[srcRes] != "" {
							srcRes = nameMapping[srcRes]
						} else {
							srcRes = normalizeDNSLabel(srcRes)
						}
						if targetKind == "metadata" && targetField != "name" {
							out[elemPath] = blueprint.Field{Raw: rawStr}
						} else {
							out[elemPath] = blueprint.Field{From: "resources." + srcRes + "." + targetKind + "." + targetField}
						}
					} else if m := reXRResourceRef.FindStringSubmatch(trimmed); len(m) >= 2 && m[0] == trimmed {
						srcRes := m[1]
						if nameMapping != nil && nameMapping[srcRes] != "" {
							srcRes = nameMapping[srcRes]
						} else {
							srcRes = normalizeDNSLabel(srcRes)
						}
						out[elemPath] = blueprint.Field{From: "resources." + srcRes + ".metadata.name"}
					} else if tmplName := matchTemplateInclude(rawStr); tmplName != "" && !isNative {
						if templateExists(bp, tmplName) {
							out[elemPath] = blueprint.Field{Template: tmplName}
						} else {
							if report != nil {
								report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath),
									fmt.Sprintf("template %q referenced by include is not defined", tmplName))
							}
							out[elemPath] = blueprint.Field{Raw: rawStr}
						}
					} else if strings.Contains(rawStr, "{{") {
						out[elemPath] = blueprint.Field{Raw: rawStr}
					} else {
						out[elemPath] = blueprint.Field{Value: rawStr}
					}
				default:
					rawStr := unmaskString(formatScalarValue(elemVal), placeholders)
					if err := checkScalarClean(rawStr); err != nil {
						report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, elemPath),
							"contains newlines or control characters")
						continue
					}
					out[elemPath] = blueprint.Field{Value: rawStr}
				}
			}
		default:
			rawStr := unmaskString(formatScalarValue(val), placeholders)
			if err := checkScalarClean(rawStr); err != nil {
				report.Record(fmt.Sprintf("resource.%s.fields.%s", resName, path),
					"contains newlines or control characters")
				continue
			}
			out[path] = blueprint.Field{Value: rawStr}
		}
	}
}

// formatScalarValue stringifies a scalar value while preserving whole numbers
// in standard decimal notation rather than scientific notation (e.g. 1209600 -> "1209600").
func formatScalarValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case float64:
		if v >= -1<<53 && v <= 1<<53 && v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		if float64(v) == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case json.Number:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(val)
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
	if bp == nil || len(nameMapping) == 0 {
		return
	}

	type renamePair struct {
		from string
		to   string
	}
	var renames []renamePair
	for from, to := range nameMapping {
		if from != "" && to != "" && from != to {
			renames = append(renames, renamePair{from: from, to: to})
		}
	}
	if len(renames) == 0 {
		return
	}
	sort.Slice(renames, func(i, j int) bool {
		if len(renames[i].from) != len(renames[j].from) {
			return len(renames[i].from) > len(renames[j].from)
		}
		return renames[i].from < renames[j].from
	})

	for i := range bp.Spec.Resources {
		r := &bp.Spec.Resources[i]
		if r.ForEach != "" {
			r.ForEach = rewriteFromWire(r.ForEach, nameMapping)
		}
		for fName, f := range r.Fields {
			if f.From != "" {
				f.From = rewriteFromWire(f.From, nameMapping)
				r.Fields[fName] = f
			}
			if f.Raw != "" {
				for _, rn := range renames {
					if rawReferencesResource(f.Raw, rn.from) {
						f.Raw = rewriteRawResource(f.Raw, rn.from, rn.to)
					}
				}
				r.Fields[fName] = f
			}
		}
		for aName, a := range r.Annotations {
			if a.From != "" {
				a.From = rewriteFromWire(a.From, nameMapping)
				r.Annotations[aName] = a
			}
			if a.Raw != "" {
				for _, rn := range renames {
					if rawReferencesResource(a.Raw, rn.from) {
						a.Raw = rewriteRawResource(a.Raw, rn.from, rn.to)
					}
				}
				r.Annotations[aName] = a
			}
		}
		for eName, e := range r.Envelope {
			if e.From != "" {
				e.From = rewriteFromWire(e.From, nameMapping)
				r.Envelope[eName] = e
			}
			if e.Raw != "" {
				for _, rn := range renames {
					if rawReferencesResource(e.Raw, rn.from) {
						e.Raw = rewriteRawResource(e.Raw, rn.from, rn.to)
					}
				}
				r.Envelope[eName] = e
			}
		}
	}
	for tName, body := range bp.Spec.Templates {
		for _, rn := range renames {
			if rawReferencesResource(body, rn.from) {
				body = rewriteRawResource(body, rn.from, rn.to)
			}
		}
		bp.Spec.Templates[tName] = body
	}
}

// rawReferencesResource checks whether a raw template/expression string contains
// references to the given resource name.
func rawReferencesResource(raw, name string) bool {
	if raw == "" || name == "" {
		return false
	}
	q := regexp.QuoteMeta(name)
	reDotted := regexp.MustCompile(`(?:(?:\$|\$\.|\.)?observed\.resources|resources)\.` + q + `($|[^a-zA-Z0-9_-])`)
	if reDotted.MatchString(raw) {
		return true
	}
	for _, quote := range []string{`\"`, `\'`, `"`, `'`, "`"} {
		reIndex := regexp.MustCompile(`\bindex\s+(?:(?:\$|\$\.|\.)?(?:observed\.)?resources)\s+` + regexp.QuoteMeta(quote) + q + regexp.QuoteMeta(quote) + `($|[^a-zA-Z0-9_-])`)
		if reIndex.MatchString(raw) {
			return true
		}
		reHasKey := regexp.MustCompile(`\bhasKey\s+(?:(?:\$|\$\.|\.)?(?:observed\.)?resources)\s+` + regexp.QuoteMeta(quote) + q + regexp.QuoteMeta(quote) + `($|[^a-zA-Z0-9_-])`)
		if reHasKey.MatchString(raw) {
			return true
		}
		reGetComposed := regexp.MustCompile(`\bgetComposedResource\s+[^\s"'` + "`" + `\\]+\s+` + regexp.QuoteMeta(quote) + q + regexp.QuoteMeta(quote) + `($|[^a-zA-Z0-9_-])`)
		if reGetComposed.MatchString(raw) {
			return true
		}
		reDig := regexp.MustCompile(`\bdig\s+(?:"resources"|'resources'|` + "`resources`" + `|\\"resources\\"|\\'resources\\')\s+` + regexp.QuoteMeta(quote) + q + regexp.QuoteMeta(quote) + `($|[^a-zA-Z0-9_-])`)
		if reDig.MatchString(raw) {
			return true
		}
	}
	return false
}

// rewriteRawResource replaces references to from with to in a raw template/expression.
func rewriteRawResource(raw, from, to string) string {
	if raw == "" || from == "" || to == "" || from == to {
		return raw
	}
	r := raw
	reDotted := regexp.MustCompile(`((?:(?:\$|\$\.|\.)?observed\.resources|resources)\.)` + regexp.QuoteMeta(from) + `($|[^a-zA-Z0-9_-])`)
	r = reDotted.ReplaceAllString(r, "${1}"+to+"${2}")

	for _, quote := range []string{`\"`, `\'`, `"`, `'`, "`"} {
		reIndex := regexp.MustCompile(`(\bindex\s+(?:(?:\$|\$\.|\.)?(?:observed\.)?resources)\s+` + regexp.QuoteMeta(quote) + `)` + regexp.QuoteMeta(from) + `(` + regexp.QuoteMeta(quote) + `)`)
		r = reIndex.ReplaceAllString(r, "${1}"+to+"${2}")

		reHasKey := regexp.MustCompile(`(\bhasKey\s+(?:(?:\$|\$\.|\.)?(?:observed\.)?resources)\s+` + regexp.QuoteMeta(quote) + `)` + regexp.QuoteMeta(from) + `(` + regexp.QuoteMeta(quote) + `)`)
		r = reHasKey.ReplaceAllString(r, "${1}"+to+"${2}")

		reGetComposed := regexp.MustCompile(`(\bgetComposedResource\s+[^\s"'` + "`" + `\\]+\s+` + regexp.QuoteMeta(quote) + `)` + regexp.QuoteMeta(from) + `(` + regexp.QuoteMeta(quote) + `)`)
		r = reGetComposed.ReplaceAllString(r, "${1}"+to+"${2}")

		reDig := regexp.MustCompile(`(\bdig\s+(?:"resources"|'resources'|` + "`resources`" + `|\\"resources\\"|\\'resources\\')\s+` + regexp.QuoteMeta(quote) + `)` + regexp.QuoteMeta(from) + `(` + regexp.QuoteMeta(quote) + `)`)
		r = reDig.ReplaceAllString(r, "${1}"+to+"${2}")
	}
	return r
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
	seenProviders := make(map[string]bool)
	seenCRDs := make(map[string]bool)
	var newSources []blueprint.Source
	for _, s := range bp.Spec.Sources {
		if s.Provider != "" {
			if !seenProviders[s.Provider] {
				seenProviders[s.Provider] = true
				newSources = append(newSources, s)
			}
		} else if s.CRDs != "" {
			if !seenCRDs[s.CRDs] {
				seenCRDs[s.CRDs] = true
				newSources = append(newSources, s)
			}
		}
	}
	for _, r := range bp.Spec.Resources {
		if r.Provider == "" || r.Provider == blueprint.NativeProvider || r.Provider == "cluster" {
			continue
		}
		lower := strings.ToLower(r.Provider)
		if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || seenCRDs[r.Provider] {
			continue
		}
		if !seenProviders[r.Provider] {
			seenProviders[r.Provider] = true
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

func pruneUnknownForProviderFields(bp *blueprint.Blueprint, opts Options, report *LossReport) {
	if bp == nil || opts.Store == nil {
		return
	}
	wantNamespaced := bp.Spec.XRD.Scope == "Namespaced" || bp.Spec.XRD.Scope == ""

	var allStoreCRDs []schema.CRD
	allStoreCRDsLoaded := false
	getAllStoreCRDs := func() []schema.CRD {
		if allStoreCRDsLoaded {
			return allStoreCRDs
		}
		allStoreCRDsLoaded = true
		if list, err := opts.Store.List(); err == nil {
			for _, ref := range list {
				if got, err := opts.Store.Load(ref); err == nil {
					allStoreCRDs = append(allStoreCRDs, got...)
				}
			}
		}
		return allStoreCRDs
	}

	for i := range bp.Spec.Resources {
		r := &bp.Spec.Resources[i]
		if r.Provider == blueprint.NativeProvider || strings.HasSuffix(r.Provider, ".yaml") || strings.HasSuffix(r.Provider, ".yml") {
			continue
		}
		var crd *schema.CRD
		if r.Provider != "" {
			if got, err := opts.Store.Load(r.Provider); err == nil {
				crd = resolveResourceCRD(got, *r, wantNamespaced)
			}
		}
		if crd == nil {
			crd = resolveResourceCRD(getAllStoreCRDs(), *r, wantNamespaced)
		}
		if crd == nil {
			continue
		}

		nodes, err := crd.ForProvider()
		if err != nil || len(nodes) == 0 {
			continue
		}

		leaves := schema.Leaves(nodes, "")
		known := make(map[string]bool, len(leaves)*2)
		for _, l := range leaves {
			known[l.Path] = true
			for _, ancestor := range ancestorPaths(l.Path) {
				known[ancestor] = true
			}
		}

		var fNames []string
		for k := range r.Fields {
			if !strings.HasPrefix(k, "metadata.") {
				fNames = append(fNames, k)
			}
		}
		sort.Strings(fNames)

		for _, fieldPath := range fNames {
			basePath, _, isMap := blueprint.ParseFieldPath(fieldPath)
			lookup := reArrayIdx.ReplaceAllString(fieldPath, "[0]")
			if isMap {
				lookup = reArrayIdx.ReplaceAllString(basePath, "[0]")
			}
			if known[lookup] || (isMap && (known[basePath] || known[reArrayIdx.ReplaceAllString(basePath, "[0]")])) {
				continue
			}

			// Dot-notation map field: convert to bracket notation if map property exists in CRD schema
			if !isMap && strings.Contains(fieldPath, ".") {
				lastDot := strings.LastIndex(fieldPath, ".")
				prefix := fieldPath[:lastDot]
				key := fieldPath[lastDot+1:]
				lookupPrefix := reArrayIdx.ReplaceAllString(prefix, "[0]")
				if isMapField(leaves, lookupPrefix) || isMapFieldPrefix(prefix, key) {
					newPath := fmt.Sprintf("%s[%s]", prefix, key)
					r.Fields[newPath] = r.Fields[fieldPath]
					delete(r.Fields, fieldPath)
					continue
				}
			}

			report.Record(
				fmt.Sprintf("resource.%s.fields.%s", r.Name, fieldPath),
				fmt.Sprintf("field %q is not in %s spec.forProvider (unknown field pruned by schema)", fieldPath, crd.Kind),
			)
			delete(r.Fields, fieldPath)
		}
	}
}

func matchesParamRef(s string, paramName, memberName string) bool {
	if s == "" {
		return false
	}
	fullPath := paramName
	if memberName != "" {
		fullPath = paramName + "." + memberName
	}

	// 1. Direct or descendant reference to fullPath:
	// e.g. $spec.network.vpc.id or params.network.vpc.id (or $spec.network.vpc if memberName is "vpc")
	reMember := regexp.MustCompile(`(?:\$spec|\$?[.]spec|params|\$?[.]observed\.composite\.resource\.spec)\.` + regexp.QuoteMeta(fullPath) + `\b`)
	if reMember.MatchString(s) {
		return true
	}
	reIndexMember := regexp.MustCompile(`\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']` + regexp.QuoteMeta(fullPath) + `["']`)
	if reIndexMember.MatchString(s) {
		return true
	}

	// 2. Direct reference to any ancestor object without trailing dot:
	// e.g. if fullPath is "network.vpc.id", an unadorned reference to $spec.network or $spec.network.vpc
	// references the entire object, which transitively references all members.
	parts := strings.Split(fullPath, ".")
	if len(parts) > 1 {
		for i := 1; i < len(parts); i++ {
			ancestor := strings.Join(parts[:i], ".")
			reAncestor := regexp.MustCompile(`(?:\$spec|\$?[.]spec|params|\$?[.]observed\.composite\.resource\.spec)\.` + regexp.QuoteMeta(ancestor) + `\b`)
			locs := reAncestor.FindAllStringIndex(s, -1)
			for _, loc := range locs {
				end := loc[1]
				if end >= len(s) || s[end] != '.' {
					return true
				}
			}
			reIndexAncestor := regexp.MustCompile(`\(?\s*index\s+\(?\s*(?:\$spec|\$?[.]spec|\$?[.]observed\.composite\.resource\.spec)\s*\)?\s+["']` + regexp.QuoteMeta(ancestor) + `["']`)
			if reIndexAncestor.MatchString(s) {
				return true
			}
		}
	}

	return false
}

func isParameterReferenced(bp *blueprint.Blueprint, paramName string, memberName string) bool {
	targetFrom := "params." + paramName
	if memberName != "" {
		targetFrom = "params." + paramName + "." + memberName
	}

	matchesFrom := func(from string) bool {
		if from == "" || !strings.HasPrefix(from, "params.") {
			return false
		}
		if from == targetFrom || strings.HasPrefix(targetFrom, from+".") || strings.HasPrefix(from, targetFrom+".") {
			return true
		}
		return false
	}

	checkField := func(f blueprint.Field) bool {
		if matchesFrom(f.From) {
			return true
		}
		if f.Raw != "" && matchesParamRef(f.Raw, paramName, memberName) {
			return true
		}
		if f.Template != "" && matchesParamRef(f.Template, paramName, memberName) {
			return true
		}
		return false
	}

	for _, r := range bp.Spec.Resources {
		if r.ForEach != "" {
			if matchesFrom(r.ForEach) || matchesParamRef(r.ForEach, paramName, memberName) {
				return true
			}
		}
		if r.When != "" {
			source, param, _, _, err := blueprint.ParseWhen(r.When)
			if err == nil && (source == "params" || source == "") {
				whenFrom := "params." + param
				if matchesFrom(whenFrom) {
					return true
				}
			}
			if matchesParamRef(r.When, paramName, memberName) {
				return true
			}
		}
		for _, f := range r.Fields {
			if checkField(f) {
				return true
			}
		}
		for _, f := range r.Envelope {
			if checkField(f) {
				return true
			}
		}
		for _, f := range r.Annotations {
			if checkField(f) {
				return true
			}
		}
	}

	for _, tmpl := range bp.Spec.Templates {
		if matchesParamRef(tmpl, paramName, memberName) {
			return true
		}
	}

	for _, env := range bp.Spec.Environment {
		if matchesParamRef(env.Default, paramName, memberName) {
			return true
		}
	}

	for _, s := range bp.Spec.Pipeline {
		if matchesParamRef(s.Input, paramName, memberName) {
			return true
		}
	}

	for _, c := range bp.Spec.Conventions {
		if matchesParamRef(c.Template, paramName, memberName) {
			return true
		}
	}

	return false
}

func pruneParameterProperties(
	bp *blueprint.Blueprint,
	paramName string,
	memberPrefix string,
	pathPrefix string,
	p *blueprint.Parameter,
	baseParam *blueprint.Parameter,
	report *LossReport,
) {
	if len(p.Properties) == 0 {
		return
	}

	memberNames := make([]string, 0, len(p.Properties))
	for m := range p.Properties {
		memberNames = append(memberNames, m)
	}
	sort.Strings(memberNames)

	for _, m := range memberNames {
		childParam := p.Properties[m]
		var childBaseParam *blueprint.Parameter
		if baseParam != nil && baseParam.Properties != nil {
			if bpProp, ok := baseParam.Properties[m]; ok {
				childBaseParam = &bpProp
			}
		}

		childMemberPath := m
		if memberPrefix != "" {
			childMemberPath = memberPrefix + "." + m
		}
		childReportPath := fmt.Sprintf("%s.properties.%s", pathPrefix, m)

		// Recurse into nested object properties first (bottom-up / post-order)
		if childParam.Type == "object" && len(childParam.Properties) > 0 {
			pruneParameterProperties(bp, paramName, childMemberPath, childReportPath, &childParam, childBaseParam, report)
			p.Properties[m] = childParam
		}

		if childBaseParam != nil {
			continue
		}

		if childParam.Type == "object" {
			if len(childParam.Properties) == 0 && !isParameterReferenced(bp, paramName, childMemberPath) {
				delete(p.Properties, m)
				report.Record(childReportPath, "parameter member orphaned by pruned unknown field dropped")
			}
		} else {
			if !isParameterReferenced(bp, paramName, childMemberPath) {
				delete(p.Properties, m)
				report.Record(childReportPath, "parameter member orphaned by pruned unknown field dropped")
			}
		}
	}
}

func pruneOrphanedParameters(bp *blueprint.Blueprint, baseBP *blueprint.Blueprint, report *LossReport) {
	if bp == nil || bp.Spec.XRD.Parameters == nil {
		return
	}

	paramNames := make([]string, 0, len(bp.Spec.XRD.Parameters))
	for n := range bp.Spec.XRD.Parameters {
		paramNames = append(paramNames, n)
	}
	sort.Strings(paramNames)

	for _, name := range paramNames {
		if name == "providerName" {
			continue
		}
		var baseParam *blueprint.Parameter
		if baseBP != nil && baseBP.Spec.XRD.Parameters != nil {
			if bpParam, ok := baseBP.Spec.XRD.Parameters[name]; ok {
				baseParam = &bpParam
			}
		}

		p := bp.Spec.XRD.Parameters[name]
		if p.Type == "object" && len(p.Properties) > 0 {
			pruneParameterProperties(bp, name, "", "xrd.parameters."+name, &p, baseParam, report)

			if baseParam != nil {
				bp.Spec.XRD.Parameters[name] = p
				continue
			}

			if len(p.Properties) == 0 && !isParameterReferenced(bp, name, "") {
				delete(bp.Spec.XRD.Parameters, name)
				report.Record("xrd.parameters."+name, "parameter orphaned by pruned unknown field dropped")
			} else {
				bp.Spec.XRD.Parameters[name] = p
			}
			continue
		}

		if baseParam != nil {
			continue
		}

		if !isParameterReferenced(bp, name, "") {
			delete(bp.Spec.XRD.Parameters, name)
			report.Record("xrd.parameters."+name, "parameter orphaned by pruned unknown field dropped")
		}
	}
}

func ancestorPaths(path string) []string {
	var out []string
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			prefix := path[:i]
			out = append(out, prefix)
			if trimmed, found := strings.CutSuffix(prefix, "[0]"); found {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

// cleanDependencyVersion extracts the first concrete semver tag from a version constraint
// (e.g. ">=v1.14.0 <v2.0.0" -> "v1.14.0", "=v0.4.0" -> "v0.4.0").
// If no valid tag can be extracted, it returns empty string so that the package ref is not corrupted.
func cleanDependencyVersion(ver string) string {
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return ""
	}
	fields := strings.FieldsFunc(ver, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';'
	})
	for _, f := range fields {
		f = strings.TrimPrefix(f, "=")
		f = strings.TrimLeft(f, ">=<~^ ")
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		valid := true
		for i, r := range f {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				continue
			}
			if (r == '.' || r == '-') && i > 0 {
				continue
			}
			valid = false
			break
		}
		if valid {
			return f
		}
	}
	return ""
}
