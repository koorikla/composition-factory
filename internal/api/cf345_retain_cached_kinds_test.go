package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
)

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			in, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			out, err := os.Create(dstPath)
			if err != nil {
				in.Close()
				return err
			}
			_, err = io.Copy(out, in)
			in.Close()
			out.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// TestCF345ServeRetainsCachedKindsOnSave verifies CF-345 (issue #235):
// cf serve loads every cached provider at startup on a blank blueprint.
// Saving the document (PUT unchanged doc, adding a native kind, or adding a provider kind)
// must NOT drop cached provider kinds from the palette or from SOURCES.
func TestCF345ServeRetainsCachedKindsOnSave(t *testing.T) {
	fixtureCache := filepath.Join("..", "..", "tests", "fixtures", "cache")
	tempCache := filepath.Join(t.TempDir(), "cache")
	if err := copyDir(fixtureCache, tempCache); err != nil {
		t.Fatalf("copy fixture cache: %v", err)
	}

	bpDir := t.TempDir()
	bpPath := filepath.Join(bpDir, "doc.cf.yaml")
	const blankBlueprint = `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: untitled
spec:
  sources: []
  xrd:
    group: platform.example.org
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName:
        type: string
        required: true
        description: ProviderConfig to reconcile the composed resources against.
  resources: []
`
	if err := os.WriteFile(bpPath, []byte(blankBlueprint), 0o644); err != nil {
		t.Fatalf("write blank blueprint: %v", err)
	}

	bp, err := blueprint.Load(bpPath)
	if err != nil {
		t.Fatalf("load blank blueprint: %v", err)
	}

	store := cache.New(tempCache)
	cachedRefs, err := store.List()
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(cachedRefs) != 2 {
		t.Fatalf("expected 2 cached providers, got %d: %v", len(cachedRefs), cachedRefs)
	}

	idx, err := BuildIndex(store, cachedRefs, bp, bpDir)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	h, err := New(Options{
		Index:     idx,
		Store:     store,
		Blueprint: bpPath,
		Providers: cachedRefs,
		OutDir:    t.TempDir(),
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 1. Verify startup state: 74 kinds and 2 providers
	var kindsResp struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds startup status = %d", code)
	}
	if len(kindsResp.Kinds) != 74 {
		t.Fatalf("startup kinds = %d, want 74", len(kindsResp.Kinds))
	}

	var provsResp struct{ Providers []providerEntry }
	if code := getJSON(t, h, "/api/providers", &provsResp); code != http.StatusOK {
		t.Fatalf("GET /api/providers startup status = %d", code)
	}
	if len(provsResp.Providers) != 2 {
		t.Fatalf("startup providers = %d, want 2", len(provsResp.Providers))
	}

	// 2. PUT unchanged document back (literal issue #235 repro)
	var curDoc blueprint.Blueprint
	if code := getJSON(t, h, "/api/blueprint", &curDoc); code != http.StatusOK {
		t.Fatalf("GET /api/blueprint status = %d", code)
	}
	docBytes, err := json.Marshal(curDoc)
	if err != nil {
		t.Fatalf("marshal blueprint: %v", err)
	}

	putRec := do(t, h, "PUT", "/api/blueprint", string(docBytes))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint unchanged doc status = %d: %s", putRec.Code, putRec.Body)
	}

	// 3. Assert kinds and providers after saving unchanged document
	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds after PUT status = %d", code)
	}
	if len(kindsResp.Kinds) != 74 {
		t.Fatalf("kinds after PUT unchanged doc = %d, want 74 (cached providers were dropped)", len(kindsResp.Kinds))
	}
	if code := getJSON(t, h, "/api/providers", &provsResp); code != http.StatusOK {
		t.Fatalf("GET /api/providers after PUT status = %d", code)
	}
	if len(provsResp.Providers) != 2 {
		t.Fatalf("providers after PUT unchanged doc = %d, want 2", len(provsResp.Providers))
	}

	// 4. Add native kind (ServiceAccount) which contributes no provider source
	curDoc.Spec.Resources = []blueprint.Resource{
		{
			Name:     "sa",
			Kind:     "ServiceAccount",
			Provider: blueprint.NativeProvider,
			Fields:   map[string]blueprint.Field{},
		},
	}
	docBytes, err = json.Marshal(curDoc)
	if err != nil {
		t.Fatalf("marshal blueprint with ServiceAccount: %v", err)
	}
	putRec = do(t, h, "PUT", "/api/blueprint", string(docBytes))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint with ServiceAccount status = %d: %s", putRec.Code, putRec.Body)
	}

	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds after native kind PUT status = %d", code)
	}
	if len(kindsResp.Kinds) != 74 {
		t.Fatalf("kinds after adding ServiceAccount = %d, want 74", len(kindsResp.Kinds))
	}

	// 5. Add provider kind (Bucket from provider-aws-s3)
	s3Ref := "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"
	curDoc.Spec.Sources = []blueprint.Source{{Provider: s3Ref}}
	curDoc.Spec.Resources = append(curDoc.Spec.Resources, blueprint.Resource{
		Name:     "bucket",
		Kind:     "Bucket",
		Provider: s3Ref,
		Fields:   map[string]blueprint.Field{},
	})
	docBytes, err = json.Marshal(curDoc)
	if err != nil {
		t.Fatalf("marshal blueprint with Bucket: %v", err)
	}
	putRec = do(t, h, "PUT", "/api/blueprint", string(docBytes))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint with Bucket status = %d: %s", putRec.Code, putRec.Body)
	}

	// All 74 kinds must still be available (Queue from provider-aws-sqs must not vanish)
	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds after adding Bucket status = %d", code)
	}
	if len(kindsResp.Kinds) != 74 {
		t.Fatalf("kinds after adding Bucket = %d, want 74", len(kindsResp.Kinds))
	}

	// 6. Explicitly delete provider-aws-sqs via DELETE /api/providers/{ref}
	sqsRef := "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"
	delRec := do(t, h, "DELETE", "/api/providers/"+url.PathEscape(sqsRef), "")
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/providers status = %d: %s", delRec.Code, delRec.Body)
	}

	// Now kinds drops by 8 (to 66) and providers to 1
	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds after DELETE status = %d", code)
	}
	if len(kindsResp.Kinds) != 66 {
		t.Fatalf("kinds after DELETE = %d, want 66", len(kindsResp.Kinds))
	}
	if code := getJSON(t, h, "/api/providers", &provsResp); code != http.StatusOK {
		t.Fatalf("GET /api/providers after DELETE status = %d", code)
	}
	if len(provsResp.Providers) != 1 || provsResp.Providers[0].Ref != s3Ref {
		t.Fatalf("providers after DELETE = %+v, want [%s]", provsResp.Providers, s3Ref)
	}

	// 7. Remove Bucket resource and sources (back to sources: []), verify provider-aws-s3 is STILL retained
	curDoc.Spec.Sources = []blueprint.Source{}
	curDoc.Spec.Resources = []blueprint.Resource{}
	docBytes, err = json.Marshal(curDoc)
	if err != nil {
		t.Fatalf("marshal blueprint with empty sources: %v", err)
	}
	putRec = do(t, h, "PUT", "/api/blueprint", string(docBytes))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/blueprint empty sources status = %d: %s", putRec.Code, putRec.Body)
	}

	if code := getJSON(t, h, "/api/kinds", &kindsResp); code != http.StatusOK {
		t.Fatalf("GET /api/kinds after removing bucket status = %d", code)
	}
	if len(kindsResp.Kinds) != 66 {
		t.Fatalf("kinds after removing bucket = %d, want 66", len(kindsResp.Kinds))
	}
	if code := getJSON(t, h, "/api/providers", &provsResp); code != http.StatusOK {
		t.Fatalf("GET /api/providers after removing bucket status = %d", code)
	}
	if len(provsResp.Providers) != 1 || provsResp.Providers[0].Ref != s3Ref {
		t.Fatalf("providers after removing bucket = %+v, want [%s]", provsResp.Providers, s3Ref)
	}
}
