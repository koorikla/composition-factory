package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/api"
)

// TestCF493GenWarnsOptionalParamIntoRequiredField pins the CLI half of the
// check the canvas already performs: when an optional XRD parameter is wired to
// a field the CRD marks required, `cf gen` must say so, naming the resource and
// the field, instead of emitting the guarded template in silence at exit 0.
//
// The genCRDs fixture marks spec.forProvider.region required on Queue; the
// blueprint below leaves the `region` parameter optional and wires it there.
func TestCF493GenWarnsOptionalParamIntoRequiredField(t *testing.T) {
	dir, _, cacheDir := seed(t)

	const optional = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xqueue}
spec:
  sources:
    - provider: example.org/provider-test:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      region: {type: string, required: false}
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {from: params.region}
`
	bpPath := filepath.Join(dir, "optreq.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(optional), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code, err := (&GenCmd{Blueprint: bpPath, Out: filepath.Join(dir, "out1"), CacheDir: cacheDir}).run(&buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("cf gen exit=%d, want 0: generating this blueprint is legal; the point is that it must not be silent\n%s", code, buf.String())
	}
	report := buf.String()
	if !strings.Contains(report, "region") || !strings.Contains(report, "main-queue") {
		t.Fatalf("cf gen said nothing about the optional parameter wired to Queue's required field.\n"+
			"expected its report to name the resource (main-queue) and the field (region); it said:\n%s", report)
	}

	// Control: a parameter the XRD marks required feeds the same field
	// legitimately, and must not draw the same remark -- a warning that fires
	// on every wire tells the user nothing.
	required := strings.Replace(optional, "region: {type: string, required: false}", "region: {type: string, required: true}", 1)
	reqPath := filepath.Join(dir, "reqreq.cf.yaml")
	if err := os.WriteFile(reqPath, []byte(required), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	code, err = (&GenCmd{Blueprint: reqPath, Out: filepath.Join(dir, "out2"), CacheDir: cacheDir}).run(&buf)
	if err != nil {
		t.Fatalf("run (required param): %v", err)
	}
	if code != 0 {
		t.Fatalf("cf gen exit=%d on the all-required blueprint, want 0:\n%s", code, buf.String())
	}
	if strings.Contains(buf.String(), "region") {
		t.Errorf("cf gen remarked on a required parameter wired to a required field; the warning must distinguish the two.\nit said:\n%s", buf.String())
	}
}

func TestCF493GenWarnsNestedOptionalParamIntoRequiredField(t *testing.T) {
	dir, _, cacheDir := seed(t)

	// Parent object is optional, member is required -> still optional overall!
	const nestedOpt = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xqueue}
spec:
  sources:
    - provider: example.org/provider-test:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      network:
        type: object
        required: false
        properties:
          region:
            type: string
            required: true
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {from: params.network.region}
`
	bpPath := filepath.Join(dir, "nested_opt.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(nestedOpt), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code, err := (&GenCmd{Blueprint: bpPath, Out: filepath.Join(dir, "out_nested1"), CacheDir: cacheDir}).run(&buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("cf gen exit=%d, want 0:\n%s", code, buf.String())
	}
	report := buf.String()
	if !strings.Contains(report, "network.region") || !strings.Contains(report, "main-queue") {
		t.Fatalf("expected warning mentioning network.region and main-queue, got:\n%s", report)
	}

	// Control: parent is required AND member is required -> no warning
	nestedReq := strings.Replace(nestedOpt, "required: false", "required: true", 1)
	reqPath := filepath.Join(dir, "nested_req.cf.yaml")
	if err := os.WriteFile(reqPath, []byte(nestedReq), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	code, err = (&GenCmd{Blueprint: reqPath, Out: filepath.Join(dir, "out_nested2"), CacheDir: cacheDir}).run(&buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("cf gen exit=%d, want 0:\n%s", code, buf.String())
	}
	if strings.Contains(buf.String(), "network.region") {
		t.Errorf("expected no warning when nested param and its parent are both required, got:\n%s", buf.String())
	}
}

func TestCF493APIGenerateCarriesWarnings(t *testing.T) {
	dir, _, cacheDir := seed(t)

	const optional = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xqueue}
spec:
  sources:
    - provider: example.org/provider-test:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      region: {type: string, required: false}
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {from: params.region}
`
	bpPath := filepath.Join(dir, "optreq_api.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(optional), 0o644); err != nil {
		t.Fatal(err)
	}

	opts, err := buildAPIOptions(bpPath, cacheDir, filepath.Join(dir, "out_api"), filepath.Join(dir, ".cf.lock"), nil, false)
	if err != nil {
		t.Fatalf("buildAPIOptions: %v", err)
	}
	h, err := api.New(opts)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/generate", strings.NewReader(`{"write":false}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/generate code = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Outputs  []any    `json:"outputs"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("expected warnings in API response, got none: %s", rec.Body.String())
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "region") && strings.Contains(w, "main-queue") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning region and main-queue, got: %v", resp.Warnings)
	}
}
