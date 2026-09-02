package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

// TestRBACGoldenForTheDemoBlueprint pins GET /api/rbac's whole response for
// the shared test blueprint (a Namespaced XQueue composing one Queue): the
// XR's own rule first, then the composed Queue's — resolved to the
// NAMESPACED variant's group (sqs.aws.m.upbound.io), never the cluster
// twin's — each carrying the full manage verb set the v1 broad-by-default
// ruling grants.
func TestRBACGoldenForTheDemoBlueprint(t *testing.T) {
	var got rbacResponse
	if code := getJSON(t, testHandler(t), "/api/rbac", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	want := rbacResponse{Rules: []rbacRule{
		{APIGroups: []string{"platform.sparky.ee"}, Resources: []string{"xqueues"}, Verbs: manageVerbs, Scope: "Namespaced"},
		{APIGroups: []string{"sqs.aws.m.upbound.io"}, Resources: []string{"queues"}, Verbs: manageVerbs, Scope: "Namespaced"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rbac = %+v, want %+v", got, want)
	}
}

// emptyResourcesBlueprintYAML is the shared test blueprint with its resources
// removed: still valid (a Namespaced XRD with its required providerName), but
// composing nothing.
const emptyResourcesBlueprintYAML = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: ghcr.io/x/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
  resources: []
`

// TestRBACEmptyBlueprintIsJustTheXR: a blueprint composing no resources
// still reports exactly one rule — the XR's own group/plural — because the
// composition machinery manages the composite itself regardless of what it
// composes. The handlers load the blueprint from disk per request, so the
// file is rewritten in place and the very next GET must reflect it.
func TestRBACEmptyBlueprintIsJustTheXR(t *testing.T) {
	h, path := testHandlerWithPath(t)
	if err := os.WriteFile(path, []byte(emptyResourcesBlueprintYAML), 0o644); err != nil {
		t.Fatalf("rewrite blueprint: %v", err)
	}

	var got rbacResponse
	if code := getJSON(t, h, "/api/rbac", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	want := rbacResponse{Rules: []rbacRule{
		{APIGroups: []string{"platform.sparky.ee"}, Resources: []string{"xqueues"}, Verbs: manageVerbs, Scope: "Namespaced"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rbac = %+v, want only the XR's own rule: %+v", got, want)
	}
}

// twoQueueBlueprintYAML declares TWO resources of the same kind, so the
// deterministic response must collapse them into one composed rule.
const twoQueueBlueprintYAML = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: ghcr.io/x/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/x/provider-aws-sqs:v2.7.0
      fields:
        maxMessageSize: {from: params.maxMessageSize}
    - name: dead-letter-queue
      kind: Queue
      provider: ghcr.io/x/provider-aws-sqs:v2.7.0
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

// TestRBACIsDeterministicAndDeduplicated: two resources composing the same
// kind yield ONE composed rule, and repeated GETs of the same blueprint are
// byte-identical (which the ETag middleware then turns into 304s — the
// response being byte-stable is what makes that caching honest).
func TestRBACIsDeterministicAndDeduplicated(t *testing.T) {
	h, path := testHandlerWithPath(t)
	if err := os.WriteFile(path, []byte(twoQueueBlueprintYAML), 0o644); err != nil {
		t.Fatalf("rewrite blueprint: %v", err)
	}

	first := do(t, h, "GET", "/api/rbac", "")
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", first.Code, first.Body)
	}
	second := do(t, h, "GET", "/api/rbac", "")
	if second.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", second.Code, second.Body)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("two GETs of the same blueprint differ:\n%s\n%s", first.Body, second.Body)
	}

	var got rbacResponse
	if code := getJSON(t, h, "/api/rbac", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	want := rbacResponse{Rules: []rbacRule{
		{APIGroups: []string{"platform.sparky.ee"}, Resources: []string{"xqueues"}, Verbs: manageVerbs, Scope: "Namespaced"},
		{APIGroups: []string{"sqs.aws.m.upbound.io"}, Resources: []string{"queues"}, Verbs: manageVerbs, Scope: "Namespaced"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rbac = %+v, want the duplicate Queue collapsed: %+v", got, want)
	}
}

func TestRBACReportsAllSupportedKindsOnUnknownNativeKind(t *testing.T) {
	h, path := testHandlerWithPath(t)
	unknownBlueprint := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters: {}
  resources:
    - name: bad-res
      kind: FakeNativeObject
      provider: k8s
      fields: {}
`
	if err := os.WriteFile(path, []byte(unknownBlueprint), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/rbac", nil))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error body: %v\n%s", err, rec.Body.String())
	}
	if !strings.Contains(got.Error, "is not one of the vendored native Kubernetes kinds") {
		t.Errorf("got error %q, want mention of vendored native Kubernetes kinds", got.Error)
	}
	for _, k := range k8s.KindNames() {
		if !strings.Contains(got.Error, k) {
			t.Errorf("got error %q, missing native kind %q", got.Error, k)
		}
	}
}
