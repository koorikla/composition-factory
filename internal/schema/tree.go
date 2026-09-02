package schema

import (
	"fmt"
	"sort"
)

// Node is one property in a schema tree. A Node with Children is a branch; one
// without is a leaf that can carry a value.
type Node struct {
	Name        string
	Type        string // string, number, integer, boolean, object, array, map
	Description string
	Required    bool
	Children    []*Node

	// RequiredChain is EFFECTIVE requiredness: true iff this node's own
	// Required flag holds and so does every ancestor's, all the way to the
	// tree root (a root-level required node is chain-true). Required alone is
	// the RAW schema flag and stays faithful to the vendored/provider data:
	// Kubernetes marks members of optional objects required WITHIN them
	// (EnvVar.name is required — but only once you add an env entry), so a
	// raw-required leaf deep under optional ancestors is NOT something a user
	// must set to compose the object. RequiredChain is what "must actually be
	// set" filters should run on.
	//
	// BuildTree leaves it false; ComputeRequiredChain annotates a built tree.
	// Only trees handed out by the CRD methods (FieldTree, ForProvider,
	// Envelope, Status) are annotated.
	RequiredChain bool
}

// ComputeRequiredChain annotates every node's RequiredChain over an already
// built tree: chain-true iff the node's own Required holds and its parent's
// chain context holds; the root context always holds, so a root-level
// required node is chain-true.
//
// topLevelHeld extends the root context one level down: when true, a
// depth-zero node passes its children a held context even if it is not
// itself required. That is the native-Kubernetes policy — the vendored
// object schemas declare NO required array at the top level (spec is
// "optional" on every kind), yet the apiserver validates top-level struct
// members unconditionally: posting a Deployment with an empty spec fails
// with `spec.selector: Required value` — and the vendored top level
// declares no required array at all, so a strict chain would vacuously
// mark nothing on every native kind. See ComputeRequiredChain.
func ComputeRequiredChain(nodes []*Node, topLevelHeld bool) {
	for _, n := range nodes {
		n.RequiredChain = n.Required
		annotateChain(n.Children, n.RequiredChain || topLevelHeld)
	}
}

func annotateChain(nodes []*Node, parentHeld bool) {
	for _, n := range nodes {
		n.RequiredChain = parentHeld && n.Required
		annotateChain(n.Children, n.RequiredChain)
	}
}

// Branch is a branch node addressed by the same dotted path grammar Leaves
// uses (a branch inside an array of objects sits under the [0] element path).
type Branch struct {
	Path string
	Node *Node
}

// RequiredBranches returns the chain-required BRANCH nodes none of whose
// leaves are chain-required — required subtrees the flat leaf listing would
// otherwise drop entirely. Deployment is the motivating case: DeploymentSpec
// requires selector and template, but both are objects whose own members are
// all optional, so no leaf under either is chain-true and a leaf-only
// required filter shows neither. A chain-required branch that DOES have
// chain-true leaves is deliberately absent: its leaves already surface it.
//
// Nested dead-ends each appear on their own row (a chain-required branch
// inside another is listed alongside it); callers get every required subtree
// explicitly rather than having to re-derive containment.
//
// The tree must have been annotated by ComputeRequiredChain first — on an
// unannotated tree this returns nothing.
func RequiredBranches(nodes []*Node, prefix string) []Branch {
	var out []Branch
	for _, n := range nodes {
		if len(n.Children) == 0 {
			continue
		}
		path := n.Name
		if prefix != "" {
			path = prefix + "." + n.Name
		}
		if n.RequiredChain && !hasChainLeaf(n) {
			out = append(out, Branch{Path: path, Node: n})
		}
		childPrefix := path
		if n.Type == "array" {
			childPrefix = path + "[0]"
		}
		out = append(out, RequiredBranches(n.Children, childPrefix)...)
	}
	return out
}

func hasChainLeaf(n *Node) bool {
	for _, c := range n.Children {
		if len(c.Children) == 0 {
			if c.RequiredChain {
				return true
			}
			continue
		}
		if hasChainLeaf(c) {
			return true
		}
	}
	return false
}

