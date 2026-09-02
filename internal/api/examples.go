package api

import (
	"net/http"

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
