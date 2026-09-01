package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/rendertest"
	"sigs.k8s.io/yaml"
)

// okRenderStream is a realistic `crossplane composition render` success
// output for the shared test blueprint's shape: the XR document first
// (identifiable by carrying NO composition-resource-name annotation), then
// two composed resources, each carrying the crossplane.io/
// composition-resource-name annotation the render command stamps on every
// composed resource it prints. The handler must count the annotated
// documents — 2 — and never the XR.
const okRenderStream = `---
apiVersion: platform.sparky.ee/v1alpha1
kind: XQueue
metadata:
  name: render-check
  namespace: default
spec:
  providerName: sample
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
---
apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: dead-letter-queue
  generateName: render-check-
spec:
  forProvider:
    region: eu-north-1
`

// dockerDownOutput is the verbatim error `crossplane composition render`
// (client v2.5.0) prints when the Docker daemon is not running — captured
// from a real run, not invented — so the docker-unavailable classification
// is tested against the exact text it must recognise.
const dockerDownOutput = `crossplane: error: cannot create Docker network for rendering: cannot create Docker network "crossplane-render-2t87lbdb": Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?`

// testRenderServer builds the shared test server with the render and
// lookPath seams swapped, from the same testServerOptions construction path
// every other test server uses (the testProviderServer pattern). lookPath
// defaults to a stub that always succeeds, so handler tests exercise the
// render path on machines that do not have the crossplane CLI installed;
// the unavailable test overrides it through the returned Options pattern
// below instead.
func testRenderServer(t *testing.T, render func(ctx context.Context, xr, comp, fns, xrd string) ([]byte, error)) (http.Handler, Options) {
	t.Helper()
	o := testServerOptions(t)
	o.render = render
	o.lookPath = func(string) (string, error) { return "/fake/bin/crossplane", nil }
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, o
}

