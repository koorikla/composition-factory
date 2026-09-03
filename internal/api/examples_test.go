package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestGetExamples(t *testing.T) {
	h := testHandler(t)
	rec := do(t, h, "GET", "/api/examples", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/examples code = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Examples []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Description   string   `json:"description"`
			Tags          []string `json:"tags"`
			ResourceCount int      `json:"resourceCount"`
			Sources       []string `json:"sources"`
			YAML          string   `json:"yaml"`
		} `json:"examples"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(body.Examples) < 3 {
		t.Errorf("got %d examples, want at least 3", len(body.Examples))
	}

	foundIRSA := false
	foundRDS := false
	foundApp := false
	for _, ex := range body.Examples {
		if ex.ID == "irsa" {
			foundIRSA = true
		}
		if ex.ID == "rds-postgres" {
			foundRDS = true
		}
		if ex.ID == "k8s-app" {
			foundApp = true
		}
		if ex.YAML == "" {
			t.Errorf("example %q has empty YAML", ex.ID)
		}
		if ex.ResourceCount == 0 {
			t.Errorf("example %q has ResourceCount 0", ex.ID)
		}
	}

	if !foundIRSA || !foundRDS || !foundApp {
		t.Errorf("missing expected examples: foundIRSA=%v, foundRDS=%v, foundApp=%v", foundIRSA, foundRDS, foundApp)
	}
}

func TestGetExampleByID(t *testing.T) {
	h := testHandler(t)

	rec := do(t, h, "GET", "/api/examples/irsa", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/examples/irsa code = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Example struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			YAML string `json:"yaml"`
		} `json:"example"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if body.Example.ID != "irsa" {
		t.Errorf("example ID = %q, want \"irsa\"", body.Example.ID)
	}

	// 404 for unknown
	rec404 := do(t, h, "GET", "/api/examples/unknown-xyz", "")
	if rec404.Code != http.StatusNotFound {
		t.Errorf("GET /api/examples/unknown-xyz code = %d, want %d", rec404.Code, http.StatusNotFound)
	}
}

func TestLoadExampleWithProviderAutoSync(t *testing.T) {
	h, _, store, _ := testServerParts(t)

	// Seed RDS provider in test cache to isolate test from network
	rdsRef := "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0"
	if err := store.Save(&xpkg.Package{Ref: rdsRef, Digest: "sha256:rds-test"}, testGenerateFixtureCRDs(t)); err != nil {
		t.Fatalf("seed rds cache: %v", err)
	}

	// Load RDS example
	rec := do(t, h, "POST", "/api/examples/rds-postgres/load", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/examples/rds-postgres/load code = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var loadedDoc struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Sources []struct {
				Provider string `json:"provider"`
			} `json:"sources"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loadedDoc); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if loadedDoc.Metadata.Name != "xpostgres" {
		t.Errorf("loadedDoc name = %q, want \"xpostgres\"", loadedDoc.Metadata.Name)
	}

	// Verify provider was registered in /api/providers
	recProv := do(t, h, "GET", "/api/providers", "")
	if recProv.Code != http.StatusOK {
		t.Fatalf("GET /api/providers code = %d", recProv.Code)
	}
	if !strings.Contains(recProv.Body.String(), "provider-aws-rds") {
		t.Errorf("GET /api/providers does not contain provider-aws-rds: %s", recProv.Body.String())
	}
}

// The chooser opens itself on a blank document, which on a fresh install is
// also a cold cache. An example whose provider schemas are not cached costs a
// download and fails outright offline, so the list has to say which cards load
// right now -- and lead with them.
func TestExamplesReportProviderReadiness(t *testing.T) {
	h := testHandler(t)
	rec := do(t, h, "GET", "/api/examples", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/examples code = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Examples []struct {
			ID             string   `json:"id"`
			Sources        []string `json:"sources"`
			SourcesReady   bool     `json:"sourcesReady"`
			MissingSources []string `json:"missingSources"`
		} `json:"examples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(body.Examples) == 0 {
		t.Fatal("no examples returned")
	}

	// Ready ones come first, so a first run's eye lands on something that works.
	seenNotReady := false
	for _, ex := range body.Examples {
		if !ex.SourcesReady {
			seenNotReady = true
			continue
		}
		if seenNotReady {
			t.Errorf("ready example %q sorted after a not-ready one", ex.ID)
		}
	}

	for _, ex := range body.Examples {
		// An example with no sources needs nothing fetched: always ready.
		if len(ex.Sources) == 0 {
			if !ex.SourcesReady {
				t.Errorf("example %q has no sources but is not ready", ex.ID)
			}
			if len(ex.MissingSources) != 0 {
				t.Errorf("example %q has no sources but reports missing %v", ex.ID, ex.MissingSources)
			}
			continue
		}
		// Otherwise readiness and the missing list must agree with each other.
		if ex.SourcesReady && len(ex.MissingSources) != 0 {
			t.Errorf("example %q is ready but reports missing %v", ex.ID, ex.MissingSources)
		}
		if !ex.SourcesReady && len(ex.MissingSources) == 0 {
			t.Errorf("example %q is not ready but names nothing missing", ex.ID)
		}
		// Everything named missing must be one of the example's own sources.
		for _, m := range ex.MissingSources {
			found := false
			for _, s := range ex.Sources {
				if s == m {
					found = true
				}
			}
			if !found {
				t.Errorf("example %q reports missing %q, not one of its sources %v", ex.ID, m, ex.Sources)
			}
		}
	}

	// This handler's store is empty, so every example that names a provider
	// must be reported as needing a download -- the cold-start case.
	for _, ex := range body.Examples {
		if len(ex.Sources) > 0 && ex.SourcesReady {
			t.Errorf("example %q claims ready against an empty cache", ex.ID)
		}
	}
}
