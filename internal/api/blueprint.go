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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
	"sigs.k8s.io/yaml"
)

// loadBlueprint reads and validates the blueprint at srv.Blueprint. On failure
// it writes the response itself and returns ok=false, so every caller can
// just `if !ok { return }`.
//
// Fix round 1 (Finding 1): the failure is classified before it is reported.
// blueprint.Load can fail two structurally different ways — it could not
// even read srv.Blueprint (missing file, a directory in its place, a
// permissions problem: the server's own fixed path or environment is wrong,
// nothing about the current HTTP request caused it), or it read the file
// fine and then the content failed to parse as YAML or failed Validate() (a
// data problem, reported the same way a rejected edit's Validate() failure
// is). blueprint.ReadError marks the first case; errors.As unwraps through
// Load's %w wrapping to find it. Everything else — parse and Validate()
// failures — keeps the previous 400 treatment.
func (srv *server) loadBlueprint(w http.ResponseWriter) (*blueprint.Blueprint, bool) {
	b, err := blueprint.Load(srv.Blueprint)
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
func (srv *server) handleGetBlueprint(w http.ResponseWriter, _ *http.Request) {
	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handlePutBlueprint serves PUT /api/blueprint: replace the whole document —
// the same shape GET returns, sent back whole. This is the canvas's route:
// it keeps the document client-side (nodes, wires, field values) and has no
// per-field edit to make against this API's narrower parameter routes, so it
// PUTs its entire in-memory document and follows up with POST /api/generate.
//
// Unlike every other mutating handler in this file, there is no load step:
// a full replace does not need the document currently on disk for anything
// (not even to answer 500 vs 400 — see below), so this handler's sequence is
// decode -> validate -> persist rather than load -> edit -> persist. It is
// still, deliberately, held to that same discipline everywhere it applies:
//
//   - decodeJSON, like every other handler here, uses DisallowUnknownFields.
//     A typo or a stray field the frontend's document does not actually have
//     fails loudly as a 400 rather than being silently dropped on the next
//     persist — the same "unknown top-level keys" behaviour the parameter
//     routes give a request body, extended to the document itself since PUT
//     /api/blueprint's body IS the document.
//   - The decoded document is validated with the same Blueprint.Validate the
//     edit layer calls internally (AddParameter/SetParameter/... in
//     internal/blueprint/edit.go all validate their candidate before
//     committing it) before anything is written, and its error is surfaced
//     verbatim — no wrapping — matching the edit routes' rule that a
//     validation failure's message names the offending field path precisely
//     and paraphrasing it would throw that away.
//   - srv.mu is held across the whole operation, not just the write. PUT does
//     not read-modify-write against the file the way the parameter handlers
//     do, but a concurrent parameter POST does: it loads the current file,
//     edits its own copy, and persists that copy back. Without the lock, a
//     PUT landing in the gap between that load and that persist would be
//     silently overwritten by the parameter POST's edit of the
//     now-stale document it read before the PUT ran — this PUT's caller gets
//     a 200, and the change is gone a moment later. Serializing against the
//     same mutex the other handlers use closes that the same way it already
//     closes it for two concurrent parameter edits.
//   - persistBlueprint is the same marshal-then-atomic-rename path every
//     other mutating handler uses (see marshalBlueprint, atomicWriteFile):
//     deterministic YAML, refuse-if-it-would-not-load-back, never a partial
//     write visible to a concurrent reader.
//
// Status codes: 400 for malformed JSON or a validation failure (the file is
// left untouched in both cases — nothing above persistBlueprint mutates
// anything on disk), 500 for an I/O failure persisting the result (the same
// split loadBlueprint/persistBlueprint use elsewhere: an unreadable/
// unwritable fixed server path is this server's own fault, not the
// caller's), 200 with the persisted document on success.
func (srv *server) handlePutBlueprint(w http.ResponseWriter, r *http.Request) {
	var b blueprint.Blueprint
	if err := decodeJSON(r, &b); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// load -> edit -> persist (here, decode -> validate -> persist) has to be
	// atomic against the other mutating handlers, or a concurrent edit reads
	// the document this PUT is about to replace and silently overwrites this
	// PUT's write with its own edit of that now-stale copy. See srv.mu and
	// this handler's doc comment above.
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if err := b.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	if err := srv.syncBlueprintSourcesLocked(ctx, &b); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to sync sources: %v", err))
		return
	}
	crds, err := srv.loadSourceCRDs(&b)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := srv.validateBlueprintAgainstCRDs(&b, crds); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := writeBlueprintFile(srv.Blueprint, &b); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, &b)
}

