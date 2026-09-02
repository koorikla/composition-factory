package emit

import (
	"fmt"
	"strings"
)

// This file renders the composed-resource body for a NATIVE Kubernetes kind
// (blueprint provider "k8s"). A native object has no forProvider envelope —
// apiVersion/kind/metadata/spec ARE the object — so its blueprint field
// paths are rooted at the object itself (spec.template.spec.containers[0].
// image, or data on a ConfigMap) and must be emitted as a real nested YAML
// tree. That is a genuine difference from the managed path, which writes
// each planned field as one flat line under forProvider: a dotted path
// written as a literal key would create a field literally named
// "spec.template.spec..." that the API server prunes without a word —
// exactly the silent-wrongness class this project exists to close.
//
// Conventions, matching schema.Leaves' path grammar exactly:
//   - a dotted segment nests a mapping key;
//   - a "seg[0]" segment opens a single-element block sequence (indexed
//     object arrays are composed as one element, per the existing [0]
//     convention);
//   - a leaf renders exactly the way the managed path renders it (same
//     quoteYAML discipline for value:, verbatim raw:, {{ $spec.x }} for
//     from:, hasKey guards for optional parameters).
//
// Conditional subtrees generalize writeMapField's rule: a branch whose
// every descendant leaf is optional must not render a bare "key:" (YAML
// null, rejected by a structural schema) when no descendant renders — so
// the whole branch, key included, is wrapped in one {{- if or (hasKey ...)
// ... }} guard over its descendants' parameters. Unlike forProvider — which
// must always exist on a managed resource, hence writeMapField's `{}`
// fallback — a native subtree is simply omitted entirely when nothing in it
// renders.

// nativeNode is one segment in the planned field tree.
type nativeNode struct {
	seg      string            // segment name, "[0]" stripped
	indexed  bool              // segment carried the [0] index
	leaf     *forProviderField // set when a planned field ends at this node
	children []*nativeNode     // path-sorted insertion order (plan is sorted)
	byName   map[string]*nativeNode
}

// buildNativeTree folds the path-sorted plan into a tree, refusing the two
// shapes that cannot be rendered as one YAML document rather than guessing:
// a path that is both a set value and a parent of other set values, and one
// array addressed both whole and by element.
func buildNativeTree(resourceName string, plan []forProviderField) (*nativeNode, error) {
	root := &nativeNode{byName: map[string]*nativeNode{}}
	for i := range plan {
		f := &plan[i]
		if f.isMap {
			for ei := range f.entries {
				entry := &f.entries[ei]
				segments := append(strings.Split(f.path, "."), entry.path)
				if err := insertNativePath(resourceName, root, f.path+"["+entry.path+"]", segments, entry); err != nil {
					return nil, err
				}
			}
		} else {
			segments := strings.Split(f.path, ".")
			if err := insertNativePath(resourceName, root, f.path, segments, f); err != nil {
				return nil, err
			}
		}
	}
	return root, nil
}

func insertNativePath(resourceName string, root *nativeNode, fullPath string, segments []string, leaf *forProviderField) error {
	cur := root
	for si, s := range segments {
		seg, indexed := strings.CutSuffix(s, "[0]")
		if seg == "" {
			return fmt.Errorf("resource %q: field %q: empty segment", resourceName, fullPath)
		}
		child := cur.byName[seg]
		if child == nil {
			child = &nativeNode{seg: seg, indexed: indexed, byName: map[string]*nativeNode{}}
			cur.byName[seg] = child
			cur.children = append(cur.children, child)
		} else if child.indexed != indexed {
			return fmt.Errorf("resource %q: %q is set both as a whole array and by element [0]; "+
				"set the whole array with one raw: field, or set element fields, not both",
				resourceName, seg)
		}
		if child.leaf != nil {
			return fmt.Errorf("resource %q: field %q conflicts with field %q, which already sets that "+
				"whole value; set the subtree with one raw: field or set individual fields inside it, not both",
				resourceName, fullPath, child.leaf.path)
		}
		if si == len(segments)-1 {
			if len(child.children) > 0 {
				return fmt.Errorf("resource %q: field %q sets a whole value, but other fields already set "+
					"paths inside it; set the subtree with one raw: field or set individual fields inside it, not both",
					resourceName, fullPath)
			}
			child.leaf = leaf
		}
		cur = child
	}
	return nil
}

