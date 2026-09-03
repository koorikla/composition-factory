package api

import (
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
)

// previewExpressionRequest is POST /api/preview-expression's request body.
type previewExpressionRequest struct {
	Expression string               `json:"expression"`
	Resource   string               `json:"resource,omitempty"`
	Blueprint  *blueprint.Blueprint `json:"blueprint,omitempty"`
}

// previewExpressionResponse is POST /api/preview-expression's JSON response body.
type previewExpressionResponse struct {
	Rendered string `json:"rendered"`
}

// handlePreviewExpression executes a single Go template expression against the
// synthetic context of the current blueprint (or an inline blueprint in the body).
func (srv *server) handlePreviewExpression(w http.ResponseWriter, r *http.Request) {
	var req previewExpressionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	b := req.Blueprint
	if b == nil {
		srv.mu.Lock()
		loaded, ok := srv.loadBlueprint(w)
		srv.mu.Unlock()
		if !ok {
			return
		}
		b = loaded
	}

	if req.Resource != "" && b.ResourceNamed(req.Resource) == nil {
		writeJSONError(w, http.StatusBadRequest, "resource \""+req.Resource+"\" is not declared in blueprint")
		return
	}

	rendered, err := emit.PreviewExpressionContext(r.Context(), b, req.Resource, req.Expression)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, previewExpressionResponse{
		Rendered: rendered,
	})
}
