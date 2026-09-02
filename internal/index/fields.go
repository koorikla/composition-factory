package index

import (
	"strings"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// Field is one settable leaf field in a schema tree, addressed by the same
// dotted, array-indexed path schema.Leaves produces (containers[0].image).
type Field struct {
	Path        string `json:"path"`
	Type        string `json:"type"` // string number integer boolean object array map
	Description string `json:"description"`
	Required    bool   `json:"required"`
	// RequiredChain is effective requiredness: the field's own Required flag
	// AND every ancestor's, per schema.Node.RequiredChain. Required stays the
	// RAW schema flag (EnvVar.name is raw-required but only binds once an
	// optional env entry exists); RequiredChain is what a "must actually set
	// this" filter runs on. False on trees the CRD methods did not annotate.
	RequiredChain bool `json:"requiredChain"`
	Depth         int  `json:"depth"` // 0 for a top-level field
}

// FieldQuery narrows the field list Fields returns. The zero value returns
// every leaf: RequiredOnly off, MaxDepth unlimited, Prefix the whole tree,
// Search unset, Limit unlimited.
//
// Filters compose in a fixed order so results are predictable regardless of
// which options are set together: Prefix, then MaxDepth, then RequiredOnly,
// then Search, then Limit.
type FieldQuery struct {
	RequiredOnly bool   // keep only chain-required leaves (Field.RequiredChain)
	MaxDepth     int    // 0 means unlimited
	Prefix       string // "" for the whole tree; e.g. "template.spec" to expand one subtree
	Search       string // case-insensitive substring over path and description
	Limit        int    // <= 0 means unlimited
}

// Fields flattens nodes to leaf Fields by delegating to schema.Leaves(nodes,
// ""), then narrows the result according to q. It does not re-walk the tree
// and never re-sorts: schema.Leaves' ordering is preserved through every
// filter stage, so the result is deterministic wherever the input tree is.
func Fields(nodes []*schema.Node, q FieldQuery) []Field {
	leaves := schema.Leaves(nodes, "")

	fields := make([]Field, 0, len(leaves))
	for _, l := range leaves {
		fields = append(fields, Field{
			Path:          l.Path,
			Type:          l.Node.Type,
			Description:   l.Node.Description,
			Required:      l.Node.Required,
			RequiredChain: l.Node.RequiredChain,
			// The path has no separator before a top-level field's name, one
			// before a field one level down, and so on; an array index is
			// part of its own segment rather than adding a separator, so
			// counting "." characters gives depth-from-zero directly.
			Depth: strings.Count(l.Path, "."),
		})
	}

	if q.Prefix != "" {
		fields = filterPrefix(fields, q.Prefix)
	}
	if q.MaxDepth > 0 {
		fields = filterMaxDepth(fields, q.MaxDepth)
	}
	if q.RequiredOnly {
		fields = filterRequiredOnly(fields)
	}
	if q.Search != "" {
		fields = filterSearch(fields, q.Search)
	}
	if q.Limit > 0 && len(fields) > q.Limit {
		fields = fields[:q.Limit]
	}
	return fields
}

// filterPrefix keeps fields at or below prefix, matching on a path-segment
// boundary: prefix "template.spec" matches "template.spec.containers[0].name"
// (and the field whose path is exactly "template.spec", if one exists) but
// must not match a hypothetical "template.specimen".
func filterPrefix(fields []Field, prefix string) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Path == prefix || strings.HasPrefix(f.Path, prefix+".") {
			out = append(out, f)
		}
	}
	return out
}

// filterMaxDepth keeps fields at or shallower than maxDepth. Callers only
// reach this when maxDepth > 0 (Fields treats 0 as unlimited and skips the
// call), so there is no zero-means-unlimited case to special-case here.
func filterMaxDepth(fields []Field, maxDepth int) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.Depth <= maxDepth {
			out = append(out, f)
		}
	}
	return out
}

// filterRequiredOnly keeps only chain-required fields — fields the user must
// actually set, not every leaf carrying the raw required flag. The raw flag
// marks members required WITHIN their parent object even when that parent is
// optional (EnvVar.name on ~30% of a Deployment's leaves); filtering on it
// made "required only" noise. See Field.RequiredChain.
func filterRequiredOnly(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f.RequiredChain {
			out = append(out, f)
		}
	}
	return out
}

// RequiredBranches converts schema.RequiredBranches' rows — chain-required
// branch nodes with no chain-true leaves beneath, the required subtrees a
// leaf-only listing drops (Deployment's spec.selector and spec.template) —
// into Field rows on the same path/depth grammar the leaf listing uses. The
// rows are branches, not settable leaves, which is why they travel in their
// own list rather than inside the leaf listing (see handleKindFields).
func RequiredBranches(nodes []*schema.Node) []Field {
	branches := schema.RequiredBranches(nodes, "")
	out := make([]Field, 0, len(branches))
	for _, b := range branches {
		out = append(out, Field{
			Path:          b.Path,
			Type:          b.Node.Type,
			Description:   b.Node.Description,
			Required:      b.Node.Required,
			RequiredChain: b.Node.RequiredChain,
			Depth:         strings.Count(b.Path, "."),
		})
	}
	return out
}

// filterSearch keeps fields whose Path or Description contains search as a
// case-insensitive substring.
func filterSearch(fields []Field, search string) []Field {
	q := strings.ToLower(search)
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f.Path), q) || strings.Contains(strings.ToLower(f.Description), q) {
			out = append(out, f)
		}
	}
	return out
}
