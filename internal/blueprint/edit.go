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

	cp.Spec.XRD.Parameters = make(map[string]Parameter, len(b.Spec.XRD.Parameters))
	for name, p := range b.Spec.XRD.Parameters {
		p.Enum = append([]string(nil), p.Enum...)
		cp.Spec.XRD.Parameters[name] = p
	}

	cp.Spec.Resources = make([]Resource, len(b.Spec.Resources))
	for i, r := range b.Spec.Resources {
		r.Fields = make(map[string]Field, len(b.Spec.Resources[i].Fields))
		for path, f := range b.Spec.Resources[i].Fields {
			r.Fields[path] = f
		}
		cp.Spec.Resources[i] = r
	}

	return &cp
}

// referencingResources returns the names of every resource that references
// params.<name> — through a field's From or through its own ForEach loop
// bound — in resource order, each resource named at most once.
func (b *Blueprint) referencingResources(name string) []string {
	want := "params." + name
	var refs []string
	for _, r := range b.Spec.Resources {
		if r.ForEach == want {
			refs = append(refs, r.Name)
			continue
		}
		for _, f := range r.Fields {
			if f.From == want {
				refs = append(refs, r.Name)
				break
			}
		}
	}
	return refs
}

// statusReferencingResources returns the names of every resource that
// references resources.<name>.status.<...> through a field's From, in
// resource order, each resource named at most once. It is the cross-resource
// mirror of referencingResources: the same discipline params refs and
// forEach refs get, applied to status wires.
func (b *Blueprint) statusReferencingResources(name string) []string {
	var refs []string
	for _, r := range b.Spec.Resources {
		for _, f := range r.Fields {
			if target, _, ok := StatusRef(f.From); ok && target == name {
				refs = append(refs, r.Name)
				break
			}
		}
	}
	return refs
}

// RenameResource renames a composed resource and rewrites every
// cross-resource status reference (resources.<from>.status.<path>) to point
// at the new name. Field keys and every other reference grammar are
// untouched — only the resource-name half of a status reference changes. It
// fails if from is not declared, if to is already declared (unless
// to == from — a blur-submit UI resubmits an unchanged name, exactly as
// RenameParameter documents), or if the resulting blueprint does not
// validate; in every failure case the receiver is left unchanged.
//
// This matters for the same reason RenameParameter's rewrite does: a
// dangling resources.<old>.status.<path> reference would emit a Composition
// whose hasKey guard chain looks up an observed key that can never exist
// again — the field silently stops materialising, forever, with every gate
// green.
func (b *Blueprint) RenameResource(from, to string) error {
	idx := -1
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == from {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("rename resource: %q is not declared", from)
	}
	if from == to {
		return nil
	}
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == to {
			return fmt.Errorf("rename resource: %q is already declared", to)
		}
	}

	cp := b.deepCopy()
	cp.Spec.Resources[idx].Name = to

	oldPrefix := statusRefPrefix + from + ".status."
	newPrefix := statusRefPrefix + to + ".status."
	for i, r := range cp.Spec.Resources {
		for path, f := range r.Fields {
			if rest, ok := strings.CutPrefix(f.From, oldPrefix); ok {
				f.From = newPrefix + rest
				cp.Spec.Resources[i].Fields[path] = f
			}
		}
	}

	if err := cp.Validate(); err != nil {
		return fmt.Errorf("rename resource %q to %q: %w", from, to, err)
	}

	*b = *cp
	return nil
}

// DeleteResource removes a composed resource. It refuses when any other
// resource still references its status (naming every referencing resource,
// the same one-round-trip courtesy DeleteParameter gives), when the resource
// is not declared, or when the resulting blueprint does not validate. In
// every failure case the receiver is left unchanged.
func (b *Blueprint) DeleteResource(name string) error {
	idx := -1
	for i := range b.Spec.Resources {
		if b.Spec.Resources[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("delete resource: %q is not declared", name)
	}
	if refs := b.statusReferencingResources(name); len(refs) > 0 {
		quoted := make([]string, len(refs))
		for i, r := range refs {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		return fmt.Errorf("delete resource %q: its status is still referenced by resources %s",
			name, strings.Join(quoted, ", "))
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
// reference (params.<from>) to point at the new name — both field From
// references and forEach loop bounds. Field keys are untouched -- only the
// reference value changes. It fails if from
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
		for path, f := range r.Fields {
			if f.From == oldRef {
				f.From = newRef
				cp.Spec.Resources[i].Fields[path] = f
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
	if refs := b.referencingResources(name); len(refs) > 0 {
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
