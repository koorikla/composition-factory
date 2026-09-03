package adopt

import (
	"fmt"
)

var serverSideMetadataKeys = []string{
	"creationTimestamp",
	"uid",
	"resourceVersion",
	"generation",
	"managedFields",
	"selfLink",
}

var serverSideAnnotationKeys = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"argocd.argoproj.io/tracking-id",
}

// ScrubDocuments iterates over a slice of parsed YAML document maps and scrubs
// server-side metadata, runtime status blocks, and annotations from each document.
// Dropped fields are recorded in the provided LossReport.
func ScrubDocuments(docs []map[string]any, report *LossReport) int {
	total := 0
	for _, doc := range docs {
		total += ScrubDocument(doc, "", report)
	}
	return total
}

// ScrubDocument systematically scrubs server-side metadata (creationTimestamp, uid,
// resourceVersion, generation, managedFields, selfLink), kubectl annotations,
// and runtime status blocks from a parsed Kubernetes object map and its nested resources.
// It records every dropped field into the LossReport and returns the count of removed items.
func ScrubDocument(doc map[string]any, pathPrefix string, report *LossReport) int {
	if doc == nil {
		return 0
	}

	removed := 0

	// 1. Scrub root or object-level 'status'
	if _, ok := doc["status"]; ok {
		delete(doc, "status")
		statusPath := "status"
		if pathPrefix != "" {
			statusPath = pathPrefix + ".status"
		}
		if report != nil {
			report.Record(statusPath, "scrubbed runtime status block")
		}
		removed++
	}

	// 2. Scrub 'metadata'
	if metaVal, ok := doc["metadata"]; ok {
		if meta, ok := metaVal.(map[string]any); ok {
			metaPath := "metadata"
			if pathPrefix != "" {
				metaPath = pathPrefix + ".metadata"
			}

			// Metadata keys
			for _, k := range serverSideMetadataKeys {
				if _, exists := meta[k]; exists {
					delete(meta, k)
					if report != nil {
						report.Record(metaPath+"."+k, "scrubbed server-side metadata field")
					}
					removed++
				}
			}

			// Annotations
			if annVal, ok := meta["annotations"]; ok {
				if ann, ok := annVal.(map[string]any); ok {
					for _, annKey := range serverSideAnnotationKeys {
						if _, exists := ann[annKey]; exists {
							delete(ann, annKey)
							if report != nil {
								report.Record(fmt.Sprintf("%s.annotations[%s]", metaPath, annKey), "scrubbed server-side annotation")
							}
							removed++
						}
					}
					if len(ann) == 0 {
						delete(meta, "annotations")
					}
				}
			}
		}
	}

	// 3. Scrub nested resources within spec (Classic Composition & Patch-and-Transform)
	if spec, ok := doc["spec"].(map[string]any); ok {
		// Classic spec.resources
		if resources, ok := spec["resources"].([]any); ok {
			for _, item := range resources {
				if resMap, ok := item.(map[string]any); ok {
					resName, _ := resMap["name"].(string)
					subPrefix := "spec.resources"
					if resName != "" {
						subPrefix = "spec.resources." + resName
					}
					if pathPrefix != "" {
						subPrefix = pathPrefix + "." + subPrefix
					}
					if base, ok := resMap["base"].(map[string]any); ok {
						removed += ScrubDocument(base, subPrefix+".base", report)
					}
				}
			}
		}

		// Pipeline composition steps with input.resources
		if pipeline, ok := spec["pipeline"].([]any); ok {
			for _, stepRaw := range pipeline {
				if step, ok := stepRaw.(map[string]any); ok {
					stepName, _ := step["step"].(string)
					if stepName == "" {
						stepName, _ = step["name"].(string)
					}
					if input, ok := step["input"].(map[string]any); ok {
						if resources, ok := input["resources"].([]any); ok {
							for _, item := range resources {
								if resMap, ok := item.(map[string]any); ok {
									resName, _ := resMap["name"].(string)
									subPrefix := "spec.pipeline." + stepName + ".resources"
									if resName != "" {
										subPrefix = subPrefix + "." + resName
									}
									if pathPrefix != "" {
										subPrefix = pathPrefix + "." + subPrefix
									}
									if base, ok := resMap["base"].(map[string]any); ok {
										removed += ScrubDocument(base, subPrefix+".base", report)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return removed
}

// unwrapListDocs flattens any Kubernetes 'List' documents into their contained items.
func unwrapListDocs(docs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, doc := range docs {
		if kind, _ := doc["kind"].(string); kind == "List" {
			if items, ok := doc["items"].([]any); ok {
				for _, item := range items {
					if itemMap, ok := item.(map[string]any); ok {
						out = append(out, itemMap)
					}
				}
				continue
			}
		}
		out = append(out, doc)
	}
	return out
}
