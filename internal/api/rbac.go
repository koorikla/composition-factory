// This file implements the /api/rbac route: the Kubernetes RBAC rules the
// current blueprint's composed resources need, computed for the canvas to
// render — what a platform team must grant Crossplane's provider/controller
// side for this composition to actually reconcile.
package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// manageVerbs is the verb set granted for every rule this endpoint reports.
// See handleRBAC's doc comment for the v1 broad-by-default ruling behind it.
var manageVerbs = []string{"get", "list", "watch", "create", "update", "patch", "delete"}

// rbacRule is one rule in GET /api/rbac's response: the apiGroups/resources/
// verbs triple a Kubernetes RBAC rule is made of, plus the scope
// (Namespaced | Cluster) so the canvas can say whether a Role or a
// ClusterRole is called for. apiGroups and resources are lists — matching
// the rbac.authorization.k8s.io shape they will be pasted into — even though
// v1 always emits exactly one entry in each.
type rbacRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
	Scope     string   `json:"scope"`
}

// rbacResponse is GET /api/rbac's body.
type rbacResponse struct {
	Rules []rbacRule `json:"rules"`
}

// handleRBAC serves GET /api/rbac: for every composed resource kind in the
// current blueprint, the RBAC rule Crossplane's provider/controller needs to
// manage that kind — apiGroups (the kind's group), resources (its plural)
// and verbs — plus one rule for the XR's own group/plural, since the
// composition machinery manages the composite itself too. Group and plural
// come from the index (never re-derived by pluralizing a kind name by hand);
// the XR's come from the blueprint's own XRD declaration.
//
// V1 RULING — BROAD BY DEFAULT, deliberately: every rule carries the full
// manage verb set [get,list,watch,create,update,patch,delete]. No attempt is
// made to narrow verbs per kind (e.g. an observe-only resource needing no
// create/delete): the blueprint does not yet express management policies, so
// any narrowing here would be a guess, and a guessed-too-narrow grant fails
// at reconcile time in the cluster — far from this tool and long after
// generation. Broad-but-honest is the v1 contract; narrowing becomes
// possible the day the blueprint states per-resource management intent.
//
// Determinism: the XR's rule is always first, composed-kind rules follow
// sorted by (group, plural), and duplicates (two resources composing the
// same kind) collapse to one rule — so the same blueprint always yields the
// byte-identical response, whatever order the resources are declared in.
//
// Status codes follow the project's split: 400 when the blueprint does not
// validate or names a kind the index cannot resolve (current blueprint/index
// state failing to produce an answer, reported like /api/generate's
// equivalent failures), 500 when the blueprint file itself cannot be read.
func (srv *server) handleRBAC(w http.ResponseWriter, _ *http.Request) {
	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	// One snapshot of the index for the whole response — see server.index.
	idx := srv.index()

	rules := []rbacRule{{
		APIGroups: []string{b.Spec.XRD.Group},
		Resources: []string{b.Spec.XRD.Plural},
		Verbs:     manageVerbs,
		Scope:     b.Spec.XRD.Scope,
	}}

	wantNamespaced := b.Spec.XRD.Scope == "Namespaced"
	seen := make(map[string]bool, len(b.Spec.Resources))
	var composed []rbacRule
	for _, res := range b.Spec.Resources {
		k, err := resolveIndexKind(idx, res, wantNamespaced)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		key := k.Group + "/" + k.Plural + "/" + k.Scope
		if seen[key] {
			continue
		}
		seen[key] = true
		composed = append(composed, rbacRule{
			APIGroups: []string{k.Group},
			Resources: []string{k.Plural},
			Verbs:     manageVerbs,
			Scope:     k.Scope,
		})
	}
	sort.Slice(composed, func(i, j int) bool {
		if composed[i].APIGroups[0] != composed[j].APIGroups[0] {
			return composed[i].APIGroups[0] < composed[j].APIGroups[0]
		}
		return composed[i].Resources[0] < composed[j].Resources[0]
	})

	writeJSON(w, http.StatusOK, rbacResponse{Rules: append(rules, composed...)})
}

// resolveIndexKind resolves one blueprint resource to its indexed Kind,
// mirroring emit's resolveKind (internal/emit/composition.go) over the index
// instead of a CRD list: filter to the resource's kind (and its provider,
// when the blueprint pins one — the index spans every cached provider, and
// two providers can legitimately ship the same Kind name under different
// groups), then require the scope variant the XRD needs. The wrong-scope
// fallback is an ERROR here exactly as it is there, not a silent substitute:
// the cluster and namespaced variants live in different groups, so reporting
// the fallback's group would grant RBAC for a resource this blueprint's
// generate step refuses to compose.
func resolveIndexKind(idx *index.Index, res blueprint.Resource, wantNamespaced bool) (index.Kind, error) {
	var fallback *index.Kind
	all := idx.All()
	for i := range all {
		k := all[i]
		if k.Kind != res.Kind {
			continue
		}
		if res.Provider != "" && k.Provider != res.Provider {
			continue
		}
		if k.Namespaced == wantNamespaced {
			return k, nil
		}
		if fallback == nil {
			fallback = &all[i]
		}
	}
	scope := "cluster-scoped"
	if wantNamespaced {
		scope = "namespaced"
	}
	if fallback != nil {
		return index.Kind{}, fmt.Errorf("resource %q: kind %q has no %s variant in the index (only %s in %s); "+
			"a %s XRD composes the matching variant", res.Name, res.Kind, scope, fallback.Scope, fallback.Group, scope)
	}
	if res.Provider == blueprint.NativeProvider {
		return index.Kind{}, fmt.Errorf("resource %q: kind %q is not one of the vendored native Kubernetes kinds (%s): "+
			"provider %q serves the subset pinned to Kubernetes %s", res.Name, res.Kind, strings.Join(k8s.KindNames(), ", "), blueprint.NativeProvider, k8s.Version)
	}
	return index.Kind{}, fmt.Errorf("resource %q: kind %q not found in any cached provider; run cf provider add",
		res.Name, res.Kind)
}