// Leaf is a settable field and the dotted path that addresses it. Array
// elements are indexed, e.g. containers[0].image.
type Leaf struct {
	Path string
	Node *Node
}

// Leaves flattens nodes to settable fields with their paths. Arrays of
// scalars (no Children, e.g. managementPolicies) are assigned whole and keep
// their plain path with no [0] index; only arrays of objects (Children
// present, Type == "array", e.g. containers) get an indexed path for their
// element fields.
func Leaves(nodes []*Node, prefix string) []Leaf {
	var out []Leaf
	for _, n := range nodes {
		path := n.Name
		if prefix != "" {
			path = prefix + "." + n.Name
		}
		switch {
		case len(n.Children) == 0:
			out = append(out, Leaf{Path: path, Node: n})
		case n.Type == "array":
			out = append(out, Leaves(n.Children, path+"[0]")...)
		default:
			out = append(out, Leaves(n.Children, path)...)
		}
	}
	return out
}

// BuildTree converts an OpenAPI properties map into sorted Nodes. Sorting keeps
// emitted output deterministic.
func BuildTree(props map[string]any, required []string) []*Node {
	req := make(map[string]bool, len(required))
	for _, r := range required {
		req[r] = true
	}
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]*Node, 0, len(names))
	for _, name := range names {
		raw, _ := props[name].(map[string]any)
		out = append(out, buildNode(name, raw, req[name]))
	}
	return out
}

