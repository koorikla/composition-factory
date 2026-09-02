package schema

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// chainProps is a small tree covering every chain case:
//
//	region                    required root leaf
//	config        (required)  endpoint required-in-required, timeout optional
//	optionalBlock (optional)  inner required-in-OPTIONAL — raw noise
//	selector      (required)  branch with no required members: a dead-end
func chainProps() map[string]any {
	return map[string]any{
		"region": map[string]any{"type": "string"},
		"config": map[string]any{
			"type": "object", "required": []any{"endpoint"},
			"properties": map[string]any{
				"endpoint": map[string]any{"type": "string"},
				"timeout":  map[string]any{"type": "integer"},
			},
		},
		"optionalBlock": map[string]any{
			"type": "object", "required": []any{"inner"},
			"properties": map[string]any{
				"inner": map[string]any{"type": "string"},
			},
		},
		"selector": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"matchLabels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			},
		},
	}
}

// chainByPath flattens an annotated tree to path -> RequiredChain over both
// leaves and branches, using Leaves' own paths for the leaves.
func chainByPath(nodes []*Node) map[string]bool {
	out := map[string]bool{}
	var walk func(ns []*Node, prefix string)
	walk = func(ns []*Node, prefix string) {
		for _, n := range ns {
			path := n.Name
			if prefix != "" {
				path = prefix + "." + n.Name
			}
			out[path] = n.RequiredChain
			childPrefix := path
			if n.Type == "array" {
				childPrefix = path + "[0]"
			}
			walk(n.Children, childPrefix)
		}
	}
	walk(nodes, "")
	return out
}

func TestComputeRequiredChainStrict(t *testing.T) {
	nodes := BuildTree(chainProps(), []string{"region", "config", "selector"})
	ComputeRequiredChain(nodes, false)

	want := map[string]bool{
		"region":               true,  // required at the root: chain-true
		"config":               true,  // required root branch
		"config.endpoint":      true,  // required within required: the whole chain holds
		"config.timeout":       false, // optional within required
		"optionalBlock":        false, // optional at the root
		"optionalBlock.inner":  false, // required within OPTIONAL: raw yes, chain no
		"selector":             true,  // required root branch (a dead-end — no required members)
		"selector.matchLabels": false,
	}
	if diff := cmp.Diff(want, chainByPath(nodes)); diff != "" {
		t.Errorf("strict chain (-want +got):\n%s", diff)
	}
}

// topLevelHeld extends the held context past a depth-zero node that is not
// itself required — the native policy (the apiserver validates top-level
// struct members like spec unconditionally, and the vendored top level
// declares no required array at all). It reaches exactly one level: a break
// in the chain any deeper still cuts everything beneath it.
func TestComputeRequiredChainTopLevelHeld(t *testing.T) {
	props := map[string]any{
		"spec": map[string]any{
			"type": "object", "required": []any{"selector", "template"},
			"properties": map[string]any{
				"replicas": map[string]any{"type": "integer"},
				"selector": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"matchLabels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					},
				},
				"template": map[string]any{
					"type": "object",
					"properties": map[string]any{
						// Not required within template: the chain breaks here,
						// so containers and its required name never chain even
						// though PodSpec/Container mark them required.
						"spec": map[string]any{
							"type": "object", "required": []any{"containers"},
							"properties": map[string]any{
								"containers": map[string]any{
									"type": "array",
									"items": map[string]any{
										"required": []any{"name"},
										"properties": map[string]any{
											"name":  map[string]any{"type": "string"},
											"image": map[string]any{"type": "string"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	nodes := BuildTree(props, nil) // native trees have no top-level required list
	ComputeRequiredChain(nodes, true)

	got := chainByPath(nodes)
	want := map[string]bool{
		"spec":                                   false, // its own raw flag stays honest…
		"spec.selector":                          true,  // …but its required members chain (topLevelHeld)
		"spec.template":                          true,
		"spec.replicas":                          false,
		"spec.template.spec":                     false, // optional-in-required: the chain breaks
		"spec.template.spec.containers":          false,
		"spec.template.spec.containers[0].name":  false, // required-in-optional stays out
		"spec.template.spec.containers[0].image": false,
		"spec.selector.matchLabels":              false,
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s: RequiredChain=%v, want %v", path, got[path], w)
		}
	}
}

func TestRequiredBranchesReturnsDeadEndsOnly(t *testing.T) {
	nodes := BuildTree(chainProps(), []string{"region", "config", "selector"})
	ComputeRequiredChain(nodes, false)

	got := RequiredBranches(nodes, "")
	// selector: chain-required, no chain-true leaves -> surfaces.
	// config:   chain-required but config.endpoint chains -> its leaf already
	//           surfaces it, so the branch stays out.
	if len(got) != 1 || got[0].Path != "selector" {
		var paths []string
		for _, b := range got {
			paths = append(paths, b.Path)
		}
		t.Errorf("RequiredBranches = %v, want exactly [selector]", paths)
	}
}

// A required dead-end branch inside an array of objects gets the same
// [0]-indexed path grammar Leaves uses, so the row is addressable the way
// every other field path is. Nested dead-ends each get their own row: pods
// (a required array with no chain-true leaves anywhere beneath) and the
// required probe branch inside its elements are both listed.
func TestRequiredBranchesUseArrayIndexedPaths(t *testing.T) {
	props := map[string]any{
		"pods": map[string]any{
			"type": "array",
			"items": map[string]any{
				"required": []any{"probe"},
				"properties": map[string]any{
					"probe": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}
	nodes := BuildTree(props, []string{"pods"})
	ComputeRequiredChain(nodes, false)

	var paths []string
	for _, b := range RequiredBranches(nodes, "") {
		paths = append(paths, b.Path)
	}
	want := []string{"pods", "pods[0].probe"}
	if diff := cmp.Diff(want, paths); diff != "" {
		t.Errorf("RequiredBranches (-want +got):\n%s", diff)
	}
}
