package api

import (
	"testing"

	"github.com/koorikla/compositionfactory/internal/index"
)

// The required flag /api/fields serves for a native kind is what the palette
// badges and the required_only filter run on; this test pins it at the HTTP
// boundary against the vendored Deployment schema's own requiredness rather
// than a hand-written field list. required_only=true must return exactly the
// fields the full listing flags required — a strict minority of the tree
// (the vendored v1.34.1 truth is 250 of 842) — with the schema's actual
// requireds (containers[0].name, ports[0].containerPort) present and its
// actual optionals (containers[0].image, spec.replicas) absent. A resolver
// that inverts or defaults the flag flips the minority check and surfaces
// image/replicas here, loudly.
func TestNativeDeploymentRequiredOnlyServesTheSchemasRequireds(t *testing.T) {
	h := nativeTestHandler(t)

	var all, req struct {
		Fields []index.Field `json:"fields"`
		Total  int           `json:"total"`
	}
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment/fields", &all); code != 200 {
		t.Fatalf("GET fields: status %d", code)
	}
	if code := getJSON(t, h, "/api/kinds/apps%2Fv1/Deployment/fields?required_only=true", &req); code != 200 {
		t.Fatalf("GET fields?required_only=true: status %d", code)
	}
	if all.Total == 0 {
		t.Fatal("Deployment served no fields at all")
	}

	// required_only must be exactly the required subset of the full listing.
	wantRequired := map[string]bool{}
	for _, f := range all.Fields {
		if f.Required {
			wantRequired[f.Path] = true
		}
	}
	if len(req.Fields) != len(wantRequired) || req.Total != len(wantRequired) {
		t.Errorf("required_only returned %d fields (total %d), want the %d the full listing flags required",
			len(req.Fields), req.Total, len(wantRequired))
	}
	got := map[string]bool{}
	for _, f := range req.Fields {
		if !f.Required {
			t.Errorf("required_only leaked unrequired field %q", f.Path)
		}
		got[f.Path] = true
	}

	// The vendored schema's requireds must be in; its optionals must not.
	for _, want := range []string{
		"spec.template.spec.containers[0].name",
		"spec.template.spec.containers[0].ports[0].containerPort",
	} {
		if !got[want] {
			t.Errorf("required_only is missing %q, which the vendored schema requires", want)
		}
	}
	for _, absent := range []string{
		"spec.template.spec.containers[0].image",
		"spec.replicas",
	} {
		if got[absent] {
			t.Errorf("required_only includes %q, which the vendored schema does NOT require", absent)
		}
	}

	// Requiredness stays a strict minority of the tree (~30% for the
	// vendored v1.34.1 Deployment; an inversion lands at ~70%).
	if req.Total*2 >= all.Total {
		t.Errorf("required_only total %d is at least half of %d fields — the required flag looks inverted or defaulted",
			req.Total, all.Total)
	}
}
