package blueprint

import (
	"fmt"
	"strings"
)

// deepCopy returns a copy of the blueprint that shares no mutable state with
// the receiver. Parameters is a map, and Resources is a slice of structs
// each containing a Fields map: a shallow struct copy (`cp := *b`) still
// aliases both, so mutating the copy's maps would mutate the receiver's too.
// Every atomic operation below edits this copy and only writes it back to
// the receiver once Validate() has accepted it -- a rejected edit therefore
// cannot leave the receiver half-changed, because the receiver was never
// touched.
//
// Maintenance note: if Parameter or Resource ever grows another slice- or
// map-typed field, it must be deep-copied here too. Missing one does not
// fail to compile and does not fail a test until someone writes a mutating
// test against that specific field -- until then, a "rejected" edit would
// silently mutate the receiver through the aliased backing store, exactly
// the failure mode this function exists to close.
func (b *Blueprint) deepCopy() *Blueprint {
	cp := *b

	cp.Spec.Sources = append([]Source(nil), b.Spec.Sources...)
	cp.Spec.Conventions = append([]Convention(nil), b.Spec.Conventions...)

	if b.Spec.Templates != nil {
		cp.Spec.Templates = make(map[string]string, len(b.Spec.Templates))
		for name, body := range b.Spec.Templates {
			cp.Spec.Templates[name] = body
		}
	}

	cp.Spec.XRD.Parameters = make(map[string]Parameter, len(b.Spec.XRD.Parameters))
	for name, p := range b.Spec.XRD.Parameters {
		cp.Spec.XRD.Parameters[name] = copyParameter(p)
	}

	cp.Spec.Resources = make([]Resource, len(b.Spec.Resources))
	for i, r := range b.Spec.Resources {
		r.Fields = make(map[string]Field, len(b.Spec.Resources[i].Fields))
		for path, f := range b.Spec.Resources[i].Fields {
			r.Fields[path] = f
		}
		// nil stays nil (not an empty map): Envelope is omitempty, and a
		// resource that never declared the key must round-trip that way.
		if b.Spec.Resources[i].Envelope != nil {
			r.Envelope = make(map[string]Field, len(b.Spec.Resources[i].Envelope))
			for path, f := range b.Spec.Resources[i].Envelope {
				r.Envelope[path] = f
			}
		}
		// Annotations: same map, same omitempty, same nil-stays-nil rule.
		if b.Spec.Resources[i].Annotations != nil {
			r.Annotations = make(map[string]Field, len(b.Spec.Resources[i].Annotations))
			for key, f := range b.Spec.Resources[i].Annotations {
				r.Annotations[key] = f
			}
		}
		cp.Spec.Resources[i] = r
	}

	return &cp
}

// copyParameter returns a copy of p sharing no mutable state with it: the
// enum slice and — for a typed object parameter — the member map, member
// declarations included. Recursive so a (validation-refused, but
// representable) deeper nesting still cannot alias; the recursion terminates
// because the declaration is a finite tree.
func copyParameter(p Parameter) Parameter {
	p.Enum = append([]string(nil), p.Enum...)
	if p.Properties != nil {
		props := make(map[string]Parameter, len(p.Properties))
		for name, member := range p.Properties {
			props[name] = copyParameter(member)
		}
		p.Properties = props
	}
	return p
}

// whenParam returns the parameter a when expression references, or "" when
// the expression is empty or unparseable. Unparseable never happens on a
// validated blueprint; tolerating it here keeps the referencer scans total.
func whenParam(when string) string {
	if when == "" {
		return ""
	}
	param, _, _, err := ParseWhen(when)
	if err != nil {
		return ""
	}
	return param
}