// decodeRenderResponse decodes rec's body into the response envelope and
// also confirms all four keys are literally present in the JSON — the
// contract says every key is always present, not omitted when zero.
func decodeRenderResponse(t *testing.T, rec *httptest.ResponseRecorder) renderResponse {
	t.Helper()
	var resp renderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body)
	}
	for _, key := range []string{`"ok"`, `"resources"`, `"error"`, `"unavailable"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("response is missing the always-present key %s: %s", key, rec.Body)
		}
	}
	return resp
}

// TestRenderCountsComposedResources is the happy path: the (fake) render
// returns a 3-document stream — XR plus two composed resources — and the
// response reports ok with exactly the two composed resources, excluding
// the XR document itself.
//
// It also pins two mechanics of the handler's setup work, observable only
// from inside the runner seam: every path handed to the command must be a
// real file on disk (the real CLI reads them), and the synthesized sample
// XR must carry the blueprint's identity plus a placeholder for each
// REQUIRED parameter and nothing for optional ones (so the render exercises
// the composition's default-injection path for those).
func TestRenderCountsComposedResources(t *testing.T) {
	var gotXR []byte
	h, _ := testRenderServer(t, func(_ context.Context, xr, comp, fns, xrd string) ([]byte, error) {
		for _, p := range []string{xr, comp, fns, xrd} {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("render was handed a path that is not a readable file: %v", err)
			}
		}
		var err error
		if gotXR, err = os.ReadFile(xr); err != nil {
			t.Errorf("read synthesized XR: %v", err)
		}
		return []byte(okRenderStream), nil
	})

	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got := decodeRenderResponse(t, rec)
	want := renderResponse{OK: true, Resources: 2}
	if got != want {
		t.Errorf("response = %+v, want %+v", got, want)
	}

	// The sample XR, as the runner saw it: testBlueprintYAML declares
	// providerName (required string) and maxMessageSize (optional integer),
	// so spec must hold exactly {providerName: sample}.
	var xr struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec map[string]any `json:"spec"`
	}
	if err := yaml.Unmarshal(gotXR, &xr); err != nil {
		t.Fatalf("synthesized XR is not YAML: %v\n%s", err, gotXR)
	}
	if xr.APIVersion != "platform.sparky.ee/v1alpha1" || xr.Kind != "XQueue" {
		t.Errorf("XR identity = %s/%s, want platform.sparky.ee/v1alpha1/XQueue", xr.APIVersion, xr.Kind)
	}
	if xr.Metadata.Name != "render-check" || xr.Metadata.Namespace != "default" {
		t.Errorf("XR metadata = %+v, want name render-check in namespace default (Namespaced scope)", xr.Metadata)
	}
	if diff := cmp.Diff(map[string]any{"providerName": "sample"}, xr.Spec); diff != "" {
		t.Errorf("XR spec (-want +got):\n%s", diff)
	}
}

// TestRenderFailureCarriesRenderOutputVerbatim: a render that fails for a
// composition-level reason (here go-templating's missingkey=error firing)
// is an outcome, not an HTTP error — 200 with ok:false and the command's
// combined output verbatim in error, unavailable empty.
func TestRenderFailureCarriesRenderOutputVerbatim(t *testing.T) {
	const output = `crossplane: error: cannot render composition: pipeline step "render-templates": run function: template: manifests:12:14: executing "manifests" at <.observed.composite.resource.spec.maxMessageSize>: map has no entry for key "maxMessageSize"`
	h, _ := testRenderServer(t, func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte(output + "\n"), errors.New("exit status 1")
	})

	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got := decodeRenderResponse(t, rec)
	want := renderResponse{OK: false, Error: output}
	if got != want {
		t.Errorf("response = %+v, want the render output verbatim in error: %+v", got, want)
	}
}

// TestRenderUnavailableWhenCrossplaneIsNotInstalled: a LookPath miss means
// the check cannot run at all — unavailable carries the reason, error stays
// empty (nothing failed; nothing ran), and the runner is never invoked.
func TestRenderUnavailableWhenCrossplaneIsNotInstalled(t *testing.T) {
	o := testServerOptions(t)
	o.lookPath = func(file string) (string, error) {
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}
	o.render = func(context.Context, string, string, string, string) ([]byte, error) {
		t.Error("render ran despite the crossplane binary being unavailable")
		return nil, nil
	}
	h, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got := decodeRenderResponse(t, rec)
	if got.OK || got.Resources != 0 || got.Error != "" {
		t.Errorf("response = %+v, want ok:false with empty error and zero resources", got)
	}
	if !strings.Contains(got.Unavailable, "crossplane") {
		t.Errorf("unavailable = %q, want it to name the missing crossplane binary", got.Unavailable)
	}
}

// TestRenderUnavailableWhenDockerIsDown: the binary exists but the render
// fails with Docker-daemon connectivity output — that is the environment
// being unable to run the check, not the blueprint failing it, so it is
// reported as unavailable (with the output as the reason), never as a
// render error and never as a fake ok.
func TestRenderUnavailableWhenDockerIsDown(t *testing.T) {
	h, _ := testRenderServer(t, func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte(dockerDownOutput + "\n"), errors.New("exit status 1")
	})

	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got := decodeRenderResponse(t, rec)
	want := renderResponse{OK: false, Unavailable: dockerDownOutput}
	if got != want {
		t.Errorf("response = %+v, want %+v", got, want)
	}
}

// TestRenderValidationFailureIs400: the same split every other route uses —
// a blueprint that fails validation is a 400 carrying the engine's own
// error, and nothing is ever rendered from an invalid document.
func TestRenderValidationFailureIs400(t *testing.T) {
	h, o := testRenderServer(t, func(context.Context, string, string, string, string) ([]byte, error) {
		t.Error("render ran for a blueprint that does not validate")
		return nil, nil
	})
	// Corrupt the blueprint on disk behind the server's back, the same way
	// TestGenerateSurfacesValidationErrorsAsIs does.
	body, err := os.ReadFile(o.Blueprint)
	if err != nil {
		t.Fatalf("read blueprint: %v", err)
	}
	corrupted := strings.Replace(string(body), "scope: Namespaced", "scope: Cluster", 1)
	if err := os.WriteFile(o.Blueprint, []byte(corrupted), 0o644); err != nil {
		t.Fatalf("corrupt blueprint: %v", err)
	}

	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Cluster") {
		t.Errorf("the engine's own error was not surfaced: %s", rec.Body)
	}
}

// TestRenderIsNeverAnsweredWith304: POST /api/render must not participate
// in conditional-GET shortcuts — the same rule fix round 2 established for
// every mutating method (see wrap in server.go). If-None-Match: * matches
// any ETag, so a regression re-enabling 304 on POST cannot slip past this.
func TestRenderIsNeverAnsweredWith304(t *testing.T) {
	h, _ := testRenderServer(t, func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte(okRenderStream), nil
	})

	req := httptest.NewRequest("POST", "/api/render", nil)
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — POST must never be answered with a 304: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() == 0 {
		t.Error("POST response body is empty — looks like it was answered as a 304")
	}
}

// TestSampleXRUsesTypeAppropriatePlaceholders pins the synthesis rules for
// every parameter type directly against sampleXR: enum takes its first
// value (even on a non-string type), string "sample", integer/number 1,
// boolean true; non-required parameters are omitted entirely so the render
// exercises the composition's default-injection path for them.
func TestSampleXRUsesTypeAppropriatePlaceholders(t *testing.T) {
	b := &blueprint.Blueprint{}
	b.Spec.XRD = blueprint.XRD{
		Group:   "platform.sparky.ee",
		Version: "v1alpha1",
		Kind:    "XThing",
		Scope:   "Namespaced",
		Parameters: map[string]blueprint.Parameter{
			"location":     {Type: "string", Required: true, Enum: []string{"EU", "US"}},
			"providerName": {Type: "string", Required: true},
			"size":         {Type: "integer", Required: true},
			"ratio":        {Type: "number", Required: true},
			"encrypted":    {Type: "boolean", Required: true},
			"maxDepth":     {Type: "integer", Default: "4"},
			"comment":      {Type: "string"},
		},
	}

	raw, err := sampleXR(b)
	if err != nil {
		t.Fatalf("sampleXR: %v", err)
	}
	var xr struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Metadata   map[string]any `json:"metadata"`
		Spec       map[string]any `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &xr); err != nil {
		t.Fatalf("sampleXR output is not YAML: %v\n%s", err, raw)
	}

	if xr.APIVersion != "platform.sparky.ee/v1alpha1" || xr.Kind != "XThing" {
		t.Errorf("identity = %s/%s, want platform.sparky.ee/v1alpha1/XThing", xr.APIVersion, xr.Kind)
	}
	wantMeta := map[string]any{"name": "render-check", "namespace": "default"}
	if diff := cmp.Diff(wantMeta, xr.Metadata); diff != "" {
		t.Errorf("metadata (-want +got):\n%s", diff)
	}
	// YAML numbers decode to float64 through the JSON round trip.
	wantSpec := map[string]any{
		"location":     "EU", // first enum value, not "sample"
		"providerName": "sample",
		"size":         float64(1),
		"ratio":        float64(1),
		"encrypted":    true,
	}
	if diff := cmp.Diff(wantSpec, xr.Spec); diff != "" {
		t.Errorf("spec (-want +got):\n%s", diff)
	}
}

