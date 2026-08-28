package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestGetBlueprintReturnsJSON(t *testing.T) {
	rec := do(t, testHandler(t), "GET", "/api/blueprint", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var b blueprint.Blueprint
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if b.Spec.XRD.Kind == "" {
		t.Error("blueprint came back empty")
	}
}

// The file on disk is the source of truth: an edit that is not persisted
// would diverge from what `cf gen` reads.
func TestAddParameterPersistsToDisk(t *testing.T) {
	h, path := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"location","parameter":{"type":"string","required":true,"enum":["EU","US"]}}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("blueprint on disk no longer loads: %v", err)
	}
	if _, ok := reloaded.Spec.XRD.Parameters["location"]; !ok {
		t.Error("parameter was not persisted to the file")
	}
}

func TestInvalidEditIs400AndLeavesTheFileUntouched(t *testing.T) {
	h, path := testHandlerWithPath(t)
	before, _ := os.ReadFile(path)
	rec := do(t, h, "POST", "/api/blueprint/parameters",
		`{"name":"not a valid name","parameter":{"type":"string"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a valid name") {
		t.Errorf("error does not name the offending input: %s", rec.Body)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("the blueprint file changed despite a rejected edit")
	}
}

func TestRenameRewritesReferencesOnDisk(t *testing.T) {
	h, path := testHandlerWithPath(t)
	if rec := do(t, h, "POST", "/api/blueprint/parameters/maxMessageSize/rename",
		`{"to":"maxBytes"}`); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	reloaded, err := blueprint.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Spec.Resources[0].Fields["maxMessageSize"].From; got != "params.maxBytes" {
		t.Errorf("reference on disk = %q, want params.maxBytes", got)
	}
}

func TestDeleteReferencedParameterIs409(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	rec := do(t, h, "DELETE", "/api/blueprint/parameters/maxMessageSize", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 — the parameter is still referenced, which is a "+
			"conflict with current state rather than a malformed request", rec.Code)
	}
}

func TestMalformedJSONBodyIs400(t *testing.T) {
	h, _ := testHandlerWithPath(t)
	if rec := do(t, h, "POST", "/api/blueprint/parameters", `{"name":`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The whole architecture rests on this: the API must not have its own emitter.
func TestGenerateProducesTheSameBytesAsTheEngine(t *testing.T) {
	h, path := testHandlerWithPath(t)
	rec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Outputs []struct {
			Path  string `json:"path"`
			Bytes int    `json:"bytes"`
		}
		Written bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(got.Outputs) != 3 {
		t.Fatalf("got %d outputs, want 3 (xrd, composition, functions.yaml)", len(got.Outputs))
	}
	if got.Written {
		t.Error("write:false still reported Written")
	}
	_ = path
}

func TestGenerateSurfacesValidationErrorsAsIs(t *testing.T) {
	h, path := testHandlerWithPath(t)
	// Corrupt the blueprint on disk behind the server's back.
	body, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(body, []byte("scope: Namespaced"), []byte("scope: Cluster"), 1), 0o644)
	rec := do(t, h, "POST", "/api/generate", `{"write":false}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Cluster") {
		t.Errorf("the engine's own error was not surfaced: %s", rec.Body)
	}
}

// --- Additional coverage beyond the brief's verbatim tests ---
//
// Not from the brief's Step 1 listing. The brief's prose (distinct from its
// verbatim tests) requires persistence to be byte-stable: "two consecutive
// identical edits produce identical bytes." None of the tests above compare
// raw bytes across two independent runs of the same edit, so this pins that
// requirement directly: the same POST against two freshly-built, identical
// starting blueprints must leave byte-for-byte identical files on disk —
// not just semantically-equal YAML that happens to format differently run
// to run.
func TestConsecutiveIdenticalEditsProduceIdenticalBytes(t *testing.T) {
	h1, path1 := testHandlerWithPath(t)
	h2, path2 := testHandlerWithPath(t)

	const editBody = `{"name":"location","parameter":{"type":"string","required":true,"enum":["EU","US"]}}`
	for _, run := range []struct {
		h    http.Handler
		path string
	}{{h1, path1}, {h2, path2}} {
		if rec := do(t, run.h, "POST", "/api/blueprint/parameters", editBody); rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}

	got1, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read %s: %v", path1, err)
	}
	got2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("read %s: %v", path2, err)
	}
	if !bytes.Equal(got1, got2) {
		t.Errorf("two independent runs of the identical edit produced different bytes:\n"+
			"--- run 1 (%s) ---\n%s\n--- run 2 (%s) ---\n%s", path1, got1, path2, got2)
	}
}
