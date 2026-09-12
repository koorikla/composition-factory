// This file implements the manifest routes: one composed resource's flat
// field map viewed and edited as nested, manifest-shaped YAML.
//
//   - GET /api/blueprint/resources/{name}/manifest -> {yaml}
//   - PUT /api/blueprint/resources/{name}/manifest <- {yaml}
//
// The conversion lives in internal/manifest; the kind's schema tree (what
// emit resolves the resource against) is the grammar authority for map
// keys, list indices and object members, so the text a user edits is the
// shape the generator will emit. PUT replaces the resource's fields IN
// FULL, inside srv.mutate like every other edit, and then runs the same CRD
// validation PUT /api/blueprint/resources/{name} does. A manifest-level
// rejection (unknown key, scalar at an object, a bad wrapper) answers 400
// with "path" and "line" beside "error" so the editor can highlight it.
// Both keys are always present on such a body, but "path" may be "" and
// "line" may be 0 when the error has no location (a syntax error yaml.v3
// could not place, a conflict between two field paths on GET); a UI treats
// those as "no location" rather than as the first line or the root.
package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/manifest"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// manifestResponse is GET /api/blueprint/resources/{name}/manifest's body.
type manifestResponse struct {
	YAML string `json:"yaml"`
}

// manifestRequest is PUT /api/blueprint/resources/{name}/manifest's body.
type manifestRequest struct {
	YAML string `json:"yaml"`
}

// manifestErr adapts manifest.Error to writeMutateError's detailedError
// contract. It holds the error in a named field on purpose: embedding a type
// called Error would give this struct a FIELD named Error that shadows the
// promoted Error() method, and it would no longer satisfy error at all.
type manifestErr struct{ err *manifest.Error }

func (m manifestErr) Error() string { return m.err.Error() }
func (m manifestErr) Unwrap() error { return m.err }

func (m manifestErr) Body() map[string]any {
	return map[string]any{"path": m.err.Path, "line": m.err.Line}
}

// fieldTreeFor resolves r's kind against the blueprint's sources exactly the
// way the emitter does and returns the settable field tree with the CRD it
// came from (the parser takes the kind's rooting from the CRD, not from the
// tree's shape). Caller must hold srv.mu (loadSourceCRDs requires it).
func (srv *server) fieldTreeFor(b *blueprint.Blueprint, r blueprint.Resource) ([]*schema.Node, schema.CRD, error) {
	crds, err := srv.loadSourceCRDs(b)
	if err != nil {
		return nil, schema.CRD{}, err
	}
	crd, err := emit.ResolveKind(crds, r, b.Spec.XRD.Scope == "Namespaced")
	if err != nil {
		return nil, schema.CRD{}, err
	}
	nodes, err := crd.FieldTree()
	if err != nil {
		return nil, schema.CRD{}, err
	}
	return nodes, crd, nil
}

// parseOptionsFor is the parser's view of the resolved CRD: object-rooted
// for a native or crds:-sourced kind and a function input (their tree is
// the object's own top level), forProvider-rooted for a managed resource.
func parseOptionsFor(crd schema.CRD) manifest.Options {
	return manifest.Options{ObjectRooted: crd.Native || crd.IsFunctionInput()}
}

// handleGetResourceManifest serves GET /api/blueprint/resources/{name}/manifest.
// Like handleRender it first waits for any background fetch of the
// blueprint's sources, so a cold-start GET resolves the kind instead of
// answering 400 while a provider is still loading. The lock is then held for
// the same reason handleGenerate holds it: the view must describe a
// document that existed as a whole, not one an edit was midway through
// replacing.
func (srv *server) handleGetResourceManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if cur, err := blueprint.Load(srv.Blueprint); err == nil && cur != nil {
		srv.awaitBlueprintSources(cur)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	res := b.ResourceNamed(name)
	if res == nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("resource %q is not declared", name))
		return
	}
	nodes, _, err := srv.fieldTreeFor(b, *res)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	y, err := manifest.Render(nodes, res.Fields)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, manifestResponse{YAML: y})
}

// handleSetResourceManifest serves PUT /api/blueprint/resources/{name}/manifest:
// replace the resource's fields in full from manifest-shaped YAML, validated
// against the kind's schema (unknown keys fail with path and line) and then
// through the same CRD validation PUT /api/blueprint/resources/{name} runs.
func (srv *server) handleSetResourceManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req manifestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		res := b.ResourceNamed(name)
		if res == nil {
			return http.StatusNotFound, fmt.Errorf("resource %q is not declared", name)
		}
		nodes, crd, err := srv.fieldTreeFor(b, *res)
		if err != nil {
			return http.StatusBadRequest, err
		}
		fields, err := manifest.ParseWith(nodes, req.YAML, parseOptionsFor(crd))
		if err != nil {
			var me *manifest.Error
			if errors.As(err, &me) {
				return http.StatusBadRequest, manifestErr{me}
			}
			return http.StatusBadRequest, err
		}
		next := *res
		next.Fields = fields
		if err := b.SetResource(name, next); err != nil {
			return http.StatusBadRequest, err
		}
		if crds, err := srv.loadSourceCRDs(b); err == nil {
			if err := srv.validateBlueprintAgainstCRDs(b, crds); err != nil {
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}
