package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

var serverSideMetadataKeys = map[string]bool{
	"managedFields":     true,
	"resourceVersion":   true,
	"uid":               true,
	"creationTimestamp": true,
	"generation":        true,
	"selfLink":          true,
}

var serverSideAnnotationKeys = map[string]bool{
	"kubectl.kubernetes.io/last-applied-configuration": true,
	"argocd.argoproj.io/tracking-id":                   true,
}

// ScrubKubectlExport removes server-side fields from a kubectl get export.
// Returns the scrubbed YAML and the count of removed fields.
func ScrubKubectlExport(raw []byte) ([]byte, int, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal yaml for scrub: %w", err)
	}

	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return raw, 0, nil
	}

	removed := 0
	topMap := root.Content[0]

	// 1. Strip top-level 'status'
	newTopContent := make([]*yaml.Node, 0, len(topMap.Content))
	for i := 0; i < len(topMap.Content); i += 2 {
		k := topMap.Content[i]
		v := topMap.Content[i+1]
		if k.Value == "status" {
			removed++
			continue
		}
		if k.Value == "metadata" && v.Kind == yaml.MappingNode {
			var rMeta int
			v, rMeta = scrubMetadataNode(v)
			removed += rMeta
		}
		newTopContent = append(newTopContent, k, v)
	}
	topMap.Content = newTopContent

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, removed, fmt.Errorf("failed to marshal scrubbed yaml: %w", err)
	}

	return out, removed, nil
}

func scrubMetadataNode(node *yaml.Node) (*yaml.Node, int) {
	removed := 0
	newContent := make([]*yaml.Node, 0, len(node.Content))

	for i := 0; i < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]

		if serverSideMetadataKeys[k.Value] {
			removed++
			continue
		}

		if k.Value == "annotations" && v.Kind == yaml.MappingNode {
			newAnnContent := make([]*yaml.Node, 0, len(v.Content))
			for j := 0; j < len(v.Content); j += 2 {
				annKey := v.Content[j]
				annVal := v.Content[j+1]
				if serverSideAnnotationKeys[annKey.Value] {
					removed++
					continue
				}
				newAnnContent = append(newAnnContent, annKey, annVal)
			}
			if len(newAnnContent) == 0 {
				removed++
				continue
			}
			v.Content = newAnnContent
		}

		newContent = append(newContent, k, v)
	}

	node.Content = newContent
	return node, removed
}
