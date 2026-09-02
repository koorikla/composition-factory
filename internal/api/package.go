package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// handlePackage serves the current blueprint as a built Configuration
// package — the same bytes `cf package` writes, streamed as a download so
// the canvas gets a one-click .xpkg. Held under srv.mu like generate: the
// package must render a document that existed as a whole.
func (srv *server) handlePackage(w http.ResponseWriter, r *http.Request) {
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
	outputs, err := emit.Generate(b, crds, "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	var docs [][]byte
	for _, prefix := range []string{"xrds/", "compositions/"} {
		for _, o := range outputs {
			if strings.HasPrefix(o.Path, prefix) {
				docs = append(docs, o.Body)
			}
		}
	}
	// the embedded source is the canonical marshal of the live document —
	// the server has no original file bytes to preserve
	source, err := yaml.Marshal(b)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, err := emit.ConfigurationMeta(b, source)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// ?format=yaml: the package.yaml stream itself — same bytes the .xpkg
	// carries, importable back through POST /api/blueprint/import
	if r.URL.Query().Get("format") == "yaml" {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", b.Metadata.Name+".package.yaml"))
		w.WriteHeader(http.StatusOK)
		w.Write(xpkg.Stream(meta, docs))
		return
	}

	img, err := xpkg.Build(meta, docs)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// build fully into memory first: an error after headers would corrupt
	// the download instead of reporting
	var buf bytes.Buffer
	if err := xpkg.WriteTarballTo(&buf, img, b.Metadata.Name); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", b.Metadata.Name+".xpkg"))
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}