// handleImportBlueprint is POST /api/blueprint/import: the body is raw
// blueprint YAML — the on-disk DSL format — run through the same
// strict-decode + Validate gate a file gets (blueprint.Parse), persisted,
// and returned as the full JSON document. The GUI's import button lands
// here; the response shape is identical to PUT's, so no new contract
// fixture is needed.
func (srv *server) handleImportBlueprint(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// ParseAny also unwraps a package.yaml stream (cf package's yaml form),
	// recovering the blueprint from its embedded-source annotation
	b, err := blueprint.ParseAny(body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if !srv.persistBlueprint(w, r, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// addParameterRequest is the POST /api/blueprint/parameters body.
type addParameterRequest struct {
	Name      string              `json:"name"`
	Parameter blueprint.Parameter `json:"parameter"`
}

// mutate loads the blueprint under lock, executes fn to perform the edit,
// writes an error response if fn returns an error, persists the modified blueprint,
// and responds with 200 OK and the updated blueprint.
func (srv *server) mutate(w http.ResponseWriter, r *http.Request, fn func(*blueprint.Blueprint) (int, error)) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}

	if status, err := fn(b); err != nil {
		writeJSONError(w, status, err.Error())
		return
	}

	if !srv.persistBlueprint(w, r, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleAddParameter serves POST /api/blueprint/parameters: declare a new
// XRD parameter.
func (srv *server) handleAddParameter(w http.ResponseWriter, r *http.Request) {
	var req addParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		_, existed := b.Spec.XRD.Parameters[req.Name]
		if err := b.AddParameter(req.Name, req.Parameter); err != nil {
			status := http.StatusBadRequest
			if existed {
				status = http.StatusConflict
			}
			return status, err
		}
		return http.StatusOK, nil
	})
}

// setParameterRequest is the PUT /api/blueprint/parameters/{name} body. The
// parameter is held as raw JSON rather than decoded straight into a
// blueprint.Parameter, so the handler can still tell which keys the caller
// actually sent — see decodeParameter.
type setParameterRequest struct {
	Parameter json.RawMessage `json:"parameter"`
}

// parameterKeys is every key of a parameter object, in the order
// blueprint.Parameter declares them, each paired with "does the current
// declaration hold a value here". Order is fixed so the error below names
// omitted keys deterministically.
//
// This list has to be kept in step with blueprint.Parameter's fields. It
// cannot be derived by reflection over the struct without also hard-coding
// what "unset" means per field, which is the only part that carries any
// judgement — so it is written out plainly instead.
var parameterKeys = []struct {
	name string
	set  func(blueprint.Parameter) bool
}{
	{"type", func(p blueprint.Parameter) bool { return p.Type != "" }},
	{"required", func(p blueprint.Parameter) bool { return p.Required }},
	{"enum", func(p blueprint.Parameter) bool { return len(p.Enum) > 0 }},
	{"default", func(p blueprint.Parameter) bool { return p.Default != "" }},
	{"description", func(p blueprint.Parameter) bool { return p.Description != "" }},
	{"properties", func(p blueprint.Parameter) bool { return len(p.Properties) > 0 }},
}

// handleSetParameter serves PUT /api/blueprint/parameters/{name}: replace an
// existing parameter's declaration in full (SetParameter is replace-only,
// not a merge/patch). Unknown name -> 404, matching this API's general
// unknown-name convention.
func (srv *server) handleSetParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req setParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	param, present, err := decodeParameter(req.Parameter)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		existing, existed := b.Spec.XRD.Parameters[name]
		if dropped := silentlyDropped(existing, present); existed && len(dropped) > 0 {
			return http.StatusBadRequest, fmt.Errorf(
				"refusing a partial update of parameter %q: PUT replaces the whole parameter, so omitting "+
					"%s would silently discard the value each of them currently holds. Send those keys "+
					"explicitly — their zero values (false, null, \"\") are how you clear one.",
				name, strings.Join(dropped, ", "))
		}

		if err := b.SetParameter(name, param); err != nil {
			status := http.StatusBadRequest
			if !existed {
				status = http.StatusNotFound
			}
			return status, err
		}
		return http.StatusOK, nil
	})
}

