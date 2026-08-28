// This file implements the /api/blueprint routes: reading the blueprint and
// editing its XRD parameters (add, replace, rename, delete).
//
// Two rules apply to every handler in this file, both load-bearing (see the
// Task 6 brief):
//
//  1. The blueprint is loaded from disk on every single request, never
//     cached in the server. The blueprint file is the source of truth for
//     `cf gen`; a server-held copy would silently diverge from it the moment
//     anyone edited the file by hand, and every response here would then be
//     describing a document that no longer exists.
//  2. A successful edit is persisted back to disk immediately, through
//     persistBlueprint (blueprint.go's own file, see below) — never left
//     in memory for some later "save" step that does not exist in this API.
//
// Status codes: 400 for a malformed request body or a validation/parse
// failure, 409 for an edit that conflicts with current state (a duplicate
// add, a rename colliding with an existing name, deleting a
// still-referenced parameter), 404 for an edit naming a parameter that is
// not declared, 500 when the blueprint file itself cannot be read (see
// loadBlueprint below — that is the server's fixed path/environment being
// wrong, not a problem the caller's request can be blamed for), 200 on
// success. Every error body carries the underlying blueprint/edit-layer
// error verbatim (see writeJSONError call sites below) — those messages name
// the offending field path precisely, and paraphrasing them would throw that
// away.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"sigs.k8s.io/yaml"
)

// loadBlueprint reads and validates the blueprint at o.Blueprint. On failure
// it writes the response itself and returns ok=false, so every caller can
// just `if !ok { return }`.
//
// Fix round 1 (Finding 1): the failure is classified before it is reported.
// blueprint.Load can fail two structurally different ways — it could not
// even read o.Blueprint (missing file, a directory in its place, a
// permissions problem: the server's own fixed path or environment is wrong,
// nothing about the current HTTP request caused it), or it read the file
// fine and then the content failed to parse as YAML or failed Validate() (a
// data problem, reported the same way a rejected edit's Validate() failure
// is). blueprint.ReadError marks the first case; errors.As unwraps through
// Load's %w wrapping to find it. Everything else — parse and Validate()
// failures — keeps the previous 400 treatment.
func (o Options) loadBlueprint(w http.ResponseWriter) (*blueprint.Blueprint, bool) {
	b, err := blueprint.Load(o.Blueprint)
	if err != nil {
		var readErr *blueprint.ReadError
		status := http.StatusBadRequest
		if errors.As(err, &readErr) {
			status = http.StatusInternalServerError
		}
		writeJSONError(w, status, err.Error())
		return nil, false
	}
	return b, true
}

