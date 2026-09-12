package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestAdoptEndpoint(t *testing.T) {
	h, bpPath := testHandlerWithPath(t)

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-adopted
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

	reqBody, _ := json.Marshal(map[string]any{
		"manifest": manifest,
		"persist":  true,
		"provider": testProviderRef,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var res adoptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !res.Persisted {
		t.Errorf("expected persisted=true")
	}
	if res.Blueprint.Metadata.Name != "test-adopted" {
		t.Errorf("blueprint name = %q, want test-adopted", res.Blueprint.Metadata.Name)
	}
	if len(res.Blueprint.Spec.Resources) != 1 {
		t.Fatalf("resources count = %d, want 1", len(res.Blueprint.Spec.Resources))
	}
	if res.Blueprint.Spec.Resources[0].Provider != testProviderRef {
		t.Errorf("resource provider = %q, want %q", res.Blueprint.Spec.Resources[0].Provider, testProviderRef)
	}
	if bpPath == "" {
		t.Fatal("empty bpPath")
	}
}

func TestAdoptEndpointWithPatchAndTransformAndLossReport(t *testing.T) {
	h, _ := testHandlerWithPath(t)

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.aws.example.org
spec:
  group: aws.example.org
  claimNames:
    kind: Queue
    plural: queues
  names:
    kind: XQueue
    plural: xqueues
  versions:
    - name: v1alpha1
      served: true
      referenceable: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                queueName:
                  type: string
                tags:
                  type: array
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: pt-adopted
spec:
  compositeTypeRef:
    apiVersion: aws.example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: queue
            base:
              apiVersion: sqs.aws.upbound.io/v1beta1
              kind: Queue
              spec:
                forProvider:
                  region: us-east-1
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.parameters.queueName
                toFieldPath: spec.forProvider.name
`

	reqBody, _ := json.Marshal(map[string]any{
		"manifest": manifest,
		"persist":  false,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var res adoptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if res.Blueprint.Metadata.Name != "pt-adopted" {
		t.Errorf("blueprint name = %q, want pt-adopted", res.Blueprint.Metadata.Name)
	}
	if len(res.Blueprint.Spec.Resources) != 1 {
		t.Fatalf("resources count = %d, want 1", len(res.Blueprint.Spec.Resources))
	}
	if res.Blueprint.Spec.Resources[0].Fields["name"].From != "params.queueName" {
		t.Errorf("queue name field = %+v, want From: params.queueName", res.Blueprint.Spec.Resources[0].Fields["name"])
	}
	if res.LossReport == nil || !res.LossReport.IsLossy() {
		t.Errorf("expected loss report in response due to claimNames and array tags")
	}
}

func TestConcurrentAdoptAndList(t *testing.T) {
	h, _ := testHandlerWithPath(t)

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-concurrent-adopted
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			reqBody, _ := json.Marshal(map[string]any{
				"manifest": manifest,
				"persist":  true,
				"provider": testProviderRef,
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("adopt status = %d, body: %s", rec.Code, rec.Body.String())
			}
		}(i)

		go func(id int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/providers", nil)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("list providers status = %d, body: %s", rec.Code, rec.Body.String())
			}
		}(i)
	}

	wg.Wait()
}

func TestCF205AdoptEndpointRecoversScalarTypeFromStore(t *testing.T) {
	h, _ := testHandlerWithPath(t)

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-adopt-scalar
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.m.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
                maxMessageSize: {{ $spec.maxMessageSize }}
`

	reqBody, _ := json.Marshal(map[string]any{
		"manifest": manifest,
		"persist":  true,
		"provider": testProviderRef,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var res adoptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	param, ok := res.Blueprint.Spec.XRD.Parameters["maxMessageSize"]
	if !ok {
		t.Fatalf("parameter maxMessageSize missing from adopted blueprint")
	}
	// The CRD in testHandlerWithPath's store defines maxMessageSize as integer.
	if param.Type != "integer" {
		t.Errorf("parameter maxMessageSize type = %q, want integer", param.Type)
	}

	if res.LossReport != nil {
		for _, d := range res.LossReport.Drops {
			if d.Path == "xrd.parameters.maxMessageSize" && strings.Contains(d.Reason, "type") {
				t.Errorf("unexpected type loss recorded: %s", d.Reason)
			}
		}
	}
}

func TestAdoptEndpointErrorDoesNotDuplicatePrefix(t *testing.T) {
	h, _ := testHandlerWithPath(t)

	// Manifest with XRD but without Composition will fail adoption because no Composition document is found.
	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.aws.example.org
`

	reqBody, _ := json.Marshal(map[string]any{
		"manifest": manifest,
		"persist":  false,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}

	errMsg := res["error"]
	if strings.HasPrefix(errMsg, "adopt failed:") {
		t.Errorf("expected error message without 'adopt failed:' prefix, got %q", errMsg)
	}
	if errMsg != "no Composition document found in manifest" {
		t.Errorf("error = %q, want %q", errMsg, "no Composition document found in manifest")
	}
}

func TestAdoptEndpoint_IfMatchStaleRejected(t *testing.T) {
	h, bpPath := testHandlerWithPath(t)

	initialDisk, err := os.ReadFile(bpPath)
	if err != nil {
		t.Fatalf("ReadFile initial blueprint: %v", err)
	}

	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest("GET", "/api/blueprint", nil)
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/blueprint status = %d, want 200", getRec.Code)
	}
	etag := getRec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("empty ETag from GET /api/blueprint")
	}

	manifest := `
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: test-adopted-if-match
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

	reqBody, _ := json.Marshal(map[string]any{
		"manifest": manifest,
		"persist":  true,
		"provider": testProviderRef,
	})

	// 1. Send adopt request with mismatched If-Match header.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	req.Header.Set("If-Match", `"mismatched-etag"`)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412 Precondition Failed, body: %s", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	wantMsg := "precondition failed: If-Match header does not match current blueprint revision"
	if !strings.Contains(errResp["error"], wantMsg) {
		t.Errorf("error = %q, want it to contain %q", errResp["error"], wantMsg)
	}

	// Verify disk was not modified.
	diskAfterStale, err := os.ReadFile(bpPath)
	if err != nil {
		t.Fatalf("ReadFile bpPath after stale adopt: %v", err)
	}
	if !bytes.Equal(initialDisk, diskAfterStale) {
		t.Fatalf("blueprint on disk was modified despite stale If-Match")
	}

	// 2. Send adopt request with matching If-Match header.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/blueprint/adopt", bytes.NewReader(reqBody))
	req.Header.Set("If-Match", etag)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK with matching ETag, body: %s", rec.Code, rec.Body.String())
	}
	var okResp adoptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &okResp); err != nil {
		t.Fatalf("unmarshal adopt response: %v", err)
	}
	if !okResp.Persisted {
		t.Fatalf("expected persisted = true")
	}
	if okResp.Blueprint.Metadata.Name != "test-adopted-if-match" {
		t.Errorf("name = %q, want test-adopted-if-match", okResp.Blueprint.Metadata.Name)
	}

	// Verify disk was updated.
	diskAfterMatch, err := os.ReadFile(bpPath)
	if err != nil {
		t.Fatalf("ReadFile bpPath after matching adopt: %v", err)
	}
	if bytes.Equal(initialDisk, diskAfterMatch) {
		t.Fatalf("blueprint on disk was not updated after successful persist")
	}
}
