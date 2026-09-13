// Package manifest converts a resource's flat blueprint field map to and
// from nested manifest-shaped YAML. The kind's schema tree is the authority
// for path grammar: a map node's children become [key] entries, an array of
// objects' children become [i] elements, an object's members use dots.
// Leaves carry the blueprint's own wrappers ({value|from|raw|template});
// a plain scalar is a literal.
//
// Two rules keep the text unambiguous in both directions:
//
//   - A mapping whose only key is one of value/from/raw/template with a
//     scalar value IS a wrapper, wherever it appears. Render therefore never
//     emits a bare `from: x` as the single entry of a map or object (a
//     genuine annotation called "from", an env entry with only value set);
//     it writes `from: {value: x}` so Parse reads it back as an entry.
//   - A literal at an object, map, or array-of-objects position is always an
//     explicit `{value: "..."}` / `{raw: "..."}` wrapper. A bare scalar there
//     is an error in Parse, never inferred as a whole-value assignment.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Error carries the offending path and YAML line so a UI can highlight it.
// Line is 0 when the problem is not tied to a line (a conflict between two
// field paths in Render, or a syntax error yaml.v3 did not locate).
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	switch {
	case e.Line > 0 && e.Path != "":
		return fmt.Sprintf("line %d: %s: %s", e.Line, e.Path, e.Msg)
	case e.Line > 0:
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	case e.Path != "":
		return fmt.Sprintf("%s: %s", e.Path, e.Msg)
	}
	return e.Msg
}

var wrapperKeys = map[string]bool{"value": true, "from": true, "raw": true, "template": true}

// ---- path tokenizer -------------------------------------------------------

// seg is one step of a field path: a dotted name, or a bracketed token
// ([0], [app], [app.kubernetes.io/part-of]). Whether a bracket is an array
// index or a map key is decided by the schema at walk time, not here: a
// ConfigMap's data[0] is the key "0".
type seg struct {
	name    string
	bracket bool
}