// ReferencingResources returns the names of every resource that references
// params.<name> — through a field's From, an envelope entry's From, an
// annotation entry's From, its own ForEach loop bound, or its When condition
// — in resource order, each resource named at most once.
func (b *Blueprint) ReferencingResources(name string) []string {
	want := "params." + name
	var refs []string
	for _, r := range b.Spec.Resources {
		if r.ForEach == want || whenParam(r.When) == name ||
			anyFrom(r.Fields, want) || anyFrom(r.Envelope, want) || anyFrom(r.Annotations, want) {
			refs = append(refs, r.Name)
		}
	}
	return refs
}

// anyFrom reports whether any entry in fields wires from want — exactly, or
// through a member reference below it (want == "params.obj" matches
// "params.obj.member": a member wire references the parameter as surely as a
// whole-value wire does, so deleting the parameter would dangle it).
func anyFrom(fields map[string]Field, want string) bool {
	for _, f := range fields {
		if f.From == want || strings.HasPrefix(f.From, want+".") {
			return true
		}
	}
	return false
}

// StatusReferencingResources returns the names of every resource that
// references resources.<name>'s status — through a field's or annotation's
// From, or through its own forEach loop bound
// (forEach: resources.<name>.status.<path>) — in resource order. It is the
// resources.<name>.status.* counterpart of ReferencingResources, and matches
// by parsing each reference with the same
// grammar the validator uses rather than by substring, so a params reference
// (a different namespace) or a name that merely shares a prefix can never
// match.
func (b *Blueprint) StatusReferencingResources(name string) []string {
	var refs []string
	for _, r := range b.Spec.Resources {
		// An observed-count loop bound references the target's status as
		// surely as a field wire does: deleting the target would leave a
		// forEach whose guard chain stays false forever — a resource that
		// silently never fans out.
		if target, _, ok := StatusRef(r.ForEach); ok && target == name {
			refs = append(refs, r.Name)
			continue
		}
		if anyStatusFrom(r.Fields, name) || anyStatusFrom(r.Annotations, name) || anyStatusFrom(r.Envelope, name) {
			refs = append(refs, r.Name)
		}
	}
	return refs
}

// rawReferencesResource checks whether a raw template/expression string contains
// references to the given resource name.
func rawReferencesResource(raw, name string) bool {
	if raw == "" || name == "" {
		return false
	}
	return strings.Contains(raw, `"`+name+`"`) ||
		strings.Contains(raw, `'`+name+`'`) ||
		strings.Contains(raw, "`"+name+"`") ||
		strings.Contains(raw, "resources."+name+".") ||
		strings.Contains(raw, "resources."+name+" ") ||
		strings.Contains(raw, "resources."+name+"}") ||
		strings.Contains(raw, ".observed.resources."+name) ||
		strings.Contains(raw, "$observed.resources."+name)
}

// rewriteRawResource replaces references to from with to in a raw template/expression.
func rewriteRawResource(raw, from, to string) string {
	if raw == "" || from == "" || to == "" || from == to {
		return raw
	}
	r := raw
	r = strings.ReplaceAll(r, `"`+from+`"`, `"`+to+`"`)
	r = strings.ReplaceAll(r, `'`+from+`'`, `'`+to+`'`)
	r = strings.ReplaceAll(r, "`"+from+"`", "`"+to+"`")
	r = strings.ReplaceAll(r, "resources."+from+".", "resources."+to+".")
	r = strings.ReplaceAll(r, ".observed.resources."+from, ".observed.resources."+to)
	r = strings.ReplaceAll(r, "$observed.resources."+from, "$observed.resources."+to)
	return r
}

// anyStatusFrom reports whether any entry in fields wires from resource
// name's status or references it in raw text.
func anyStatusFrom(fields map[string]Field, name string) bool {
	for _, f := range fields {
		if f.From != "" {
			if ref, err := ParseFrom(f.From); err == nil && ref.Resource == name {
				return true
			}
		}
		if f.Raw != "" && rawReferencesResource(f.Raw, name) {
			return true
		}
	}
	return false
}