// analyze reports whether any descendant leaf of n renders unconditionally
// (an empty guard), and the distinct guards among its conditional descendants
// in first-appearance (path-sorted) order — the inputs to the branch guard.
// Guards are deduplicated by their rendered text, which is deterministic per
// source (one hasKey form per optional parameter, one hasKey chain per status
// reference), so two leaves wired from the same parameter contribute one
// condition, exactly as the old per-parameter dedup did.
func (n *nativeNode) analyze() (unconditional bool, guards []string) {
	seen := map[string]bool{}
	var walk func(*nativeNode)
	walk = func(m *nativeNode) {
		if m.leaf != nil {
			if m.leaf.guard == "" {
				unconditional = true
			} else if !seen[m.leaf.guard] {
				seen[m.leaf.guard] = true
				guards = append(guards, m.leaf.guard)
			}
		}
		for _, c := range m.children {
			walk(c)
		}
	}
	walk(n)
	return unconditional, guards
}

// writeNativeFields renders plan as the object's own nested body, starting
// at indent (the same level as the object's metadata: key). An empty plan
// writes nothing: the composed object is then just apiVersion/kind/metadata,
// and whether that is a valid object of its kind is the API server's loud
// business, not something to paper over here.
func writeNativeFields(d *Doc, indent int, resourceName string, plan []forProviderField) error {
	root, err := buildNativeTree(resourceName, plan)
	if err != nil {
		return err
	}
	for _, child := range root.children {
		writeNativeNode(d, indent, child)
	}
	return nil
}

// writeNativeNode renders one node. Guard layering mirrors writeMapField:
// the branch-level {{- if or ... }} exists so a subtree whose every leaf is
// optional disappears entirely (key included) when none of its parameters
// are set, while each optional leaf inside still carries its own hasKey
// guard so ONE present parameter renders only its own line.
func writeNativeNode(d *Doc, indent int, n *nativeNode) {
	if n.leaf != nil {
		writeNativeLeaf(d, indent, n)
		return
	}

	unconditional, guards := n.analyze()
	guarded := !unconditional
	if guarded {
		conds := make([]string, len(guards))
		for i, g := range guards {
			conds[i] = "(" + g + ")"
		}
		d.Line(indent, "{{- if or %s }}", strings.Join(conds, " "))
	}

	d.Line(indent, "%s:", formatKey(n.seg))
	if n.indexed {
		// One sequence element. When the first child is an unconditional
		// leaf its line carries the dash ("- name: web"); otherwise the dash
		// stands alone on its own line — a template guard or a nested branch
		// cannot share a line with it — and YAML reads the deeper-indented
		// mapping that follows as the element's value.
		first := n.children[0]
		rest := n.children
		if first.leaf != nil && first.leaf.guard == "" && !first.indexed {
			d.Line(indent+1, "- %s: %s", formatKey(first.seg), first.leaf.rhs)
			rest = n.children[1:]
		} else {
			d.Line(indent+1, "-")
		}
		for _, c := range rest {
			writeNativeNode(d, indent+2, c)
		}
	} else {
		for _, c := range n.children {
			writeNativeNode(d, indent+1, c)
		}
	}

	if guarded {
		d.Line(indent, "{{- end }}")
	}
}

// writeNativeLeaf renders a planned field at its final position. The
// indexed-leaf case is a whole array element set outright (a raw: flow
// mapping on "containers[0]", say): the key opens a sequence whose single
// element is the value.
func writeNativeLeaf(d *Doc, indent int, n *nativeNode) {
	if n.leaf.guard != "" {
		d.Line(indent, "{{- if %s }}", n.leaf.guard)
	}
	if n.indexed {
		d.Line(indent, "%s:", formatKey(n.seg))
		d.Line(indent+1, "- %s", n.leaf.rhs)
	} else {
		d.Line(indent, "%s: %s", formatKey(n.seg), n.leaf.rhs)
	}
	if n.leaf.guard != "" {
		d.Line(indent, "{{- end }}")
	}
}