// tokenize splits "a.b[0].c[app.kubernetes.io/x]" into segments; everything
// inside [...] is one token, dots included.
func tokenize(path string) ([]seg, error) {
	var out []seg
	i := 0
	for i < len(path) {
		j := i
		for j < len(path) && path[j] != '.' && path[j] != '[' {
			j++
		}
		if j > i {
			out = append(out, seg{name: path[i:j]})
		}
		i = j
		for i < len(path) && path[i] == '[' {
			k := strings.IndexByte(path[i:], ']')
			if k < 0 {
				return nil, &Error{Path: path, Msg: "unterminated [ in field path"}
			}
			out = append(out, seg{name: path[i+1 : i+k], bracket: true})
			i += k + 1
		}
		if i < len(path) && path[i] == '.' {
			i++
		}
	}
	if len(out) == 0 {
		return nil, &Error{Path: path, Msg: "empty field path"}
	}
	return out, nil
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---- Render ---------------------------------------------------------------

// node is one position in the nested tree Render builds from the flat paths.
type node struct {
	children []*node          // named/keyed members in insertion (path-sorted) order
	byKey    map[string]*node // members by label
	elems    map[int]*node    // array elements by index
	kind     string           // "obj" (mapping) | "seq" (sequence) | "" (leaf)
	label    string
	path     string
	field    *blueprint.Field
	schema   *schema.Node // the schema node at this position; for an array element, the array node
	mapEntry bool         // this node is one [key] entry of a map: a string scalar position
	elem     bool         // this node is one [i] element of an array of objects
}

func newNode(label, path string, sn *schema.Node) *node {
	return &node{byKey: map[string]*node{}, elems: map[int]*node{}, label: label, path: path, schema: sn}
}

func childSchema(parent *schema.Node, name string, roots []*schema.Node) *schema.Node {
	list := roots
	if parent != nil {
		list = parent.Children
	}
	for _, c := range list {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// isBranch reports whether sn is a position that holds members rather than
// a scalar: an object with declared members, a map, or an array of objects.
// A childless object and an array of scalars are leaves (schema.Leaves
// agrees).
func isBranch(sn *schema.Node) bool {
	if sn == nil {
		return false
	}
	switch sn.Type {
	case "map":
		return true
	case "array":
		return len(sn.Children) > 0
	default:
		return len(sn.Children) > 0
	}
}

// Render nests fields into manifest YAML. Unknown paths still render (the
// engine's own validation owns that message); the schema only decides
// scalar tags and which brackets are keys rather than indices. Fields are
// laid out in path-sorted order, the order the emitter plans them in.
func Render(nodes []*schema.Node, fields map[string]blueprint.Field) (string, error) {
	root := newNode("", "", nil)
	paths := make([]string, 0, len(fields))
	for p := range fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := fields[p]
		segs, err := tokenize(p)
		if err != nil {
			return "", err
		}
		cur := root
		for si, s := range segs {
			var next *node
			switch {
			case s.bracket && isIndex(s.name) && (cur.schema == nil || cur.schema.Type != "map") && !cur.mapEntry:
				// An index into an array (or into an unknown path, where a
				// numeric bracket can only mean an index).
				idx, _ := strconv.Atoi(s.name)
				next = cur.elems[idx]
				if next == nil {
					next = newNode("", cur.path+"["+s.name+"]", cur.schema)
					next.elem = true
					cur.elems[idx] = next
					cur.kind = "seq"
				}
			case s.bracket:
				key := strings.Trim(s.name, `"'`)
				next = cur.byKey[key]
				if next == nil {
					next = newNode(key, cur.path+"["+s.name+"]", nil)
					next.mapEntry = true
					cur.byKey[key] = next
					cur.children = append(cur.children, next)
					cur.kind = "obj"
				}
			default:
				var sn *schema.Node
				if cur == root {
					sn = childSchema(nil, s.name, nodes)
				} else if cur.schema != nil && !cur.mapEntry {
					sn = childSchema(cur.schema, s.name, nil)
				}
				next = cur.byKey[s.name]
				if next == nil {
					np := s.name
					if cur.path != "" {
						np = cur.path + "." + s.name
					}
					next = newNode(s.name, np, sn)
					cur.byKey[s.name] = next
					cur.children = append(cur.children, next)
					cur.kind = "obj"
				}
			}
			if si == len(segs)-1 {
				if next.kind != "" {
					return "", &Error{Path: p, Msg: "sets a whole value that other fields set members of"}
				}
				if next.field != nil {
					return "", &Error{Path: p, Msg: fmt.Sprintf("is set twice (also as %q)", next.path)}
				}
				ff := f
				next.field = &ff
			} else if next.field != nil {
				return "", &Error{Path: p, Msg: fmt.Sprintf("conflicts with %q which sets the whole value", next.path)}
			}
			cur = next
		}
	}
	y := toYAML(root, true)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(y); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func flowMap(key string, val *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{strScalar(key), val}}
}

func strScalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// typedScalar tags a literal by the schema type it sits at so integers,
// numbers and booleans read unquoted while strings that merely look like
// them stay quoted. An integer literal at an IntOrString position (a port
// that may be a name) reads as the number it is, the way the emitter writes
// it.
func typedScalar(v string, sn *schema.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: v}
	typ := ""
	if sn != nil {
		typ = sn.Type
		if sn.Format == "int-or-string" {
			if _, err := strconv.ParseInt(v, 10, 64); err == nil {
				typ = "integer"
			}
		}
	}
	switch typ {
	case "integer":
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			n.Tag = "!!int"
			return n
		}
	case "number":
		if x, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(x) && !math.IsInf(x, 0) {
			n.Tag = "!!float"
			return n
		}
	case "boolean":
		if v == "true" || v == "false" {
			n.Tag = "!!bool"
			return n
		}
	}
	n.Tag = "!!str"
	return n
}