// TestSampleXROmitsNamespaceOutsideNamespacedScope: the namespace is tied
// to the XRD's scope, not written unconditionally — a cluster-scoped XR
// with a namespace would be rejected by the render's schema validation the
// day Cluster scope lands in this generator.
func TestSampleXROmitsNamespaceOutsideNamespacedScope(t *testing.T) {
	b := &blueprint.Blueprint{}
	b.Spec.XRD = blueprint.XRD{
		Group: "platform.sparky.ee", Version: "v1alpha1", Kind: "XThing", Scope: "Cluster",
	}
	raw, err := sampleXR(b)
	if err != nil {
		t.Fatalf("sampleXR: %v", err)
	}
	var xr struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &xr); err != nil {
		t.Fatalf("sampleXR output is not YAML: %v\n%s", err, raw)
	}
	want := map[string]any{"name": "render-check"}
	if diff := cmp.Diff(want, xr.Metadata); diff != "" {
		t.Errorf("metadata (-want +got):\n%s", diff)
	}
}

// TestRenderIntegrationRealCrossplane runs the whole route against the real
// crossplane CLI and Docker — the same spirit as the repo's network-gated
// acceptance test, and skipped for the same reasons: no binary, no daemon,
// or -short. When it does run, the shared test blueprint composes exactly
// one resource (main-queue), so the count is pinned, not just "some".
func TestRenderIntegrationRealCrossplane(t *testing.T) {
	if testing.Short() {
		t.Skip("real crossplane render needs Docker; skipped under -short")
	}
	if _, err := exec.LookPath("crossplane"); err != nil {
		t.Skipf("crossplane not installed: %v", err)
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	h := testHandler(t) // nil seams: the real exec path end to end

	// Serialized against the root acceptance tests' renders: all of them
	// reuse the same runtime-docker-name containers, and two renders in
	// concurrently running test processes race on that name — see
	// internal/rendertest.
	release := rendertest.Lock(t)
	defer release()
	rec := do(t, h, "POST", "/api/render", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	resp := decodeRenderResponse(t, rec)
	// Same once-only retry the root acceptance tests' renderComposition
	// applies, for the same pinned-container/network race: dockerd settles a
	// previous render's network teardown asynchronously after that crossplane
	// process exits, so no test-side serialization closes the window
	// completely. Gated on the exact error text; anything else fails below,
	// unretried.
	if !resp.OK && strings.Contains(resp.Error, "is not connected to Docker network") {
		t.Logf("retrying render once after the pinned-container/network race: %s", resp.Error)
		_ = exec.Command("docker", "rm", "-f", "cf-function-go-templating", "cf-function-auto-ready").Run()
		rec = do(t, h, "POST", "/api/render", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		resp = decodeRenderResponse(t, rec)
	}
	if resp.Unavailable != "" {
		// docker info passed above but the render still could not reach the
		// runtime — an environment problem, not a code one.
		t.Skipf("render environment unavailable despite docker info passing: %s", resp.Unavailable)
	}
	if !resp.OK {
		t.Fatalf("real render failed: %s", resp.Error)
	}
	if resp.Resources != 1 {
		t.Errorf("resources = %d, want 1 (the test blueprint composes exactly main-queue)", resp.Resources)
	}
}
