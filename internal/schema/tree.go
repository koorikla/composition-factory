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
}

// Leaf is a settable field and the dotted path that addresses it. Array
// elements are indexed, e.g. containers[0].image.
type Leaf struct {
	Path string
	Node *Node
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

// specProperties returns spec.properties and spec.required for the preferred version.
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

// ForProvider returns the spec.forProvider subtree.
func (c CRD) ForProvider() ([]*Node, error) {
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
	return BuildTree(inner, stringSlice(fp["required"])), nil
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
	v, err := c.Preferred()
	if err != nil {
		return nil, err
	}
	rest := make(map[string]any, len(v.Properties))
	for k, val := range v.Properties {
		switch k {
		case "apiVersion", "kind", "metadata", "status":
			continue
		}
		rest[k] = val
	}
	// No required list: Kubernetes object schemas mark nothing required at
	// the top level (required lives inside spec's own subtrees, which
	// BuildTree reads from each node's schema as usual).
	return BuildTree(rest, nil), nil
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
	return BuildTree(inner, stringSlice(st["required"])), nil
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
	props, required, err := c.specProperties()
	if err != nil {
		return nil, err
	}
	rest := make(map[string]any, len(props))
	for k, v := range props {
		if k == "forProvider" || k == "initProvider" {
			continue
		}
		rest[k] = v
	}
	return BuildTree(rest, required), nil
}
