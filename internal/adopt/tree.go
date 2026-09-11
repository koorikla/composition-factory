package adopt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
)

// AdoptTree walks a Configuration package source tree directory containing
// crossplane.yaml, apis/<xr>/definition.yaml, and composition.yaml, extracting
// package metadata, provider dependencies, XRD schemas/parameters, resource templates,
// field wires, and pipeline steps into a canonical Blueprint.
func AdoptTree(dirPath string, opts Options) (*blueprint.Blueprint, *LossReport, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, nil, fmt.Errorf("stat directory %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dirPath)
	}

	var yamlFiles []string
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") && name != "." && name != ".." {
				return filepath.SkipDir
			}
			if name == "templates" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			yamlFiles = append(yamlFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("walk directory %s: %w", dirPath, err)
	}

	sort.Strings(yamlFiles)

	report := &LossReport{}
	var configDocs []map[string]any
	var xrdDocs []map[string]any
	var compDocs []map[string]any
	var envConfigDocs []map[string]any

	if opts.SourceDir == "" {
		opts.SourceDir = dirPath
	}
	if opts.FunctionPackages == nil {
		opts.FunctionPackages = make(map[string]string)
	}
	if opts.Store == nil && opts.CacheDir != "" {
		opts.Store = cache.New(opts.CacheDir)
	}
	if lock, _ := cache.ReadLock(filepath.Join(dirPath, ".cf.lock")); lock != nil {
		for _, f := range lock.Functions {
			opts.FunctionPackages[f.Ref] = f.Ref
			clean := f.Ref
			if i := strings.Index(clean, "@"); i >= 0 {
				clean = clean[:i]
			}
			if i := strings.LastIndex(clean, ":"); i >= 0 {
				clean = clean[:i]
			}
			if i := strings.LastIndex(clean, "/"); i >= 0 {
				clean = clean[i+1:]
			}
			if clean != "" {
				opts.FunctionPackages[clean] = f.Ref
			}
		}
	}

	for _, file := range yamlFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		docs, err := splitYAML(data)
		if err != nil {
			report.Record(file, fmt.Sprintf("skipped unparseable YAML: %v", err))
			continue
		}
		docs = unwrapListDocs(docs)
		ScrubDocuments(docs, report)
		for _, doc := range docs {
			kind, _ := doc["kind"].(string)
			switch kind {
			case "Configuration":
				configDocs = append(configDocs, doc)
			case "CompositeResourceDefinition":
				xrdDocs = append(xrdDocs, doc)
			case "Composition":
				compDocs = append(compDocs, doc)
			case "EnvironmentConfig":
				envConfigDocs = append(envConfigDocs, doc)
			case "Function":
				if meta, ok := doc["metadata"].(map[string]any); ok {
					fnName, _ := meta["name"].(string)
					if fSpec, ok := doc["spec"].(map[string]any); ok {
						if pkg, ok := fSpec["package"].(string); ok && fnName != "" {
							opts.FunctionPackages[fnName] = pkg
						}
					}
				}
			}
		}
	}

	if len(compDocs) == 0 {
		return nil, nil, fmt.Errorf("no Composition document found in tree %s", dirPath)
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
			return nil, nil, fmt.Errorf("composition %q not found in tree %s (available: %s)", targetComp, dirPath, strings.Join(compNames, ", "))
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
			return nil, nil, fmt.Errorf("ambiguous multi-composition input: found %d Compositions in tree %s (%s); specify a target composition to adopt", len(compDocs), dirPath, strings.Join(compNames, ", "))
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

	// 1. Process Configuration package metadata and dependencies from crossplane.yaml
	for _, cfgDoc := range configDocs {
		if meta, ok := cfgDoc["metadata"].(map[string]any); ok {
			if name, ok := meta["name"].(string); ok && name != "" {
				bp.Metadata.Name = name
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
					if pkg == "" {
						if f, ok := dep["function"].(string); ok && f != "" {
							pkg = f
							depKind = "Function"
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
						cleanVer := strings.TrimPrefix(ver, "=")
						cleanVer = strings.TrimLeft(cleanVer, ">=<~^ ")
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
					if depKind == "Provider" || (depKind == "" && pkg != "" && dep["function"] == nil) {
						providerRef := pkg
						if ver != "" && !strings.Contains(providerRef, ":") && !strings.Contains(providerRef, "@") {
							cleanVer := strings.TrimPrefix(ver, "=")
							cleanVer = strings.TrimLeft(cleanVer, ">=<~^ ")
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

	// 2. Process metadata and compositeTypeRef from the chosen Composition
	if meta, ok := compDoc["metadata"].(map[string]any); ok {
		if bp.Metadata.Name == "" {
			if name, ok := meta["name"].(string); ok && name != "" {
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
	spec, _ := compDoc["spec"].(map[string]any)
	if spec != nil {
		checkCompositionSpecFields(spec, report)
		if ctr, ok := spec["compositeTypeRef"].(map[string]any); ok {
			if k, ok := ctr["kind"].(string); ok && bp.Spec.XRD.Kind == "" {
				bp.Spec.XRD.Kind = k
			}
			if av, ok := ctr["apiVersion"].(string); ok {
				parts := strings.Split(av, "/")
				if len(parts) == 2 {
					if bp.Spec.XRD.Group == "" {
						bp.Spec.XRD.Group = parts[0]
					}
					if bp.Spec.XRD.Version == "" {
						bp.Spec.XRD.Version = parts[1]
					}
				} else if bp.Spec.XRD.Version == "" {
					bp.Spec.XRD.Version = av
				}
			}
			if p, ok := ctr["plural"].(string); ok && p != "" && bp.Spec.XRD.Plural == "" {
				bp.Spec.XRD.Plural = p
			}
		}
	}

	// 3. Process XRD definitions matching compositeTypeRef
	var matchedXRD map[string]any
	if len(xrdDocs) == 1 {
		matchedXRD = xrdDocs[0]
	} else if len(xrdDocs) > 1 {
		for _, xd := range xrdDocs {
			if xSpec, ok := xd["spec"].(map[string]any); ok {
				if names, ok := xSpec["names"].(map[string]any); ok {
					if k, _ := names["kind"].(string); k != "" && k == bp.Spec.XRD.Kind {
						matchedXRD = xd
						break
					}
				}
			}
		}
		if matchedXRD == nil {
			matchedXRD = xrdDocs[0]
		}
	}
	if matchedXRD != nil {
		parseXRDDoc(matchedXRD, bp, report)
	}

	// 4. Process Composition resources/pipeline
	defaultProvider := opts.DefaultProviderRef
	if defaultProvider == "" && len(bp.Spec.Sources) > 0 && bp.Spec.Sources[0].Provider != "" {
		defaultProvider = bp.Spec.Sources[0].Provider
	}

	nameMapping := make(map[string]string)
	if spec != nil {
		if pipeline, ok := spec["pipeline"].([]any); ok && len(pipeline) > 0 {
			if err := parsePipelineComposition(pipeline, bp, opts, report, nameMapping, len(xrdDocs) > 0); err != nil {
				return nil, nil, err
			}
		} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
			patchSets, _ := spec["patchSets"].([]any)
			if err := parseClassicComposition(resources, patchSets, bp, opts, report, nameMapping); err != nil {
				return nil, nil, err
			}
		}
	}

	parseEnvironmentConfigDocs(envConfigDocs, bp, report)
	if len(bp.Spec.Environment) == 0 && len(bp.Spec.EnvironmentConfigs) > 0 {
		for _, cfg := range bp.Spec.EnvironmentConfigs {
			report.Record(fmt.Sprintf("environmentConfig.%s", cfg.Name), "EnvironmentConfig declared without any environment keys")
		}
		bp.Spec.EnvironmentConfigs = nil
	}

	// 5. Set defaults for any missing XRD fields
	if bp.Metadata.Name == "" {
		bp.Metadata.Name = "adopted-composition"
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
	if bp.Spec.XRD.Scope == "Namespaced" {
		if _, ok := bp.Spec.XRD.Parameters["providerName"]; !ok {
			bp.Spec.XRD.Parameters["providerName"] = blueprint.Parameter{
				Type:        "string",
				Required:    true,
				Description: "Crossplane ProviderConfig name to use for managed resources",
			}
			synthesized["providerName"] = true
		}
	}

	// 6. Rewrite status references and finalize sources
	rewriteStatusReferences(bp, nameMapping)

	collectSources(bp, defaultProvider)

	pruneUnknownForProviderFields(bp, opts, report)

	if len(xrdDocs) == 0 {
		pruneOrphanedParameters(bp, opts.BaseBlueprint, report)
	}

	// No XRD in the tree: the parameters were inferred from their uses in the
	// Compositions. Settle what they prove and name the rest as lost.
	if len(xrdDocs) == 0 {
		applyXRDlessEvidence(bp, []map[string]any{compDoc}, synthesized, report, opts.BaseBlueprint, opts.Store)
	}

	if err := bp.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return bp, report, nil
}