func buildNode(name string, raw map[string]any, required bool) *Node {
	n := &Node{Name: name, Required: required}
	n.Type, _ = raw["type"].(string)
	n.Description, _ = raw["description"].(string)

	switch n.Type {
	case "object":
		// additionalProperties means a map of scalars: a leaf, not a branch.
		if _, isMap := raw["additionalProperties"]; isMap {
			n.Type = "map"
			return n
		}
		if props, ok := raw["properties"].(map[string]any); ok {
			n.Children = BuildTree(props, stringSlice(raw["required"]))
		}
	case "array":
		if items, ok := raw["items"].(map[string]any); ok {
			if props, ok := items["properties"].(map[string]any); ok {
				n.Children = BuildTree(props, stringSlice(items["required"]))
			}
		}
	default:
		// An untyped node with properties is still an object in practice.
		if props, ok := raw["properties"].(map[string]any); ok && n.Type == "" {
			n.Type = "object"
			n.Children = BuildTree(props, stringSlice(raw["required"]))
		}
	}
	return n
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (c CRD) specProperties() (map[string]any, []string, error) {
	v, err := c.Preferred()
	if err != nil {
		return nil, nil, err
	}
	spec, ok := v.Properties["spec"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s: no spec in openAPIV3Schema", c.Kind)
	}
	props, ok := spec["properties"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s: no spec.properties", c.Kind)
	}
	return props, stringSlice(spec["required"]), nil
}

func (c CRD) getCachedTree(treeType string, compute func() ([]*Node, error)) ([]*Node, error) {
	if c.cache == nil {
		return compute()
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	if c.cache.trees == nil {
		c.cache.trees = make(map[string][]*Node)
	}
	if nodes, ok := c.cache.trees[treeType]; ok {
		return nodes, nil
	}
	nodes, err := compute()
	if err != nil {
		return nil, err
	}
	c.cache.trees[treeType] = nodes
	return nodes, nil
}

// ForProvider returns the spec.forProvider subtree.
func (c CRD) ForProvider() ([]*Node, error) {
	return c.getCachedTree("forProvider", func() ([]*Node, error) {
		props, _, err := c.specProperties()
		if err != nil {
			return nil, err
		}
		fp, ok := props["forProvider"].(map[string]any)
		if !ok {
			// Legitimate: provider-kubernetes ObservedObjectCollection has none.
			return nil, nil
		}
		inner, _ := fp["properties"].(map[string]any)
		nodes := BuildTree(inner, stringSlice(fp["required"]))
		// Strict chain: forProvider's own required list is the root context, and
		// an optional block's inner requireds bind only when the block is set —
		// the CRD's structural validation applies them exactly that way.
		ComputeRequiredChain(nodes, false)
		return nodes, nil
	})
}

// FieldTree returns the settable field tree for composing this kind — the
// one tree blueprint field paths are checked against and /api/fields serves.
//
// For a managed resource that is the spec.forProvider subtree, paths rooted
// there (region, redrivePolicy.deadLetterTargetArn). For a native Kubernetes
// kind there is no forProvider — the composed object IS the object — so the
// tree is the object's own top-level properties minus what a composition
// author never sets by path: apiVersion and kind (the generator emits them),
// metadata (the generator owns the composition-resource-name annotation; the
// nested pod-template metadata under spec.template stays fully addressable),
// and status (server-owned). Paths therefore read exactly the way they do in
// a manifest: spec.template.spec.containers[0].image, or data on a ConfigMap.
func (c CRD) FieldTree() ([]*Node, error) {
	if !c.Native {
		return c.ForProvider()
	}
	return c.getCachedTree("fieldTree", func() ([]*Node, error) {
		v, err := c.Preferred()
		if err != nil {
			return nil, err
		}
		rest := make(map[string]any, len(v.Properties))
		for k, val := range v.Properties {
			switch k {
			case "apiVersion", "kind", "status":
				continue
			}
			rest[k] = val
		}
		// No required list: Kubernetes object schemas mark nothing required at
		// the top level (required lives inside spec's own subtrees, which
		// BuildTree reads from each node's schema as usual).
		nodes := BuildTree(rest, nil)
		// topLevelHeld=true is the native chain policy: the apiserver validates
		// top-level struct members (spec) unconditionally — an empty Deployment
		// fails with `spec.selector: Required value` — and the vendored top level
		// declares no required array at all, so a strict chain would vacuously
		// mark nothing on every native kind. See ComputeRequiredChain.
		ComputeRequiredChain(nodes, true)
		return nodes, nil
	})
}

// Status returns the top-level .status subtree of the preferred version's
// schema, built the same way ForProvider builds spec.forProvider. It is the
// schema a cross-resource status wire (`from:
// resources.<name>.status.<path>`) resolves its path against.
//
// Nothing here assumes an upjet envelope: the tree is whatever the CRD's own
// status schema declares (for upjet that is atProvider plus the Crossplane
// machinery fields; a native-shaped resource carries whatever it carries).
// A CRD with no status schema returns nil, not an error — mirroring
// ForProvider's contract for a missing forProvider — because only the
// caller knows whether anything is actually being wired from it.
func (c CRD) Status() ([]*Node, error) {
	return c.getCachedTree("status", func() ([]*Node, error) {
		v, err := c.Preferred()
		if err != nil {
			return nil, err
		}
		st, ok := v.Properties["status"].(map[string]any)
		if !ok {
			return nil, nil
		}
		inner, _ := st["properties"].(map[string]any)
		if inner == nil {
			return nil, nil
		}
		nodes := BuildTree(inner, stringSlice(st["required"]))
		ComputeRequiredChain(nodes, false)
		return nodes, nil
	})
}

// Envelope returns spec.properties minus forProvider and initProvider. It is
// computed rather than hard-coded: the envelope is not universal across providers.
//
// A native kind has no envelope at all — there is no Crossplane wrapper
// around the object, so the honest answer is an empty tree, not the object's
// own spec (which is FieldTree's job to serve as settable fields).
func (c CRD) Envelope() ([]*Node, error) {
	if c.Native {
		return nil, nil
	}
	return c.getCachedTree("envelope", func() ([]*Node, error) {
		props, required, err := c.specProperties()
		if err != nil {
			return nil, err
		}
		rest := make(map[string]any, len(props))
		for k, val := range props {
			if k == "forProvider" || k == "initProvider" {
				continue
			}
			rest[k] = val
		}
		nodes := BuildTree(rest, required)
		ComputeRequiredChain(nodes, false)
		return nodes, nil
	})
}
