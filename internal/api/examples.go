package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/examples"
)

// listedExample is an examples.Example plus the one thing only the server can
// know: whether loading it would need a provider download. Loading an example
// syncs its sources (see handleExample), so an example whose providers are not
// cached costs a network fetch and fails outright offline -- which is exactly
// the situation a first run in a fresh container is in. The chooser opens
// itself on a blank document, so it has to say which cards are ready now.
type listedExample struct {
	examples.Example
	SourcesReady   bool     `json:"sourcesReady"`
	MissingSources []string `json:"missingSources,omitempty"`
}

// handleExamples serves GET /api/examples: lists all curated starter
// blueprints, each marked with whether its provider schemas are already
// cached, and ordered so the ones that load instantly come first. Order is
// otherwise the canonical one, so the list stays stable as the cache fills.
func (srv *server) handleExamples(w http.ResponseWriter, r *http.Request) {
	src := examples.List()
	list := make([]listedExample, 0, len(src))
	for _, ex := range src {
		le := listedExample{Example: ex, SourcesReady: true}
		for _, ref := range ex.Sources {
			if srv.Store == nil {
				le.SourcesReady = false
				le.MissingSources = append(le.MissingSources, ref)
				continue
			}
			if _, err := srv.Store.Load(ref); err != nil {
				le.SourcesReady = false
				le.MissingSources = append(le.MissingSources, ref)
			}
		}
		list = append(list, le)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].SourcesReady && !list[j].SourcesReady
	})
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
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to cache provider: %v", err))
		return
	}

	if !srv.persistBlueprint(w, r, b) {
		return
	}
	writeJSON(w, http.StatusOK, b)
}