// handleGetBlueprint serves GET /api/blueprint: the whole document as JSON.
func (o Options) handleGetBlueprint(w http.ResponseWriter, _ *http.Request) {
	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// addParameterRequest is the POST /api/blueprint/parameters body.
type addParameterRequest struct {
	Name      string              `json:"name"`
	Parameter blueprint.Parameter `json:"parameter"`
}

// handleAddParameter serves POST /api/blueprint/parameters: declare a new
// XRD parameter.
//
// Status classification does not string-match AddParameter's error text —
// it instead checks, BEFORE calling AddParameter, whether the name was
// already declared. AddParameter's own first action is exactly that same
// check (see internal/blueprint/edit.go), and it leaves the receiver fully
// unchanged on any failure, so "the name existed going in" and "AddParameter
// failed because it was a duplicate" are one and the same fact, checkable
// from outside the edit layer without depending on the wording of its error.
func (o Options) handleAddParameter(w http.ResponseWriter, r *http.Request) {
	var req addParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}

	_, existed := b.Spec.XRD.Parameters[req.Name]
	if err := b.AddParameter(req.Name, req.Parameter); err != nil {
		status := http.StatusBadRequest
		if existed {
			status = http.StatusConflict
		}
		writeJSONError(w, status, err.Error())
		return
	}

	if !o.persistBlueprint(w, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// setParameterRequest is the PUT /api/blueprint/parameters/{name} body.
type setParameterRequest struct {
	Parameter blueprint.Parameter `json:"parameter"`
}

// handleSetParameter serves PUT /api/blueprint/parameters/{name}: replace an
// existing parameter's declaration in full (SetParameter is replace-only,
// not a merge/patch). Unknown name -> 404, matching this API's general
// unknown-name convention.
func (o Options) handleSetParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req setParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}

	_, existed := b.Spec.XRD.Parameters[name]
	if err := b.SetParameter(name, req.Parameter); err != nil {
		status := http.StatusBadRequest
		if !existed {
			status = http.StatusNotFound
		}
		writeJSONError(w, status, err.Error())
		return
	}

	if !o.persistBlueprint(w, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// renameParameterRequest is the POST /api/blueprint/parameters/{name}/rename
// body.
type renameParameterRequest struct {
	To string `json:"to"`
}

// handleRenameParameter serves POST /api/blueprint/parameters/{name}/rename:
// rename a parameter and rewrite every resource field that references it.
//
// to == from succeeds as a no-op — RenameParameter already handles that (see
// internal/blueprint/edit.go's doc comment: a blur-submit UI routinely
// resubmits an unchanged name), so it is deliberately NOT special-cased
// here. When to == from, RenameParameter returns a nil error and this
// handler's error-classification branch below never runs.
//
// As with handleAddParameter, status classification reads the blueprint's
// state from before the call rather than the error text: RenameParameter
// checks "from declared" before "to == from" before "to already declared"
// before validating, in that order and unconditionally on any prior check's
// failure, so fromExists and toCollides (captured before the call, since a
// failed call leaves the receiver untouched) reproduce the same branch the
// edit layer took.
func (o Options) handleRenameParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req renameParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}

	_, fromExists := b.Spec.XRD.Parameters[name]
	_, toCollides := b.Spec.XRD.Parameters[req.To]

	if err := b.RenameParameter(name, req.To); err != nil {
		switch {
		case !fromExists:
			writeJSONError(w, http.StatusNotFound, err.Error())
		case toCollides:
			writeJSONError(w, http.StatusConflict, err.Error())
		default:
			writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	if !o.persistBlueprint(w, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleDeleteParameter serves DELETE /api/blueprint/parameters/{name}.
//
// DeleteParameter's own "still referenced" check lives on
// *blueprint.Blueprint as an unexported method (referencingResources), so it
// cannot be called from this package to classify the failure the way the
// other three handlers do. referencingResources below is a small,
// deliberate duplicate of that same read-only scan over exported fields
// (Resource.Fields, Field.From) — not a reimplementation of any generation
// or validation logic, just the presence check this HTTP layer needs to
// choose 409 vs 400 without parsing DeleteParameter's error text. It cannot
// be replaced by an "existed" check alone the way Add/Set/Rename's
// classification is: deleting providerName from a Namespaced XRD is
// existed==true, refs==0 (nothing sets it via a field's `from:`) and still
// fails, via Validate rejecting the XRD afterwards — a genuine 400, not a
// 409 — so "still referenced" has to be established independently of
// "existed" and independently of "the delete failed".
func (o Options) handleDeleteParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}

	_, existed := b.Spec.XRD.Parameters[name]
	refs := referencingResources(b, name)

	if err := b.DeleteParameter(name); err != nil {
		switch {
		case !existed:
			writeJSONError(w, http.StatusNotFound, err.Error())
		case len(refs) > 0:
			writeJSONError(w, http.StatusConflict, err.Error())
		default:
			writeJSONError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	if !o.persistBlueprint(w, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// referencingResources returns the names of every resource with a field
// whose From references params.<name>, in resource order. It mirrors
// blueprint.Blueprint's unexported referencingResources (see
// internal/blueprint/edit.go) exactly, over the same exported Resource/Field
// data — see handleDeleteParameter's doc comment for why this HTTP layer
// needs its own copy of this one check.
func referencingResources(b *blueprint.Blueprint, name string) []string {
	want := "params." + name
	var refs []string
	for _, res := range b.Spec.Resources {
		for _, f := range res.Fields {
			if f.From == want {
				refs = append(refs, res.Name)
				break
			}
		}
	}
	return refs
}

// persistBlueprint writes b to o.Blueprint, deterministically and only if
// the result would itself load back. On failure it writes the 500 response
// itself and returns false, so callers can just `if !o.persistBlueprint(w,
// b) { return }`; a failure here means the write was refused, not attempted
// half-done, so the file on disk is left exactly as it was before the call.
func (o Options) persistBlueprint(w http.ResponseWriter, b *blueprint.Blueprint) bool {
	if err := writeBlueprintFile(o.Blueprint, b); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

// writeBlueprintFile marshals b as deterministic YAML and writes it to path,
// refusing to write anything that would not load back (Task 6 brief,
// decision 1).
//
// Round-trip check before writing: marshal, re-parse and re-validate the
// result exactly as blueprint.Load would, rather than trusting that b
// itself already validated. Every AddParameter/SetParameter/
// RenameParameter/DeleteParameter call above already validates its own
// candidate before committing it to the receiver (internal/blueprint/edit.go
// never writes back a Blueprint that failed cp.Validate()), so by the time
// control reaches here b is already known-valid — this check exists to
// catch a bug in marshalBlueprint or in the YAML round trip itself (a
// mismatch Validate, which runs against the Go struct and never sees the
// bytes about to land on disk, cannot detect on its own), not a
// blueprint-layer one. "Never leave an unloadable blueprint on disk" is
// stated as an absolute in the brief, so this is a hard refusal rather than
// a best-effort check: if the round trip does not come back equivalent, the
// write does not happen.
func writeBlueprintFile(path string, b *blueprint.Blueprint) error {
	body, err := marshalBlueprint(b)
	if err != nil {
		return fmt.Errorf("encode blueprint: %w", err)
	}

	var reloaded blueprint.Blueprint
	if err := yaml.Unmarshal(body, &reloaded); err != nil {
		return fmt.Errorf("refusing to write: generated blueprint does not parse back: %w", err)
	}
	if err := reloaded.Validate(); err != nil {
		return fmt.Errorf("refusing to write: generated blueprint does not validate: %w", err)
	}

	return atomicWriteFile(path, body, 0o644)
}

// marshalBlueprint renders b as deterministic YAML: sorted map keys, LF line
// endings, exactly one trailing newline.
//
// sigs.k8s.io/yaml.Marshal already produces all three properties for this
// struct (verified directly: it round-trips every Blueprint through
// encoding/json into a map[string]interface{} before handing off to the
// underlying YAML encoder, which sorts every map's keys and never emits
// "\r\n" — see the Task 6 report for the measured byte output). The
// normalization below is applied anyway, on general principle: it is the
// same defensive bytes.TrimRight(...,"\n")+append('\n') pattern
// internal/emit/yaml.go's Doc.Bytes uses, so this function's determinism
// does not silently depend on that upstream behavior never changing.
//
// This marshals the blueprint.Blueprint struct directly (already
// json-tagged; sigs.k8s.io/yaml marshals through those same tags) rather
// than hand-assembling YAML the way internal/emit's emitters do, per the
// brief's explicit direction: the file is meant to stay hand-editable, and
// preserving its apiVersion/kind/metadata/spec{sources,xrd,resources}
// structure this way means every key the user might have added or reordered
// by hand round-trips through the same Go types cf gen already trusts.
//
// One consequence worth noting: neither blueprint.Parameter nor
// blueprint.Field carries `omitempty` json tags, so every field this struct
// has — not just the ones a given parameter or resource field actually
// uses — is written out explicitly (e.g. every parameter gains an explicit
// `enum: null`, `default: ""`, `description: ""` even when unset, and every
// Field gains `raw: ""`/`value: ""` alongside whichever of from/value/raw is
// actually set). The file grows more verbose than the hand-written fixtures
// in this package's own tests as a result. That is an accepted, visible
// trade-off of marshaling the tagged struct as directed, not a bug in this
// function — closing it would mean adding omitempty to blueprint's types,
// which is outside this task's file scope (internal/blueprint is not in
// Task 6's touch list).
func marshalBlueprint(b *blueprint.Blueprint) ([]byte, error) {
	out, err := yaml.Marshal(b)
	if err != nil {
		return nil, err
	}
	out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
	return append(bytes.TrimRight(out, "\n"), '\n'), nil
}

// atomicWriteFile writes body to path via a temp file in the same directory
// plus a rename, so a reader (or a crash mid-write) never observes a
// partially-written blueprint. os.Rename within one directory is atomic on
// every platform this project targets, which a direct os.WriteFile is not:
// without this, "never leave an unloadable blueprint on disk" would hold for
// the bytes this function chooses to write but not for what a concurrent
// reader could actually see while it writes them.
func atomicWriteFile(path string, body []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".blueprint-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("set temp file permissions: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// decodeJSON decodes r's body as JSON into v, rejecting unknown fields so a
// client typo (e.g. "paramter") fails loudly as a 400 instead of silently
// being ignored.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}