// leafYAML renders one field at its position. Wires, raw and template
// values are always wrappers. A literal is a plain scalar at a scalar
// position, a flow list at an array-of-scalars position, and an explicit
// {value: ...} wrapper at a branch position (object, map, array of objects)
// where a bare scalar would be rejected by Parse.
func leafYAML(n *node) *yaml.Node {
	f := n.field
	switch {
	case f.From != "":
		return flowMap("from", strScalar(f.From))
	case f.Raw != "":
		return flowMap("raw", strScalar(f.Raw))
	case f.Template != "":
		return flowMap("template", strScalar(f.Template))
	}
	if n.mapEntry {
		return typedScalar(f.Value, nil)
	}
	if n.elem || isBranch(n.schema) {
		return flowMap("value", strScalar(f.Value))
	}
	if n.schema != nil && n.schema.Type == "array" {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, it := range strings.Split(f.Value, ",") {
			seq.Content = append(seq.Content, strScalar(strings.TrimSpace(it)))
		}
		return seq
	}
	return typedScalar(f.Value, n.schema)
}

func toYAML(n *node, isRoot bool) *yaml.Node {
	if n.field != nil {
		return leafYAML(n)
	}
	if n.kind == "seq" {
		idx := make([]int, 0, len(n.elems))
		for i := range n.elems {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		out := &yaml.Node{Kind: yaml.SequenceNode}
		last := -1
		for _, i := range idx {
			for gap := last + 1; gap < i; gap++ {
				out.Content = append(out.Content, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle})
			}
			out.Content = append(out.Content, toYAML(n.elems[i], false))
			last = i
		}
		return out
	}
	out := &yaml.Node{Kind: yaml.MappingNode}
	if len(n.children) == 0 {
		out.Style = yaml.FlowStyle
	}
	// A mapping whose entries are ALL named like wrapper keys would read
	// back as a wrapper (one entry) or a malformed one (two); wrap each
	// scalar so every entry reads as the entry it is. The root is never
	// taken for a wrapper, so a top-level `value:` field stays bare.
	allWrapperKeys := !isRoot && len(n.children) > 0
	for _, c := range n.children {
		if !wrapperKeys[c.label] {
			allWrapperKeys = false
			break
		}
	}
	for _, c := range n.children {
		v := toYAML(c, false)
		if allWrapperKeys {
			switch {
			case v.Kind == yaml.ScalarNode:
				v = flowMap("value", v)
			case v.Kind == yaml.SequenceNode && c.field != nil:
				// A list-of-scalars literal: Parse reads {value: [a, b]} as a
				// malformed wrapper, so carry the comma-joined value instead.
				v = flowMap("value", strScalar(c.field.Value))
			}
		}
		out.Content = append(out.Content, strScalar(c.label), v)
	}
	return out
}

// ---- Parse ----------------------------------------------------------------

// Options tunes Parse for the kind the tree belongs to.
type Options struct {
	// ObjectRooted says the tree is an object's own top level (metadata,
	// spec, data ...) — a native or crds:-sourced kind, or a function input —
	// rather than a managed resource's spec.forProvider body. It only picks
	// the hint an unknown top-level envelope key gets (see unknownFieldMsg).
	// The API passes crd.Native || crd.IsFunctionInput() from the CRD the
	// resource resolved to.
	ObjectRooted bool
}

// Parse is ParseWith with the rooting inferred from the tree: a top-level
// metadata node means an object's own top level. That is right for the
// vendored native kinds and for managed kinds, and wrong for a
// crds:-sourced kind whose tree has only spec and status — callers that
// hold the resolved CRD use ParseWith and say so.
func Parse(nodes []*schema.Node, text string) (map[string]blueprint.Field, error) {
	return ParseWith(nodes, text, Options{ObjectRooted: childSchema(nil, "metadata", nodes) != nil})
}

