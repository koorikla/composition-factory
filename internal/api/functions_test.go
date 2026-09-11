package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

// TestListFunctionsReturnsEmptyArrayWhenNoFunctions pins the CF-213 contract:
// when no function packages are pinned in the lockfile (or no lockfile exists),
// GET /api/functions must return {"functions":[]} rather than {"functions":null},
// matching the normalization behavior of sibling list routes (/api/kinds, /api/providers).
func TestListFunctionsReturnsEmptyArrayWhenNoFunctions(t *testing.T) {
	t.Run("missing lockfile", func(t *testing.T) {
		h := testHandler(t)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/functions", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := strings.TrimSpace(rec.Body.String())
		const want = `{"functions":[]}`
		if body != want {
			t.Fatalf("body = %q, want %q", body, want)
		}

		var resp struct {
			Functions []functionEntry `json:"functions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Functions == nil {
			t.Errorf("Functions slice is nil, want non-nil empty slice")
		}
	})

	t.Run("empty lockfile", func(t *testing.T) {
		_, _, store, _ := testServerParts(t)
		lockPath := filepath.Join(t.TempDir(), ".cf.lock")
		if err := os.WriteFile(lockPath, []byte(`{"providers":[]}`), 0o644); err != nil {
			t.Fatalf("write lock: %v", err)
		}

		srv := &server{
			Store:     store,
			Lock:      lockPath,
			Blueprint: testBlueprintPath(t),
		}
		rec := httptest.NewRecorder()
		srv.handleListFunctions(rec, httptest.NewRequest("GET", "/api/functions", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := strings.TrimSpace(rec.Body.String())
		const want = `{"functions":[]}`
		if body != want {
			t.Fatalf("body = %q, want %q", body, want)
		}

		var resp struct {
			Functions []functionEntry `json:"functions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Functions == nil {
			t.Errorf("Functions slice is nil, want non-nil empty slice")
		}
	})
}

func TestListFunctionsReturnsPopulatedEntries(t *testing.T) {
	_, _, store, _ := testServerParts(t)
	lockPath := filepath.Join(t.TempDir(), ".cf.lock")
	lockContent := `{
		"providers": [],
		"functions": [
			{"ref": "` + testFunctionRef + `", "digest": "sha256:testdigest"}
		]
	}`
	if err := os.WriteFile(lockPath, []byte(lockContent), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	digest := "sha256:testdigest"
	crds := []schema.CRD{{
		Group: "autoready.fn.crossplane.io", Kind: "AutoReady", Plural: "autoreadies",
		Function: true,
	}}
	if err := store.Save(&xpkg.Package{Ref: testFunctionRef, Digest: digest}, crds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	srv := &server{
		Store:     store,
		Lock:      lockPath,
		Blueprint: testBlueprintPath(t),
	}
	rec := httptest.NewRecorder()
	srv.handleListFunctions(rec, httptest.NewRequest("GET", "/api/functions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Functions []functionEntry `json:"functions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Functions) != 1 {
		t.Fatalf("got %d functions, want 1", len(resp.Functions))
	}
	if resp.Functions[0].Ref != testFunctionRef || resp.Functions[0].Digest != digest || resp.Functions[0].Inputs != 1 {
		t.Errorf("unexpected function entry: %+v", resp.Functions[0])
	}
}
