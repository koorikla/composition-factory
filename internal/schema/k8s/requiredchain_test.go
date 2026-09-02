package k8s

import (
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// Effective requiredness for the vendored Deployment, derived from the data
// itself (required_test.go pins the RAW flags; this pins the CHAIN built on
// them). The vendored arrays say: the Deployment's top level requires
// nothing (spec is "optional"), DeploymentSpec requires selector and
// template — both BRANCHES whose own members are all optional
// (LabelSelector and PodTemplateSpec declare none required) — and every
// other required in the tree (PodSpec.containers, Container.name, ...) sits
// below template.spec, which PodTemplateSpec does NOT require, so its chain
// is broken. Therefore: ZERO chain-true leaves, and exactly two required
// branches. That is the effective truth the raw flags drown in noise: 250 of
// 842 leaves are raw-required, but only selector and template must actually
// be set.
func TestDeploymentChainIsExactlySelectorAndTemplate(t *testing.T) {
	tree, err := kindByName(t, "Deployment").FieldTree()
	if err != nil {
		t.Fatal(err)
	}

	var chainLeaves []string
	for _, l := range schema.Leaves(tree, "") {
		if l.Node.RequiredChain {
			chainLeaves = append(chainLeaves, l.Path)
		}
	}
	if len(chainLeaves) != 0 {
		t.Errorf("Deployment has chain-true leaves %v; the vendored data proves none "+
			"(every required leaf sits under an unrequired ancestor)", chainLeaves)
	}

	var branches []string
	for _, b := range schema.RequiredBranches(tree, "") {
		branches = append(branches, b.Path)
	}
	if len(branches) != 2 || branches[0] != "spec.selector" || branches[1] != "spec.template" {
		t.Errorf("RequiredBranches = %v, want exactly [spec.selector spec.template]", branches)
	}
}

// The chain never lifts a raw-optional field: every chain-true node's own
// Required flag holds too, across the whole Deployment tree. (The converse
// is the whole point — most raw-required nodes are NOT chain-true.)
func TestDeploymentChainImpliesRawRequired(t *testing.T) {
	tree, err := kindByName(t, "Deployment").FieldTree()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(ns []*schema.Node, prefix string)
	walk = func(ns []*schema.Node, prefix string) {
		for _, n := range ns {
			path := prefix + n.Name
			if n.RequiredChain && !n.Required {
				t.Errorf("%s: RequiredChain without Required — the chain must be built on the raw flags, never override them", path)
			}
			walk(n.Children, path+".")
		}
	}
	walk(tree, "")
}