// ParseWith walks manifest YAML against the schema tree and flattens it to
// fields. A mapping whose only key is value/from/raw/template with a scalar
// value is a wrapper; a null scalar leaves its field unset. Every error is
// an *Error carrying the field path and the line of the offending node.
func ParseWith(nodes []*schema.Node, text string, o Options) (map[string]blueprint.Field, error) {
	out := map[string]blueprint.Field{}
	dec := yaml.NewDecoder(strings.NewReader(text))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		return nil, syntaxError(err)
	}
	// A single-node decode would silently keep the first document and drop
	// the rest; a second document is refused at its --- marker instead.
	var second yaml.Node
	if err := dec.Decode(&second); err == nil {
		line := second.Line
		if line == 0 && len(second.Content) > 0 {
			line = second.Content[0].Line
		}
		return nil, &Error{Line: line, Msg: "a manifest is one YAML document; remove the --- and what follows it"}
	} else if !errors.Is(err, io.EOF) {
		return nil, syntaxError(err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return out, nil
	}
	body := deref(doc.Content[0])
	if isNull(body) {
		return out, nil
	}
	if body.Kind != yaml.MappingNode {
		return nil, &Error{Line: body.Line, Msg: "manifest must be a mapping"}
	}
	p := &parser{out: out, objectRooted: o.ObjectRooted}
	if err := p.walkObject(body, nodes, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// parser carries one walk's output and the kind's rooting.
type parser struct {
	out          map[string]blueprint.Field
	objectRooted bool
}

// syntaxError turns a yaml.v3 parse error into an *Error. Error() names the
// line itself, so yaml.v3's own "yaml: line N: " prefix is dropped and the
// message carries the line exactly once.
func syntaxError(err error) *Error {
	line := yamlErrLine(err)
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	if line > 0 {
		msg = strings.TrimPrefix(msg, fmt.Sprintf("line %d: ", line))
	}
	return &Error{Line: line, Msg: msg}
}

// deref follows an alias (*anchor) to the node it names, so an anchored
// scalar or mapping reads the same through every alias of it.
func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func isMergeKey(k *yaml.Node) bool {
	return k.Tag == "!!merge" || k.Value == "<<"
}

// yamlErrLine pulls the line out of a yaml.v3 syntax error ("yaml: line 3:
// ..."); 0 when the message carries none.
func yamlErrLine(err error) int {
	var l int
	if _, e := fmt.Sscanf(err.Error(), "yaml: line %d:", &l); e == nil {
		return l
	}
	return 0
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

const wrapperMsg = "a wrapper takes exactly one of value, from, raw, template with a scalar value"

// wrapper recognises {value|from|raw|template: scalar}. member reports
// whether a key is a declared member at this position, and decides what a
// mapping that is SHAPED like a wrapper but malformed means — a wrapper key
// with a list or object value, or several wrapper keys: an error when the
// keys are not members (a mistyped wrapper at a leaf), entries when they
// all are. The schema has to arbitrate: a Deployment's spec has a member
// called template, an annotation may be called "from" (Render writes it as
// `from: {value: x}`), and neither is a wrapper. A wrapped null is always an
// error: leaving the field out is how a value is unset.
func wrapper(n *yaml.Node, member func(string) bool) (blueprint.Field, bool, *Error) {
	if n.Kind != yaml.MappingNode || len(n.Content) < 2 {
		return blueprint.Field{}, false, nil
	}
	allMembers := true
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if !wrapperKeys[k] {
			return blueprint.Field{}, false, nil
		}
		if !member(k) {
			allMembers = false
		}
	}
	if len(n.Content) > 2 {
		if allMembers {
			return blueprint.Field{}, false, nil
		}
		return blueprint.Field{}, false, &Error{Line: n.Line, Msg: wrapperMsg}
	}
	k, v := n.Content[0], deref(n.Content[1])
	if isNull(v) {
		return blueprint.Field{}, false, &Error{Line: v.Line, Msg: fmt.Sprintf("{%s: ...} needs a scalar; leave the field out to unset it", k.Value)}
	}
	if v.Kind != yaml.ScalarNode {
		if member(k.Value) {
			return blueprint.Field{}, false, nil
		}
		return blueprint.Field{}, false, &Error{Line: v.Line, Msg: wrapperMsg}
	}
	switch k.Value {
	case "value":
		return blueprint.Field{Value: v.Value}, true, nil
	case "from":
		return blueprint.Field{From: v.Value}, true, nil
	case "raw":
		return blueprint.Field{Raw: v.Value}, true, nil
	default:
		return blueprint.Field{Template: v.Value}, true, nil
	}
}

// noMember is the member predicate where nothing nests: a map entry, or a
// position whose value can only be a scalar or a wrapper.
func noMember(string) bool { return false }

// memberOf is the member predicate for an object position: its declared
// children. A map position takes any key; an array position takes none
// (its items do, see the element loop in walkValue).
func memberOf(sn *schema.Node) func(string) bool {
	switch {
	case sn.Type == "map":
		return func(string) bool { return true }
	case sn.Type == "array" || len(sn.Children) == 0:
		return noMember
	default:
		return func(k string) bool { return childSchema(sn, k, nil) != nil }
	}
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// checkScalar refuses a literal that the schema type cannot hold, with the
// same acceptance rules the emitter applies (base-10 integers, finite
// numbers, true/false in any case), so the line is reported here rather
// than a path-only message later from CRD validation.
func checkScalar(v *yaml.Node, sn *schema.Node, path string) *Error {
	switch sn.Type {
	case "integer":
		if _, err := strconv.ParseInt(v.Value, 10, 64); err != nil {
			return &Error{Path: path, Line: v.Line, Msg: fmt.Sprintf("%q is not a valid integer", v.Value)}
		}
	case "number":
		if x, err := strconv.ParseFloat(v.Value, 64); err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return &Error{Path: path, Line: v.Line, Msg: fmt.Sprintf("%q is not a valid number", v.Value)}
		}
	case "boolean":
		switch strings.ToLower(v.Value) {
		case "true", "false":
		default:
			return &Error{Path: path, Line: v.Line, Msg: fmt.Sprintf("%q is not a valid boolean (use true or false)", v.Value)}
		}
	}
	return nil
}

// forProviderHint is the message for a managed kind given an envelope key:
// its settable tree is spec.forProvider's body, nothing around it.
const forProviderHint = "paste only the spec.forProvider body — apiVersion, kind and metadata are generated"

// unknownFieldMsg explains a key the schema does not declare. A top-level
// envelope key gets a dedicated hint: an object-rooted tree is the
// object's own top level (metadata, spec, data ...) so only apiVersion,
// kind and status are foreign there; a managed kind's tree is the
// spec.forProvider body, which has no metadata or spec of its own. Which
// of the two applies comes from the resolved CRD (Options.ObjectRooted),
// never from the tree's shape. Otherwise the closest declared sibling is
// suggested when one is close.
func unknownFieldMsg(key string, top, objectRooted bool, siblings []*schema.Node) string {
	if top {
		switch key {
		case "apiVersion", "kind":
			if objectRooted {
				return "omit apiVersion and kind; the generator emits them"
			}
			return forProviderHint
		case "status":
			if objectRooted {
				return "omit status; the server owns it"
			}
			return forProviderHint
		case "metadata", "spec":
			if !objectRooted {
				return forProviderHint
			}
		}
	}
	names := make([]string, 0, len(siblings))
	for _, c := range siblings {
		names = append(names, c.Name)
	}
	if best := blueprint.ClosestPath(key, names); best != "" {
		return fmt.Sprintf("unknown field; did you mean %q?", best)
	}
	return "unknown field"
}

func (p *parser) walkObject(m *yaml.Node, children []*schema.Node, prefix string) error {
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		path := join(prefix, k.Value)
		if isMergeKey(k) {
			return &Error{Path: path, Line: k.Line, Msg: "YAML merge keys (<<) are not supported here"}
		}
		if seen[k.Value] {
			return &Error{Path: path, Line: k.Line, Msg: "duplicate key"}
		}
		seen[k.Value] = true
		sn := childSchema(nil, k.Value, children)
		if sn == nil {
			return &Error{Path: path, Line: k.Line, Msg: unknownFieldMsg(k.Value, prefix == "", p.objectRooted, children)}
		}
		if err := p.walkValue(v, sn, path); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) walkValue(v *yaml.Node, sn *schema.Node, path string) error {
	v = deref(v)
	if isNull(v) {
		return nil
	}
	if f, ok, werr := wrapper(v, memberOf(sn)); werr != nil {
		werr.Path = path
		return werr
	} else if ok {
		p.out[path] = f
		return nil
	}
	switch {
	case sn.Type == "map":
		if v.Kind != yaml.MappingNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a mapping of keys; to set the whole map verbatim use {raw: \"...\"}"}
		}
		return p.walkMap(v, path)
	case sn.Type == "array" && len(sn.Children) > 0:
		if v.Kind != yaml.SequenceNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a list; to set the whole list verbatim use {raw: \"...\"}"}
		}
		for i, item := range v.Content {
			item = deref(item)
			ep := path + "[" + strconv.Itoa(i) + "]"
			if isNull(item) {
				continue
			}
			if f, ok, werr := wrapper(item, func(k string) bool { return childSchema(sn, k, nil) != nil }); werr != nil {
				werr.Path = ep
				return werr
			} else if ok {
				p.out[ep] = f
				continue
			}
			if item.Kind != yaml.MappingNode {
				return &Error{Path: ep, Line: item.Line, Msg: "expected a mapping for a list element; to set it verbatim use {raw: \"...\"}"}
			}
			if err := p.walkObject(item, sn.Children, ep); err != nil {
				return err
			}
		}
		return nil
	case sn.Type == "array":
		if v.Kind == yaml.SequenceNode {
			parts := make([]string, 0, len(v.Content))
			for _, item := range v.Content {
				item = deref(item)
				if item.Kind != yaml.ScalarNode || isNull(item) || strings.Contains(item.Value, ",") || strings.TrimSpace(item.Value) == "" {
					return &Error{Path: path, Line: item.Line, Msg: "list entries must be non-empty scalars without commas; use {raw: \"[...]\"} otherwise"}
				}
				parts = append(parts, item.Value)
			}
			if len(parts) == 0 {
				return &Error{Path: path, Line: v.Line, Msg: "an empty list sets nothing; leave the field out to unset it"}
			}
			p.out[path] = blueprint.Field{Value: strings.Join(parts, ",")}
			return nil
		}
		if v.Kind == yaml.ScalarNode {
			p.out[path] = blueprint.Field{Value: v.Value}
			return nil
		}
		return &Error{Path: path, Line: v.Line, Msg: "expected a list of scalars"}
	case len(sn.Children) > 0:
		if v.Kind != yaml.MappingNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a mapping; to set the whole object verbatim use {raw: \"...\"}"}
		}
		return p.walkObject(v, sn.Children, path)
	default:
		if v.Kind != yaml.ScalarNode {
			msg := "expected a scalar or a {value|from|raw|template} wrapper"
			if sn.Type == "object" {
				msg += "; to set the whole object verbatim use {raw: \"...\"}"
			}
			return &Error{Path: path, Line: v.Line, Msg: msg}
		}
		if err := checkScalar(v, sn, path); err != nil {
			return err
		}
		p.out[path] = blueprint.Field{Value: v.Value}
		return nil
	}
}

// walkMap flattens a map node's entries to path[key] fields. Entries are
// scalars or wrappers; a nested structure has no field form and must be
// set verbatim through {raw: "..."} on the map itself.
func (p *parser) walkMap(v *yaml.Node, path string) error {
	seen := map[string]bool{}
	for i := 0; i+1 < len(v.Content); i += 2 {
		k, ev := v.Content[i], deref(v.Content[i+1])
		ep := path + "[" + k.Value + "]"
		if isMergeKey(k) {
			return &Error{Path: ep, Line: k.Line, Msg: "YAML merge keys (<<) are not supported here"}
		}
		if seen[k.Value] {
			return &Error{Path: ep, Line: k.Line, Msg: "duplicate key"}
		}
		seen[k.Value] = true
		if isNull(ev) {
			continue
		}
		if ev.Kind == yaml.ScalarNode {
			p.out[ep] = blueprint.Field{Value: ev.Value}
			continue
		}
		if f, ok, werr := wrapper(ev, noMember); werr != nil {
			werr.Path = ep
			return werr
		} else if ok {
			p.out[ep] = f
			continue
		}
		return &Error{Path: ep, Line: ev.Line, Msg: "a map entry must be a scalar or a {value|from|raw|template} wrapper"}
	}
	return nil
}
