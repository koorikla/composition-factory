package api

import (
	"testing"

	"github.com/koorikla/compositionfactory/internal/index"
)

// fieldsResponse mirrors GET /api/kinds/{av}/{kind}/fields' body for these
// tests, including the requiredBranches list handleKindFields serves.
type fieldsResponse struct {
	Fields           []index.Field `json:"fields"`
	Total            int           `json:"total"`
	RequiredBranches []index.Field `json:"requiredBranches"`
}

// Effective requiredness at the HTTP boundary, against the vendored
// Deployment schema. The RAW flag marks 250 of 842 leaves required
// (members of optional objects, EnvVar.name and friends) — noise for a
// "what must I set" filter — while the two fields a user must actually set,
// spec.selector and spec.template, are BRANCH nodes the leaf list
// structurally drops. So: required_only=true must return exactly the
// chain-true leaves (the vendored data proves there are none — every
// required leaf sits under an unrequired ancestor, see
// internal/schema/k8s/requiredchain_test.go), and requiredBranches must
// carry exactly selector and template. The raw flag still travels on every
// row for the badge, and stays a strict minority of the full listing.
func TestNativeDeploymentRequiredOnlyServesEffectiveRequireds(t *testing.T) {
	h := nativeTestHandler(t)

	var all, req fieldsResponse
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment/fields", &all); code != 200 {
		t.Fatalf("GET fields: status %d", code)
	}
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment/fields?required_only=true", &req); code != 200 {
		t.Fatalf("GET fields?required_only=true: status %d", code)
	}
	if all.Total == 0 {
		t.Fatal("Deployment served no fields at all")
	}

	// required_only must be exactly the chain-required subset of the full
	// listing — for the vendored Deployment, the empty set.
	wantChain := map[string]bool{}
	for _, f := range all.Fields {
		if f.RequiredChain {
			wantChain[f.Path] = true
		}
	}
	if len(wantChain) != 0 {
		t.Errorf("full listing flags %d chain-required leaves, want 0 (the vendored data proves none)", len(wantChain))
	}
	if len(req.Fields) != 0 || req.Total != 0 {
		t.Errorf("required_only returned %d leaves (total %d), want 0: every raw-required Deployment leaf "+
			"sits under an unrequired ancestor", len(req.Fields), req.Total)
	}

	// The two fields a user must actually set surface as required branches —
	// on BOTH responses, filtered by nothing.
	for _, resp := range []struct {
		name string
		got  []index.Field
	}{{"default listing", all.RequiredBranches}, {"required_only", req.RequiredBranches}} {
		var paths []string
		for _, b := range resp.got {
			paths = append(paths, b.Path)
		}
		if len(paths) != 2 || paths[0] != "spec.selector" || paths[1] != "spec.template" {
			t.Errorf("%s requiredBranches = %v, want exactly [spec.selector spec.template]", resp.name, paths)
		}
	}

	// The raw flag is untouched by all of this: the schema's actual
	// requireds still carry it, its actual optionals still do not, and raw
	// requiredness stays a strict minority of the tree (~30% for the
	// vendored v1.34.1; an inversion lands at ~70%).
	raw := map[string]bool{}
	rawCount := 0
	for _, f := range all.Fields {
		raw[f.Path] = f.Required
		if f.Required {
			rawCount++
		}
	}
	for _, want := range []string{
		"spec.template.spec.containers[0].name",
		"spec.template.spec.containers[0].ports[0].containerPort",
	} {
		if !raw[want] {
			t.Errorf("raw required flag missing on %q, which the vendored schema requires", want)
		}
	}
	for _, absent := range []string{
		"spec.template.spec.containers[0].image",
		"spec.replicas",
	} {
		if raw[absent] {
			t.Errorf("raw required flag set on %q, which the vendored schema does NOT require", absent)
		}
	}
	if rawCount == 0 {
		t.Error("no raw-required leaves at all — the vendored required arrays are being dropped")
	}
	if rawCount*2 >= all.Total {
		t.Errorf("%d of %d leaves carry the raw required flag (>= half) — it looks inverted or defaulted",
			rawCount, all.Total)
	}
}
