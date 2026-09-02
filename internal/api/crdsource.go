package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// crdSourceNameRE keeps the manifest's file name a plain slug: the name is
// joined into a path under the blueprint's directory, so anything fancier
// is either an escape attempt or a typo.
var crdSourceNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

type addCRDSourceRequest struct {
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

// handleAddCRDSource ingests a scanned CRD manifest — "compose any object":
// an Argo Workflow, another composition's XR, whatever kind a CRD defines.
// The manifest is validated (ParseCRDManifest, the object-rooted door),
// written to <blueprint dir>/crds/<name>.yaml, declared in spec.sources as
// a crds: entry, and indexed under that path so the palette groups the new
// kinds by file.
func (srv *server) handleAddCRDSource(w http.ResponseWriter, r *http.Request) {
	var req addCRDSourceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSuffix(strings.TrimSuffix(req.Name, ".yaml"), ".yml")
	if !crdSourceNameRE.MatchString(name) {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("name %q must be a lowercase slug (letters, digits, hyphens)", req.Name))
		return
	}
	scanned, err := schema.ParseCRDManifest([]byte(req.YAML))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest,
			"not a CRD manifest: "+err.Error()+" (expected one or more CustomResourceDefinition documents)")
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	rel := filepath.Join("crds", name+".yaml")
	dir := filepath.Dir(srv.Blueprint)
	if err := os.MkdirAll(filepath.Join(dir, "crds"), 0o755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(req.YAML), 0o644); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	declared := false
	for _, s := range b.Spec.Sources {
		if s.CRDs == rel {
			declared = true
			break
		}
	}
	if !declared {
		b.Spec.Sources = append(b.Spec.Sources, blueprint.Source{CRDs: rel})
		if !srv.persistBlueprint(w, r, b) {
			return
		}
	}

	if err := srv.rebuildIndexLocked(b); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	kinds := make([]string, 0, len(scanned))
	for _, c := range scanned {
		kinds = append(kinds, c.Kind)
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": rel, "kinds": kinds})
}
