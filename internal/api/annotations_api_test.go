// HTTP-layer coverage for resource annotations: the 409 classification
// mirrors (this package's referencingResources/statusReferencingResources
// must track annotation wires exactly as blueprint's unexported scans do, or
// the two layers silently disagree on conflict-vs-bad-request), and the PUT
// round trip (annotations survive decode -> validate -> persist -> reload,
// byte-stably).
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
)

// An annotation wire is a params.<name> reference exactly like a field's
// from: deleting its parameter must classify as 409, not fall through to
// 400 — the same HTTP-layer half of the referencer rule the forEach and
// when tests pin.
func TestDeleteAnnotationReferencedParameterIs409(t *testing.T) {
	h, path := testHandlerWithPath(t)
	withAnn := strings.Replace(testBlueprintYAML,
		"      maxMessageSize: {type: integer}",
		"      maxMessageSize: {type: integer}\n      team: {type: string}", 1)
	withAnn = strings.Replace(withAnn,
		"      fields:\n        maxMessageSize: {from: params.maxMessageSize}",
		"      fields:\n        maxMessageSize: {from: params.maxMessageSize}\n"+
			"      annotations:\n        example.com/team: {from: params.team}", 1)
	if err := os.WriteFile(path, []byte(withAnn), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := blueprint.Load(path); err != nil {
		t.Fatalf("mutated fixture does not itself validate: %v", err)
	}
	rec := do(t, h, "DELETE", "/api/blueprint/parameters/team", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 — team is still referenced by an annotation: %s",
			rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "main-queue") {
		t.Errorf("body = %s, want it to name the annotated resource", rec.Body)
	}
}

// An annotation status wire keeps its source resource undeletable, exactly
// as a field wire does — 409, naming the wired resource.
func TestDeleteAnnotationStatusWiredResourceIs409(t *testing.T) {
	h, path := testHandlerWithPath(t)
	withSA := strings.Replace(testBlueprintYAML,
		"    - name: main-queue",
		"    - name: sa\n      kind: ServiceAccount\n      provider: k8s\n"+
			"      annotations:\n        eks.amazonaws.com/role-arn: {from: resources.main-queue.status.atProvider.arn}\n"+
			"    - name: main-queue", 1)
	if err := os.WriteFile(path, []byte(withSA), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := blueprint.Load(path); err != nil {
		t.Fatalf("mutated fixture does not itself validate: %v", err)
	}
	rec := do(t, h, "DELETE", "/api/blueprint/resources/main-queue", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 — main-queue's status is still wired into an annotation: %s",
			rec.Code, rec.Body)
	}
	// The body is JSON, so the engine's quoted resource name arrives escaped.
	if !strings.Contains(rec.Body.String(), `\"sa\"`) {
		t.Errorf("body = %s, want it to name the annotated resource", rec.Body)
	}
}

// The anti-silent-destruction rule for the whole-document route: a PUT
// carrying annotations persists them, GET and a disk reload agree, and
// repeating the identical PUT is byte-stable — annotations can never be
// silently shed by the decode -> validate -> persist path.
func TestPutBlueprintRoundTripsAnnotations(t *testing.T) {
	h, path := testHandlerWithPath(t)

	current := mustLoadBlueprint(t, path)
	updated := *current
	updated.Spec.Resources = append([]blueprint.Resource(nil), current.Spec.Resources...)
	res := updated.Spec.Resources[0]
	res.Annotations = map[string]blueprint.Field{
		"example.com/max-size":       {From: "params.maxMessageSize"},
		"eks.amazonaws.com/role-arn": {Value: "arn:aws:iam::123456789012:role/demo"},
	}
	updated.Spec.Resources[0] = res
	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal PUT body: %v", err)
	}

	rec := do(t, h, "PUT", "/api/blueprint", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status %d (DisallowUnknownFields rejecting annotations?): %s", rec.Code, rec.Body)
	}
	var fromPut blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &fromPut); err != nil {
		t.Fatalf("PUT response not JSON: %v", err)
	}
	if diff := cmp.Diff(res.Annotations, fromPut.Spec.Resources[0].Annotations); diff != "" {
		t.Errorf("PUT response annotations (-sent +got):\n%s", diff)
	}

	getRec := do(t, h, "GET", "/api/blueprint", "")
	var fromGet blueprint.Blueprint
	if err := json.Unmarshal(getRec.Body.Bytes(), &fromGet); err != nil {
		t.Fatalf("GET response not JSON: %v", err)
	}
	if diff := cmp.Diff(res.Annotations, fromGet.Spec.Resources[0].Annotations); diff != "" {
		t.Errorf("GET annotations (-sent +got):\n%s", diff)
	}

	reloaded := mustLoadBlueprint(t, path)
	if diff := cmp.Diff(res.Annotations, reloaded.Spec.Resources[0].Annotations); diff != "" {
		t.Errorf("persisted annotations (-sent +got):\n%s", diff)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first PUT: %v", err)
	}
	if rec := do(t, h, "PUT", "/api/blueprint", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("second PUT: status %d: %s", rec.Code, rec.Body)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second PUT: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("PUTting the identical annotated document twice produced different bytes:\n"+
			"--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}