// renameParameterRequest is the POST /api/blueprint/parameters/{name}/rename
// body.
type renameParameterRequest struct {
	To string `json:"to"`
}

// handleRenameParameter serves POST /api/blueprint/parameters/{name}/rename:
// rename a parameter and rewrite every resource field that references it.
func (srv *server) handleRenameParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req renameParameterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		_, fromExists := b.Spec.XRD.Parameters[name]
		_, toCollides := b.Spec.XRD.Parameters[req.To]

		if err := b.RenameParameter(name, req.To); err != nil {
			switch {
			case !fromExists:
				return http.StatusNotFound, err
			case toCollides:
				return http.StatusConflict, err
			default:
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}

// handleDeleteParameter serves DELETE /api/blueprint/parameters/{name}.
func (srv *server) handleDeleteParameter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		_, existed := b.Spec.XRD.Parameters[name]
		refs := b.ReferencingResources(name)

		if err := b.DeleteParameter(name); err != nil {
			switch {
			case !existed:
				return http.StatusNotFound, err
			case len(refs) > 0:
				return http.StatusConflict, err
			default:
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}

// renameResourceRequest is the POST /api/blueprint/resources/{name}/rename
// body.
type renameResourceRequest struct {
	To string `json:"to"`
}

// handleRenameResource serves POST /api/blueprint/resources/{name}/rename:
// rename a composed resource and rewrite every cross-resource status
// reference (resources.<name>.status.<path>) that points at it.
func (srv *server) handleRenameResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req renameResourceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		fromExists := b.ResourceNamed(name) != nil
		toCollides := b.ResourceNamed(req.To) != nil

		if err := b.RenameResource(name, req.To); err != nil {
			switch {
			case !fromExists:
				return http.StatusNotFound, err
			case toCollides:
				return http.StatusConflict, err
			default:
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}

// handleDeleteResource serves DELETE /api/blueprint/resources/{name}.
func (srv *server) handleDeleteResource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		existed := b.ResourceNamed(name) != nil
		refs := b.StatusReferencingResources(name)

		if err := b.DeleteResource(name); err != nil {
			switch {
			case !existed:
				return http.StatusNotFound, err
			case len(refs) > 0:
				return http.StatusConflict, err
			default:
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}

// syncBlueprintSourcesLocked ensures all sources declared in b.Spec.Sources
// are present in srv.Store and indexed in srv.Index.
func (srv *server) syncBlueprintSourcesLocked(ctx context.Context, b *blueprint.Blueprint) error {
	if srv.Store == nil || b == nil {
		return nil
	}
	existing := make(map[string]bool, len(srv.Providers))
	for _, p := range srv.Providers {
		existing[p] = true
	}
	var newProviders []string
	for _, s := range b.Spec.Sources {
		if s.Provider != "" && s.Provider != blueprint.NativeProvider && !existing[s.Provider] {
			newProviders = append(newProviders, s.Provider)
			existing[s.Provider] = true
		}
	}
	fetch := srv.fetch
	if fetch == nil {
		fetch = func(ref string) (*xpkg.Package, error) {
			return xpkg.Fetch(ctx, ref)
		}
	}

	origProviders := append([]string(nil), srv.Providers...)
	for _, ref := range newProviders {
		// If already in store cache, ensure it is pinned in lockfile and record in srv.Providers
		if _, err := srv.Store.Load(ref); err == nil {
			if srv.Lock != "" {
				digest, err := srv.Store.LoadDigest(ref)
				if err != nil {
					srv.Providers = origProviders
					return fmt.Errorf("load digest %s: %w", ref, err)
				}
				l, err := cache.ReadLock(srv.Lock)
				if err != nil {
					srv.Providers = origProviders
					return fmt.Errorf("read lock: %w", err)
				}
				l.Set(ref, digest)
				if err := l.Write(srv.Lock); err != nil {
					srv.Providers = origProviders
					return fmt.Errorf("write lock: %w", err)
				}
			}
			srv.Providers = append(srv.Providers, ref)
			continue
		}
		// Otherwise fetch from remote registry
		pkg, err := fetch(ref)
		if err != nil {
			srv.Providers = origProviders
			return fmt.Errorf("fetch %s: %w", ref, err)
		}
		crds, err := schema.ParseCRDs(pkg.Docs)
		if err != nil {
			srv.Providers = origProviders
			return fmt.Errorf("parse %s: %w", ref, err)
		}
		if srv.Lock != "" {
			l, err := cache.ReadLock(srv.Lock)
			if err != nil {
				srv.Providers = origProviders
				return fmt.Errorf("read lock: %w", err)
			}
			l.Set(ref, pkg.Digest)
			if err := l.Write(srv.Lock); err != nil {
				srv.Providers = origProviders
				return fmt.Errorf("write lock: %w", err)
			}
		}
		if err := srv.Store.Save(pkg, crds); err != nil {
			srv.Providers = origProviders
			return fmt.Errorf("save %s: %w", ref, err)
		}
		srv.Providers = append(srv.Providers, ref)
	}

	if err := srv.rebuildIndexLocked(b); err != nil {
		srv.Providers = origProviders
		return err
	}
	return nil
}

// persistBlueprint writes b to srv.Blueprint, deterministically and only if
// the result would itself load back. On failure it writes the 500 response
// itself and returns false, so callers can just `if !srv.persistBlueprint(w,
// r, b) { return }`; a failure here means the write was refused, not attempted
// half-done, so the file on disk is left exactly as it was before the call.
func (srv *server) persistBlueprint(w http.ResponseWriter, r *http.Request, b *blueprint.Blueprint) bool {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	if err := srv.syncBlueprintSourcesLocked(ctx, b); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to sync sources: %v", err))
		return false
	}
	if err := writeBlueprintFile(srv.Blueprint, b); err != nil {
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
// brief's explicit direction.
//
// What that costs, stated plainly (fix round 2 — an earlier version of this
// comment claimed the opposite, that "every key the user might have added or
// reordered by hand round-trips", which is simply false): THE FILE IS NOT
// PRESERVED. Every edit through this API rewrites the whole document from
// the Go structs, so on the first edit anyone makes:
//
//   - every comment is gone, including a file header;
//   - blank lines and hand-chosen key order are gone (keys come back
//     sorted), as is any block-vs-flow style choice;
//   - any key the blueprint.* structs do not model is dropped silently,
//     not rejected;
//   - every modeled key is written explicitly, set or not (see below).
//
// "Hand-editable" holds only in the direction that matters for `cf gen`: a
// human can write this file and both the CLI and this server will read it.
// It does not hold in reverse — a hand-written file that has been edited
// through the API comes back canonically reformatted, carrying only what
// the Go types model. Preserving comments and unmodeled keys would mean
// editing a yaml.Node tree in place instead of marshaling structs; that is
// a different design from the one the brief directs, and it is deliberately
// out of scope here rather than half-attempted.
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

// decodeParameter decodes a raw `parameter` object into a blueprint.Parameter
// and, separately, reports which keys the body actually carried.
//
// The key set cannot be recovered from the decoded struct — that is the whole
// problem — and pointer fields would not recover it either: encoding/json
// unmarshals an explicit `null` into a pointer field by setting the pointer
// to nil, making `"enum": null` (a deliberate clear) indistinguishable from
// no `enum` key at all (an accident), which is exactly the distinction
// handleSetParameter needs. Decoding twice — once into the struct for the
// values, once into a map for the keys — keeps both.
//
// The struct decode keeps DisallowUnknownFields, so a typo inside the
// parameter object ("requird") is still a 400 rather than a silently ignored
// key; holding the parameter as json.RawMessage in setParameterRequest would
// otherwise have lost that check for everything below the top level.
func decodeParameter(raw json.RawMessage) (blueprint.Parameter, map[string]bool, error) {
	var p blueprint.Parameter
	present := make(map[string]bool, len(parameterKeys))
	if len(raw) == 0 { // no "parameter" key at all
		return p, present, nil
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, nil, fmt.Errorf("invalid request body: %w", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return p, nil, fmt.Errorf("invalid request body: %w", err)
	}
	for k := range keys {
		present[k] = true
	}
	return p, present, nil
}

// silentlyDropped returns the keys that are absent from a PUT body but
// currently hold a value on existing — the ones a whole-parameter replace
// would discard without the caller having asked for it — in
// parameterKeys order.
func silentlyDropped(existing blueprint.Parameter, present map[string]bool) []string {
	var dropped []string
	for _, k := range parameterKeys {
		if !present[k.name] && k.set(existing) {
			dropped = append(dropped, k.name)
		}
	}
	return dropped
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
