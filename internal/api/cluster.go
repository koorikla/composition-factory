package api

import (
	"encoding/json"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cluster"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// handleGetCluster serves GET /api/cluster: returns current cluster connection info.
func (srv *server) handleGetCluster(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	cl := srv.ClusterClient
	srv.mu.Unlock()

	if cl == nil {
		writeJSON(w, http.StatusOK, cluster.ClusterInfo{
			Connected: false,
			Error:     "no cluster configured",
		})
		return
	}

	info := cl.Info(r.Context())
	writeJSON(w, http.StatusOK, info)
}

// connectClusterRequest is the JSON body for POST /api/cluster/connect.
type connectClusterRequest struct {
	Kubeconfig string `json:"kubeconfig,omitempty"` // raw kubeconfig YAML or path
	Context    string `json:"context,omitempty"`
}

// handleConnectCluster serves POST /api/cluster/connect.
func (srv *server) handleConnectCluster(w http.ResponseWriter, r *http.Request) {
	var req connectClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	var cl *cluster.Client
	var err error
	if len(req.Kubeconfig) > 0 && req.Kubeconfig[0] == 'a' || req.Kubeconfig != "" && (req.Kubeconfig[0] == '{' || req.Kubeconfig[0] == 'a' || req.Kubeconfig[0] == 'k') {
		// Could be YAML string
		cl, err = cluster.FromKubeconfig([]byte(req.Kubeconfig), req.Context)
	}
	if cl == nil {
		cl, err = cluster.NewClient(req.Kubeconfig, req.Context)
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "failed to connect: "+err.Error())
		return
	}

	srv.mu.Lock()
	srv.ClusterClient = cl
	srv.mu.Unlock()

	srv.handleSyncCluster(w, r)
}

// handleSyncCluster serves POST /api/cluster/sync: fetches CRDs from the cluster,
// updates the store, rebuilds the in-memory index, and returns the updated status.
func (srv *server) handleSyncCluster(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	cl := srv.ClusterClient
	if cl == nil {
		writeJSONError(w, http.StatusBadRequest, "no cluster configured to sync from")
		return
	}

	crds, err := cl.FetchCRDs(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "cluster discovery failed: "+err.Error())
		return
	}

	// Save to cache store under ProviderLabel ("cluster")
	if err := srv.Store.SaveCRDs(cluster.ProviderLabel, cl.Context(), crds); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to cache cluster schemas: "+err.Error())
		return
	}

	// Check if cluster is in Providers list; if not, append
	hasCluster := false
	for _, p := range srv.Providers {
		if p == cluster.ProviderLabel {
			hasCluster = true
			break
		}
	}
	if !hasCluster {
		srv.Providers = append(srv.Providers, cluster.ProviderLabel)
	}

	// Rebuild index
	byProvider := make(map[string][]schema.CRD, len(srv.Providers)+1)
	for _, ref := range srv.Providers {
		c, err := srv.Store.Load(ref)
		if err != nil {
			continue
		}
		byProvider[ref] = c
	}

	native, err := k8s.Kinds()
	if err == nil {
		byProvider[blueprint.NativeProvider] = native
	}

	newIdx, err := index.Build(byProvider)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to build index: "+err.Error())
		return
	}

	srv.Index = newIdx

	writeJSON(w, http.StatusOK, cluster.ClusterInfo{
		Connected: true,
		Context:   cl.Context(),
		Server:    cl.Server(),
		CRDCount:  len(crds),
	})
}
