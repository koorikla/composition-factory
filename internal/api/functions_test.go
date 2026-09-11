package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
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

func TestAddFunctionRefusesProviderPackageWithoutPinningLock(t *testing.T) {
	t.Run("fetched provider", func(t *testing.T) {
		const providerRef = "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0"
		h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
			return &xpkg.Package{
				Ref:    ref,
				Digest: "sha256:rdsdigest",
				Docs: [][]byte{
					managedCRDDoc("rds.aws.upbound.io", "Instance", "instances"),
				},
			}, nil
		})

		rec := do(t, h, "POST", "/api/functions", `{"ref":"`+providerRef+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		want := fmt.Sprintf("package %q is a provider package, not a function (use 'cf provider add %s')", providerRef, providerRef)
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if resp["error"] != want {
			t.Errorf("error = %q, want %q", resp["error"], want)
		}

		l, err := cache.ReadLock(o.Lock)
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		if _, ok := l.FindProvider(providerRef); ok {
			t.Errorf("provider %q was pinned into lockfile despite 400 refusal", providerRef)
		}
		if _, ok := l.FindFunction(providerRef); ok {
			t.Errorf("provider %q was pinned as function into lockfile despite 400 refusal", providerRef)
		}
	})

	t.Run("cached provider", func(t *testing.T) {
		const providerRef = "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"
		h, o := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
			t.Fatalf("unexpected network fetch in test for %s", ref)
			return nil, fmt.Errorf("unexpected fetch")
		})

		crds := []schema.CRD{{
			Group: "s3.aws.upbound.io", Kind: "Bucket", Plural: "buckets",
			Categories: []string{"managed"},
		}}
		if err := o.Store.Save(&xpkg.Package{Ref: providerRef, Digest: "sha256:s3cached"}, crds); err != nil {
			t.Fatalf("Save: %v", err)
		}

		rec := do(t, h, "POST", "/api/functions", `{"ref":"`+providerRef+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		want := fmt.Sprintf("package %q is a provider package, not a function (use 'cf provider add %s')", providerRef, providerRef)
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if resp["error"] != want {
			t.Errorf("error = %q, want %q", resp["error"], want)
		}

		l, err := cache.ReadLock(o.Lock)
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		if _, ok := l.FindProvider(providerRef); ok {
			t.Errorf("provider %q was pinned into lockfile despite 400 refusal", providerRef)
		}
		if _, ok := l.FindFunction(providerRef); ok {
			t.Errorf("provider %q was pinned as function into lockfile despite 400 refusal", providerRef)
		}
	})
}
