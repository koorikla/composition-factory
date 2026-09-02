package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		"provider": "xpkg.upbound.io/upbound/provider-aws-sqs:v1.14.0",
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
	if bpPath == "" {
		t.Fatal("empty bpPath")
	}
}