// resourceIndex returns the position of the resource named name, or -1.
func (b *Blueprint) resourceIndex(name string) int {
	for i, r := range b.Spec.Resources {
		if r.Name == name {
			return i
		}
	}
	return -1
}

// renameParamRef rewrites a from: reference from oldRef (params.<old>) to
// newRef when it references that parameter — exactly, or through a member
// (oldRef + "." + member). Any other value, including a params.<old>X
// lookalike sharing the prefix as a string, comes back unchanged.
func renameParamRef(from, oldRef, newRef string) (string, bool) {
	if from == oldRef {
		return newRef, true
	}
	if rest, ok := strings.CutPrefix(from, oldRef+"."); ok {
		return newRef + "." + rest, true
	}
	return from, false
}

// RenameResource renames a composed resource and rewrites every status wire
// (resources.<from>.status.<path>) — in fields AND annotations — to point at
// the new name, the same contract RenameParameter has for params.<name>
// references. Params references are a different namespace and are never
// touched. It fails if from is not declared, if to is already declared
// (unless to == from — the same blur-submit no-op RenameParameter absorbs),
// or if the resulting blueprint does not validate (e.g. to is not a DNS
// label); in every failure case the receiver is left unchanged.
func (b *Blueprint) RenameResource(from, to string) error {
	idx := b.resourceIndex(from)
	if idx < 0 {
		return fmt.Errorf("rename resource: %q is not declared", from)
	}
	if from == to {
		return nil
	}
	if b.resourceIndex(to) >= 0 {
		return fmt.Errorf("rename resource: %q is already declared", to)
	}

	cp := b.deepCopy()
	cp.Spec.Resources[idx].Name = to
	oldPrefix := "resources." + from + ".status."
	newPrefix := "resources." + to + ".status."
	oldMetaRef := "resources." + from + ".metadata.name"
	newMetaRef := "resources." + to + ".metadata.name"
	for i, r := range cp.Spec.Resources {
		for path, f := range r.Fields {
			if rest, ok := strings.CutPrefix(f.From, oldPrefix); ok {
				f.From = newPrefix + rest
				cp.Spec.Resources[i].Fields[path] = f
			} else if f.From == oldMetaRef {
				f.From = newMetaRef
				cp.Spec.Resources[i].Fields[path] = f
			}
			if f.Raw != "" && rawReferencesResource(f.Raw, from) {
				f.Raw = rewriteRawResource(f.Raw, from, to)
				cp.Spec.Resources[i].Fields[path] = f
			}
		}
		for path, f := range r.Envelope {
			if rest, ok := strings.CutPrefix(f.From, oldPrefix); ok {
				f.From = newPrefix + rest
				cp.Spec.Resources[i].Envelope[path] = f
			} else if f.From == oldMetaRef {
				f.From = newMetaRef
				cp.Spec.Resources[i].Envelope[path] = f
			}
			if f.Raw != "" && rawReferencesResource(f.Raw, from) {
				f.Raw = rewriteRawResource(f.Raw, from, to)
				cp.Spec.Resources[i].Envelope[path] = f
			}
		}
		// An annotation wire is the same reference shape and gets the same
		// rewrite: a dangling one would leave a guard chain that indexes an
		// observed key that can never exist — an annotation that silently
		// never materialises.
		for key, f := range r.Annotations {
			if rest, ok := strings.CutPrefix(f.From, oldPrefix); ok {
				f.From = newPrefix + rest
				cp.Spec.Resources[i].Annotations[key] = f
			} else if f.From == oldMetaRef {
				f.From = newMetaRef
				cp.Spec.Resources[i].Annotations[key] = f
			}
			if f.Raw != "" && rawReferencesResource(f.Raw, from) {
				f.Raw = rewriteRawResource(f.Raw, from, to)
				cp.Spec.Resources[i].Annotations[key] = f
			}
		}
		// An observed-count loop bound (forEach:
		// resources.<from>.status.<path>) references the renamed resource
		// exactly the way a field wire does, and a dangling one would leave a
		// guard chain that stays false forever — a fan-out that silently
		// never happens. Same prefix-exact rewrite, same namespace rules: a
		// params.<name> forEach is never touched.
		if rest, ok := strings.CutPrefix(r.ForEach, oldPrefix); ok {
			cp.Spec.Resources[i].ForEach = newPrefix + rest
		}
	}
	for tName, body := range cp.Spec.Templates {
		if rawReferencesResource(body, from) {
			cp.Spec.Templates[tName] = rewriteRawResource(body, from, to)
		}
	}

	if err := cp.Validate(); err != nil {
		return fmt.Errorf("rename resource %q to %q: %w", from, to, err)
	}

	*b = *cp
	return nil
}

