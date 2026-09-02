package api

import (
	"fmt"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/examples"
)

// handleExamples serves GET /api/examples: lists all curated starter blueprints.
func (srv *server) handleExamples(w http.ResponseWriter, r *http.Request) {
	list := examples.List()
	writeJSON(w, http.StatusOK, map[string]any{
		"examples": list,
	})
}

// handleExample serves GET /api/examples/{id}: retrieves a single starter blueprint by ID.
func (srv *server) handleExample(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ex, err := examples.Get(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"example": ex,
	})
}

// handleLoadExample serves POST /api/examples/{id}/load:
// Retrieves the curated starter blueprint, imports and caches all required providers,
// indexes them, persists the blueprint to disk, and returns the loaded document.
func (srv *server) handleLoadExample(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ex, err := examples.Get(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	b, err := blueprint.Parse([]byte(ex.YAML))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	// Ensure all required providers in the example are fetched, cached, and indexed
	if err := srv.syncBlueprintSourcesLocked(r.Context(), b); err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("failed to cache provider: %v", err))
		return
	}

	if !srv.persistBlueprint(w, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}
