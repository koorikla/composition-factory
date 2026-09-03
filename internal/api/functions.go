package api

import (
	"fmt"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

type addFunctionRequest struct {
	Ref string `json:"ref"`
}

type functionEntry struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
	Inputs int    `json:"inputs"`
}

func (srv *server) handleListFunctions(w http.ResponseWriter, _ *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	lock, err := cache.ReadLock(srv.Lock)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var entries []functionEntry
	for _, f := range lock.Functions {
		crds, _ := srv.Store.Load(f.Ref)
		inputs := 0
		for _, crd := range crds {
			if crd.IsFunctionInput() || crd.Function {
				inputs++
			}
		}
		entries = append(entries, functionEntry{
			Ref:    f.Ref,
			Digest: f.Digest,
			Inputs: inputs,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"functions": entries})
}

func (srv *server) handleAddFunction(w http.ResponseWriter, r *http.Request) {
	var req addFunctionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Ref == "" {
		writeJSONError(w, http.StatusBadRequest, "ref is required")
		return
	}
	if err := xpkg.ValidateRef(req.Ref); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	fetch := srv.fetch
	if fetch == nil {
		fetch = func(ref string) (*xpkg.Package, error) {
			return xpkg.Fetch(r.Context(), ref)
		}
	}
	pkg, crds, err := srv.Store.FetchAndSave(r.Context(), srv.Lock, req.Ref, fetch)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	inputs := 0
	managed := 0
	for _, crd := range crds {
		if crd.IsFunctionInput() || crd.Function {
			inputs++
		}
		if crd.IsManaged() {
			managed++
		}
	}
	if inputs == 0 && managed > 0 {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("package %q is a provider package, not a function (use 'cf provider add %s')", req.Ref, req.Ref))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"function": functionEntry{
			Ref:    req.Ref,
			Digest: pkg.Digest,
			Inputs: inputs,
		},
	})
}
