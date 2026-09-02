// This file implements the /api/generate route, and nothing else. That
// restriction is the load-bearing architectural rule of the whole project
// (see the Task 6 brief): the CLI (cmd/cf's GenCmd), this HTTP server and
// the future MCP server must all produce byte-identical output for the same
// blueprint, which only holds if they share one rendering path. This
// handler therefore loads the blueprint and its provider schemas — the same
// two inputs cmd/cf/gen.go's run loads them from — and calls
// emit.Generate(b, crds, srv.OutDir). Nothing in this file computes, renders
// or otherwise touches a single byte of an XRD, Composition or functions.yaml;
// any code here that did would be a defect, not an optimisation.
package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/cluster"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// generateRequest is the POST /api/generate body.
type generateRequest struct {
	Write bool `json:"write"`
}

// generateOutput is one rendered file, summarized for the JSON response:
// path, size, and the full rendered content. Body is the file's exact bytes
// as a UTF-8 string (the engine only ever emits YAML, so this never needs
// base64) — the canvas renders it directly in the output pane. It is
// included on both write modes: write:false is a preview that must show the
// content without touching disk, and write:true includes it too since the
// canvas still wants to render what it just wrote, and the total payload
// for three small YAML files is a few KB — the gzip middleware already
// handles the size. Bytes remains a separate field (not derived from Body
// by every caller) for backward compatibility with anything that only reads
// the summary.
type generateOutput struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Body  string `json:"body"`
}

// handleGenerate serves POST /api/generate: {"write":bool} ->
// {"outputs":[{"path":...,"bytes":N,"body":"..."}],"written":bool}.
//
// write:false reports what emit.Generate would produce without touching
// disk — a dry-run preview for the canvas, body included so the canvas can
// render the output pane straight from the preview. write:true additionally
// writes every output through the exact same os.MkdirAll+os.WriteFile
// sequence cmd/cf/gen.go's run uses for a non-check `cf gen`, so a
// generation triggered from the canvas leaves the output tree in the
// identical state a CLI run would have; its response carries the same
// bodies as write:false, since a write does not change what was rendered.
//
// Every failure here — a blueprint that no longer validates, a provider not
// yet in the cache, a field that does not exist on its resolved CRD — is
// surfaced as 400 with emit.Generate's (or blueprint.Load's) own error text
// verbatim; see TestGenerateSurfacesValidationErrorsAsIs. None of these are
// the caller's request body being malformed — they are the current
// blueprint/cache state failing to produce valid output — but they are
// reported the same way malformed-request errors are (400), matching this
// task's own given test rather than introducing a finer-grained code this
// task's tests do not ask for. The one exception is srv.loadBlueprint's own
// 500 case (Fix round 1, Finding 1): if srv.Blueprint itself cannot be read at
// all, that is the server's fixed path being wrong, not the blueprint's
// content, and is reported as 500 there — see blueprint.go's loadBlueprint.
func (srv *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Held for the whole handler, not just the write half. Two concurrent
	// write:true generations render from whatever the blueprint said when
	// each of them loaded it and then write the same output paths with
	// os.WriteFile, which is not atomic — interleaved, they can leave a file
	// that is neither run's output. Serializing with the blueprint's own
	// mutating handlers additionally means a generation renders a document
	// that actually existed as a whole, rather than one an edit was midway
	// through replacing. See server.mu.
	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}

	crds, err := srv.loadSourceCRDs(b)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	outputs, err := emit.Generate(b, crds, srv.OutDir)
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
		summaries[i] = generateOutput{Path: out.Path, Bytes: len(out.Body), Body: string(out.Body)}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"outputs": summaries,
		"written": req.Write,
	})
}

// loadSourceCRDs loads every provider schema b.Spec.Sources names, from
// srv.Store — the identical loop cmd/cf/gen.go's run uses, so this route reads
// the provider cache exactly the way `cf gen` does rather than inventing its
// own lookup order or error handling for it — then appends the vendored
// native Kubernetes kinds, exactly the way `cf gen` does. Native kinds name
// no source (blueprint.Validate refuses a source called "k8s") and live in
// no cache: they are compiled into the binary and always available.
func (srv *server) loadSourceCRDs(b *blueprint.Blueprint) ([]schema.CRD, error) {
	crds, err := cache.LoadSources(srv.Store, b, filepath.Dir(srv.Blueprint))
	if err != nil {
		return nil, err
	}
	native, err := k8s.Kinds()
	if err != nil {
		return nil, err
	}
	all := append(crds, native...)
	for _, p := range srv.Providers {
		if p == cluster.ProviderLabel {
			if clusterCRDs, err := srv.Store.Load(cluster.ProviderLabel); err == nil {
				all = append(all, clusterCRDs...)
			}
			break
		}
	}
	return all, nil
}