// DeleteResource removes a composed resource. It refuses while any other
// resource still wires from the deleted resource's status (naming every
// referencing resource, the same one-round-trip contract DeleteParameter
// has), when the resource is not declared, or when the resulting blueprint
// does not validate. In every failure case the receiver is left unchanged.
func (b *Blueprint) DeleteResource(name string) error {
	idx := b.resourceIndex(name)
	if idx < 0 {
		return fmt.Errorf("delete resource: %q is not declared", name)
	}
	if refs := b.StatusReferencingResources(name); len(refs) > 0 {
		quoted := make([]string, len(refs))
		for i, r := range refs {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		return fmt.Errorf("delete resource %q: its status, metadata, or raw reference is still wired into resources %s",
			name, strings.Join(quoted, ", "))
	}
	for tName, body := range b.Spec.Templates {
		if rawReferencesResource(body, name) {
			return fmt.Errorf("delete resource %q: still referenced by template %q", name, tName)
		}
	}

	cp := b.deepCopy()
	cp.Spec.Resources = append(cp.Spec.Resources[:idx], cp.Spec.Resources[idx+1:]...)
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("delete resource %q: %w", name, err)
	}

	*b = *cp
	return nil
}

// AddParameter declares a new XRD parameter. It fails if name is already
// declared, or if the resulting blueprint does not validate; in either case
// the receiver is left unchanged.
func (b *Blueprint) AddParameter(name string, p Parameter) error {
	if _, exists := b.Spec.XRD.Parameters[name]; exists {
		return fmt.Errorf("add parameter: %q is already declared", name)
	}

	cp := b.deepCopy()
	cp.Spec.XRD.Parameters[name] = p
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("add parameter %q: %w", name, err)
	}

	*b = *cp
	return nil
}

