// Tests for typed object parameters at the HTTP boundary: the
// silently-dropped guard covers properties, and the 409 classification
// counts member references (params.<name>.<member>) as references.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// putTypedBlueprint GETs the current document, grafts a typed object
// parameter with a wired member onto it, PUTs it back, and returns the
// handler for follow-up requests.
func putTypedBlueprint(t *testing.T) http.Handler {
	t.Helper()
	h := testHandler(t)

	rec := do(t, h, "GET", "/api/blueprint", "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/blueprint: %d: %s", rec.Code, rec.Body)
	}
	var b blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode blueprint: %v", err)
	}
	b.Spec.XRD.Parameters["tuning"] = blueprint.Parameter{
		Type: "object",
		Properties: map[string]blueprint.Parameter{
			"maxSize": {Type: "integer", Default: "2048"},
		},
	}
	// unwired exists so the silently-dropped guard is tested on its own:
	// nothing references it, so without the guard a partial PUT would
	// succeed and quietly erase the member schema.
	b.Spec.XRD.Parameters["unwired"] = blueprint.Parameter{
		Type: "object",
		Properties: map[string]blueprint.Parameter{
			"mode": {Type: "string"},
		},
	}
	b.Spec.Resources[0].Fields["maxMessageSize"] = blueprint.Field{From: "params.tuning.maxSize"}

	body, err := json.Marshal(&b)
	if err != nil {
		t.Fatalf("marshal blueprint: %v", err)
	}
	if rec := do(t, h, "PUT", "/api/blueprint", string(body)); rec.Code != 200 {
		t.Fatalf("PUT /api/blueprint: %d: %s", rec.Code, rec.Body)
	}
	return h
}

// PUT /api/blueprint/parameters/{name} replaces the whole parameter, so a
// body that omits properties while the declaration holds members would
// silently discard the member schema — and every member wire with it. The
// silently-dropped guard has to name properties like any other key.
func TestSetParameterRefusesSilentPropertiesDrop(t *testing.T) {
	h := putTypedBlueprint(t)

	rec := do(t, h, "PUT", "/api/blueprint/parameters/unwired",
		`{"parameter":{"type":"object"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "refusing a partial update") ||
		!strings.Contains(rec.Body.String(), "properties") {
		t.Errorf("error must be the partial-update refusal naming properties: %s", rec.Body)
	}
}

// Deleting a parameter whose members are wired is a conflict (409), not a
// generic validation failure: the HTTP layer's referencingResources copy has
// to count params.<name>.<member> references, exactly as the edit layer's
// does.
func TestDeleteParameterWithMemberReferenceIs409(t *testing.T) {
	h := putTypedBlueprint(t)

	rec := do(t, h, "DELETE", "/api/blueprint/parameters/tuning", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "main-queue") {
		t.Errorf("error must name the referencing resource: %s", rec.Body)
	}
}

// A typed parameter round-trips through GET → PUT → GET intact: the JSON
// tags carry properties, and re-persisting does not grow a properties key on
// the parameters that never declared one.
func TestTypedParameterSurvivesTheAPIRoundTrip(t *testing.T) {
	h := putTypedBlueprint(t)

	rec := do(t, h, "GET", "/api/blueprint", "")
	if rec.Code != 200 {
		t.Fatalf("GET: %d: %s", rec.Code, rec.Body)
	}
	var b blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m, ok := b.Spec.XRD.Parameters["tuning"].Properties["maxSize"]
	if !ok || m.Type != "integer" || m.Default != "2048" {
		t.Fatalf("tuning.properties.maxSize = %+v, want the declared integer member", m)
	}
	// The document as served must not have grown properties keys elsewhere.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	params := raw["spec"].(map[string]any)["xrd"].(map[string]any)["parameters"].(map[string]any)
	for name, p := range params {
		if name == "tuning" || name == "unwired" {
			continue
		}
		if _, has := p.(map[string]any)["properties"]; has {
			t.Errorf("parameter %q gained a properties key it never declared", name)
		}
	}
}
