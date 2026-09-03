package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
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

	for _, file := range yamlFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		docs, err := splitYAML(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse yaml %s: %w", file, err)
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
			}
		}
	}

	if len(compDocs) == 0 {
		return nil, nil, fmt.Errorf("no Composition document found in tree %s", dirPath)
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
					if depKind == "Provider" || (depKind == "" && pkg != "") {
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

	// 2. Process XRD definitions
	for _, xrdDoc := range xrdDocs {
		parseXRDDoc(xrdDoc, bp, report)
	}

	// 3. Process Compositions
	defaultProvider := opts.DefaultProviderRef
	if defaultProvider == "" && len(bp.Spec.Sources) > 0 && bp.Spec.Sources[0].Provider != "" {
		defaultProvider = bp.Spec.Sources[0].Provider
	}

	nameMapping := make(map[string]string)
	for _, compDoc := range compDocs {
		if bp.Metadata.Name == "" {
			if meta, ok := compDoc["metadata"].(map[string]any); ok {
				if name, ok := meta["name"].(string); ok && name != "" {
					bp.Metadata.Name = name
				}
			}
		}
		spec, ok := compDoc["spec"].(map[string]any)
		if !ok {
			continue
		}
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
		}
		if pipeline, ok := spec["pipeline"].([]any); ok && len(pipeline) > 0 {
			if err := parsePipelineComposition(pipeline, bp, defaultProvider, report, nameMapping); err != nil {
				return nil, nil, err
			}
		} else if resources, ok := spec["resources"].([]any); ok && len(resources) > 0 {
			if err := parseClassicComposition(resources, bp, defaultProvider, report, nameMapping); err != nil {
				return nil, nil, err
			}
		}
	}

	// 4. Set defaults for any missing XRD fields
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

	// 5. Rewrite status references and finalize sources
	rewriteStatusReferences(bp, nameMapping)
	collectSources(bp, defaultProvider)

	// Sort resources by name for deterministic ordering
	sort.Slice(bp.Spec.Resources, func(i, j int) bool {
		return bp.Spec.Resources[i].Name < bp.Spec.Resources[j].Name
	})

	if err := bp.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate adopted blueprint: %w", err)
	}

	return bp, report, nil
}
