package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

type versionResponse struct {
	Version   string   `json:"version"`
	Engines   []string `json:"engines"`
	OutDir    string   `json:"outDir"`
	Blueprint string   `json:"blueprint,omitempty"`
	Container bool     `json:"container,omitempty"`
}

func (srv *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v := srv.Version
	if v == "" {
		v = "dev"
	}
	bp := filepath.Clean(srv.Blueprint)
	if filepath.IsAbs(bp) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, bp); err == nil && !strings.HasPrefix(rel, "..") {
				bp = rel
			}
		} else if rel, err := filepath.Rel(".", bp); err == nil && !strings.HasPrefix(rel, "..") {
			bp = rel
		}
	}
	bp = filepath.ToSlash(bp)

	writeJSON(w, http.StatusOK, versionResponse{
		Version:   v,
		Engines:   blueprint.SupportedEngines,
		OutDir:    srv.OutDir,
		Blueprint: bp,
		Container: srv.isContainerEnv(),
	})
}
