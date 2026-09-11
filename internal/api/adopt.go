package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/koorikla/compositionfactory/internal/adopt"
	"github.com/koorikla/compositionfactory/internal/blueprint"
)

type adoptRequest struct {
	Manifest      string               `json:"manifest"`
	Persist       bool                 `json:"persist"`
	Provider      string               `json:"provider"`
	BaseBlueprint *blueprint.Blueprint `json:"baseBlueprint,omitempty"`
}

type adoptResponse struct {
	Blueprint  *blueprint.Blueprint `json:"blueprint"`
	LossReport *adopt.LossReport    `json:"lossReport,omitempty"`
	Persisted  bool                 `json:"persisted"`
}

func (srv *server) handleAdoptBlueprint(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read request body: "+err.Error())
		return
	}

	var req adoptRequest
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
			return
		}
	} else {
		// Allow raw YAML manifest in request body
		req.Manifest = string(body)
	}

	if req.Manifest == "" {
		writeJSONError(w, http.StatusBadRequest, "manifest cannot be empty")
		return
	}

	opts := adopt.Options{
		DefaultProviderRef: req.Provider,
		BaseBlueprint:      req.BaseBlueprint,
	}
	if srv.Store != nil {
		opts.Store = srv.Store
		opts.CacheDir = srv.Store.Root
	}
	if opts.BaseBlueprint == nil && srv.Blueprint != "" {
		if data, err := os.ReadFile(srv.Blueprint); err == nil {
			if existingBP, err := blueprint.Parse(data); err == nil {
				opts.BaseBlueprint = existingBP
			}
		}
	}
	bp, report, err := adopt.Adopt([]byte(req.Manifest), opts)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "adopt failed: "+err.Error())
		return
	}

	persisted := false
	if req.Persist && srv.Blueprint != "" {
		srv.mu.Lock()
		if !srv.persistBlueprint(w, r, bp) {
			srv.mu.Unlock()
			return
		}
		srv.mu.Unlock()
		persisted = true
	}

	writeJSON(w, http.StatusOK, adoptResponse{
		Blueprint:  bp,
		LossReport: report,
		Persisted:  persisted,
	})
}
