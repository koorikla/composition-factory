package api

import (
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

type versionResponse struct {
	Version string   `json:"version"`
	Engines []string `json:"engines"`
}

func (srv *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v := srv.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, versionResponse{
		Version: v,
		Engines: blueprint.SupportedEngines,
	})
}
