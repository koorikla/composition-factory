// This file implements the /api/render route: a real `crossplane
// composition render` of the current blueprint against a synthesized sample
// XR, reporting whether the generated artifacts actually render.
//
// The single render pipeline lives in internal/emit.RenderCheck, shared between
// this HTTP handler and the CLI (`cf gen --validate`). This handler loads the
// blueprint and source CRDs under srv.mu, then delegates execution to
// emit.RenderCheck unlocked so a render never blocks concurrent edits.
package api

import (
	"errors"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// renderResponse is POST /api/render's body.
type renderResponse = emit.RenderResult

// handleRender serves POST /api/render.
func (srv *server) handleRender(w http.ResponseWriter, r *http.Request) {
	b, crds, ok := srv.renderInputs(w)
	if !ok {
		return
	}

	opts := emit.RenderOptions{
		LookPath: srv.lookPath,
		Runner:   srv.render,
	}
	res, err := emit.RenderCheck(r.Context(), b, crds, opts)
	if err != nil {
		var genErr *emit.GenerateError
		if errors.As(err, &genErr) {
			writeJSONError(w, http.StatusBadRequest, genErr.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	res.Container = srv.isContainerEnv()
	writeJSON(w, http.StatusOK, res)
}

// renderInputs loads and validates the blueprint and its source CRDs under srv.mu.
func (srv *server) renderInputs(w http.ResponseWriter) (*blueprint.Blueprint, []schema.CRD, bool) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return nil, nil, false
	}
	crds, err := srv.loadSourceCRDs(b)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	return b, crds, true
}

// sampleXR delegates synthesis to emit.SampleXR for test assertions.
func sampleXR(b *blueprint.Blueprint) ([]byte, error) {
	return emit.SampleXR(b)
}