// RenameParameter renames an XRD parameter and rewrites every resource
// reference (params.<from>) to point at the new name — field From references,
// envelope From references, annotation From references, when conditions and
// forEach loop bounds. Field keys are untouched
// -- only the reference value changes. It fails if from
// is not declared, if to is already declared (unless to == from -- see
// below), or if the resulting blueprint does not validate; in every failure
// case the receiver is left unchanged.
//
// from == to is a no-op success, not a collision error: a blur-submit UI
// routinely resubmits an unchanged name, and requiring every caller to
// special-case "did the name actually change" before calling this is a
// burden the API should absorb instead. An unknown from still errors even
// when from == to, since there is nothing to rename.
func (b *Blueprint) RenameParameter(from, to string) error {
	p, exists := b.Spec.XRD.Parameters[from]
	if !exists {
		return fmt.Errorf("rename parameter: %q is not declared", from)
	}
	if from == to {
		return nil
	}
	if _, collides := b.Spec.XRD.Parameters[to]; collides {
		return fmt.Errorf("rename parameter: %q is already declared", to)
	}

	cp := b.deepCopy()
	delete(cp.Spec.XRD.Parameters, from)
	cp.Spec.XRD.Parameters[to] = p

	oldRef, newRef := "params."+from, "params."+to
	for i, r := range cp.Spec.Resources {
		// A forEach loop bound is a params.<name> reference exactly like a
		// field's From, and gets the same rewrite discipline: a dangling
		// forEach would emit a Composition whose loop bound dereferences a
		// parameter that no longer exists, which under missingkey=error can
		// never render.
		if r.ForEach == oldRef {
			cp.Spec.Resources[i].ForEach = newRef
		}
		// A when condition references its parameter too, and a dangling one
		// is the same never-renders failure. Rebuilt through ParseWhen's
		// canonical form rather than string-replaced, so the rewrite cannot
		// touch a literal that happens to contain "params.<from>".
		if whenParam(r.When) == from {
			_, op, literal, _ := ParseWhen(r.When) // parseable: whenParam just parsed it
			if op == "" {
				cp.Spec.Resources[i].When = newRef
			} else {
				cp.Spec.Resources[i].When = fmt.Sprintf("%s %s %q", newRef, op, literal)
			}
		}
		// The rewrite covers member references too (params.<from>.<member>):
		// there are no member-level rename routes in v1, so the
		// parameter-level rename is the one rewrite member wires get, and it
		// rewrites the PREFIX. renameParamRef is exact about the boundary —
		// params.<from>X is a different parameter, never rewritten.
		for path, f := range r.Fields {
			if rewritten, changed := renameParamRef(f.From, oldRef, newRef); changed {
				f.From = rewritten
				cp.Spec.Resources[i].Fields[path] = f
			}
		}
		// Envelope froms are the same reference shape and get the same
		// rewrite: a dangling one would emit a Composition dereferencing a
		// parameter that no longer exists, unrenderable under
		// missingkey=error.
		for path, f := range r.Envelope {
			if rewritten, changed := renameParamRef(f.From, oldRef, newRef); changed {
				f.From = rewritten
				cp.Spec.Resources[i].Envelope[path] = f
			}
		}
		// Annotation froms too, for exactly the same reason.
		for key, f := range r.Annotations {
			if f.From == oldRef {
				f.From = newRef
				cp.Spec.Resources[i].Annotations[key] = f
			}
		}
	}

	if err := cp.Validate(); err != nil {
		return fmt.Errorf("rename parameter %q to %q: %w", from, to, err)
	}

	*b = *cp
	return nil
}

// SetParameter replaces an existing XRD parameter's declaration in place. It
// fails if name is not already declared, or if the resulting blueprint does
// not validate; in either case the receiver is left unchanged.
func (b *Blueprint) SetParameter(name string, p Parameter) error {
	if _, exists := b.Spec.XRD.Parameters[name]; !exists {
		return fmt.Errorf("set parameter: %q is not declared", name)
	}

	cp := b.deepCopy()
	cp.Spec.XRD.Parameters[name] = p
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("set parameter %q: %w", name, err)
	}

	*b = *cp
	return nil
}

// DeleteParameter removes an XRD parameter declaration. It refuses when any
// resource field still references the parameter (naming every referencing
// resource, rather than cascading the delete into those fields, so a user
// can fix every reference in one round-trip instead of discovering them one
// at a time), when the parameter is not declared, or when the resulting
// blueprint does not validate (e.g. deleting providerName from a Namespaced
// XRD). In every failure case the receiver is left unchanged.
func (b *Blueprint) DeleteParameter(name string) error {
	if _, exists := b.Spec.XRD.Parameters[name]; !exists {
		return fmt.Errorf("delete parameter: %q is not declared", name)
	}
	if refs := b.ReferencingResources(name); len(refs) > 0 {
		quoted := make([]string, len(refs))
		for i, r := range refs {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		return fmt.Errorf("delete parameter %q: still referenced by resources %s", name, strings.Join(quoted, ", "))
	}

	cp := b.deepCopy()
	delete(cp.Spec.XRD.Parameters, name)
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("delete parameter %q: %w", name, err)
	}

	*b = *cp
	return nil
}
