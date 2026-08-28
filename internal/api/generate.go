// This file implements the /api/generate route, and nothing else. That
// restriction is the load-bearing architectural rule of the whole project
// (see the Task 6 brief): the CLI (cmd/cf's GenCmd), this HTTP server and
// the future MCP server must all produce byte-identical output for the same
// blueprint, which only holds if they share one rendering path. This
// handler therefore loads the blueprint and its provider schemas — the same
// two inputs cmd/cf/gen.go's run loads them from — and calls
// emit.Generate(b, crds, o.OutDir). Nothing in this file computes, renders
// or otherwise touches a single byte of an XRD, Composition or functions.yaml;
// any code here that did would be a defect, not an optimisation.
package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// generateRequest is the POST /api/generate body.
type generateRequest struct {
	Write bool `json:"write"`
}

// generateOutput is one rendered file, summarized for the JSON response —
// path and size, not the body: the canvas asks for a preview or triggers a
// write, it does not need megabytes of generated YAML echoed back to it.
type generateOutput struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

// handleGenerate serves POST /api/generate: {"write":bool} ->
// {"outputs":[{"path":...,"bytes":N}],"written":bool}.
//
// write:false reports what emit.Generate would produce without touching
// disk — a dry-run preview for the canvas. write:true additionally writes
// every output through the exact same os.MkdirAll+os.WriteFile sequence
// cmd/cf/gen.go's run uses for a non-check `cf gen`, so a generation
// triggered from the canvas leaves the output tree in the identical state a
// CLI run would have.
//
// Every failure here — a blueprint that no longer validates, a provider not
// yet in the cache, a field that does not exist on its resolved CRD — is
// surfaced as 400 with emit.Generate's (or blueprint.Load's) own error text
// verbatim; see TestGenerateSurfacesValidationErrorsAsIs. None of these are
// the caller's request body being malformed — they are the current
// blueprint/cache state failing to produce valid output — but they are
// reported the same way malformed-request errors are (400), matching this
// task's own given test rather than introducing a finer-grained code this
// task's tests do not ask for.
func (o Options) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	b, ok := o.loadBlueprint(w)
	if !ok {
		return
	}

	crds, err := o.loadSourceCRDs(b)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	outputs, err := emit.Generate(b, crds, o.OutDir)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Write {
		for _, out := range outputs {
			if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := os.WriteFile(out.Path, out.Body, 0o644); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	summaries := make([]generateOutput, len(outputs))
	for i, out := range outputs {
		summaries[i] = generateOutput{Path: out.Path, Bytes: len(out.Body)}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"outputs": summaries,
		"written": req.Write,
	})
}

// loadSourceCRDs loads every provider schema b.Spec.Sources names, from
// o.Store — the identical loop cmd/cf/gen.go's run uses, so this route reads
// the provider cache exactly the way `cf gen` does rather than inventing its
// own lookup order or error handling for it.
func (o Options) loadSourceCRDs(b *blueprint.Blueprint) ([]schema.CRD, error) {
	var crds []schema.CRD
	for _, s := range b.Spec.Sources {
		got, err := o.Store.Load(s.Provider)
		if err != nil {
			return nil, err
		}
		crds = append(crds, got...)
	}
	return crds, nil
}
