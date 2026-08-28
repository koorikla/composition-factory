package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/koorikla/compositionfactory/internal/index"
)

func getJSON(t *testing.T, h http.Handler, path string, into any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code == http.StatusOK && into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: body is not JSON: %v\n%s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestListKindsReturnsTheIndex(t *testing.T) {
	var got struct{ Kinds []index.Kind }
	if code := getJSON(t, testHandler(t), "/api/kinds", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if len(got.Kinds) != 2 {
		t.Fatalf("got %d kinds, want 2 (namespaced and cluster-scoped Queue)", len(got.Kinds))
	}
	for _, k := range got.Kinds {
		if k.APIVersion == "" || k.Kind == "" {
			t.Errorf("kind is missing identity fields: %+v", k)
		}
	}
}

func TestListKindsSearchAndLimit(t *testing.T) {
	h := testHandler(t)
	var got struct{ Kinds []index.Kind }
	getJSON(t, h, "/api/kinds?q=sqs.aws.m", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("q=sqs.aws.m returned %d, want 1", len(got.Kinds))
	}
	got.Kinds = nil
	getJSON(t, h, "/api/kinds?q=queue&limit=1", &got)
	if len(got.Kinds) != 1 {
		t.Errorf("limit=1 returned %d", len(got.Kinds))
	}
}

func TestGetKindReturnsIdentityAndEnvelope(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	var got struct {
		Kind     index.Kind
		Envelope []index.Field
	}
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Queue", &got); code != 200 {
		t.Fatalf("status %d", code)
	}
	if got.Kind.Kind != "Queue" || !got.Kind.Namespaced {
		t.Errorf("kind = %+v, want the namespaced Queue", got.Kind)
	}
}

func TestGetUnknownKindIs404WithJSON(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	if code := getJSON(t, testHandler(t), "/api/kinds/"+esc+"/Nonexistent", nil); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestFieldsHonoursRequiredOnly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	var all, req struct {
		Fields []index.Field
		Total  int
	}
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields", &all)
	getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields?required_only=true", &req)
	if len(all.Fields) != 2 {
		t.Errorf("all fields = %d, want 2 (region, tags)", len(all.Fields))
	}
	if len(req.Fields) != 1 || req.Fields[0].Path != "region" {
		t.Errorf("required_only = %+v, want just region", req.Fields)
	}
	if req.Total != 1 {
		t.Errorf("total = %d, want it to count the returned set", req.Total)
	}
}

func TestFieldsRejectsBadQueryParamsLoudly(t *testing.T) {
	esc := url.PathEscape("sqs.aws.m.upbound.io/v1beta1")
	h := testHandler(t)
	for _, q := range []string{"?max_depth=abc", "?limit=-x", "?required_only=maybe"} {
		if code := getJSON(t, h, "/api/kinds/"+esc+"/Queue/fields"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s -> status %d, want 400; silently ignoring a malformed filter would "+
				"return the wrong field set with no signal", q, code)
		}
	}
}
