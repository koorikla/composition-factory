package examples_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/examples"
	"github.com/koorikla/compositionfactory/internal/rendertest"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

const requireEnv = "CF_REQUIRE_ACCEPTANCE"

type reporter interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

func unavailable(t reporter, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireEnv) == "1" {
		t.Fatalf(requireEnv+"=1 but a prerequisite is missing: "+format, args...)
		return
	}
	t.Skipf(format, args...)
}

func requireTool(t *testing.T, name string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		unavailable(t, "%s not installed", name)
	}
	if len(args) > 0 {
		if err := exec.Command(name, args...).Run(); err != nil {
			unavailable(t, "%s %v failed: %v", name, args, err)
		}
	}
}

// renderComposition executes `crossplane composition render` serialized by
// rendertest.Lock, with a single retry for the pinned docker container/network race.
func renderComposition(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	release := rendertest.Lock(t)
	defer release()
	full := append([]string{"composition", "render"}, args...)
	rendered, err := exec.Command("crossplane", full...).CombinedOutput()
	if err != nil && bytes.Contains(rendered, []byte("is not connected to Docker network")) {
		t.Logf("retrying render once after the pinned-container/network race:\n%s", rendered)
		_ = exec.Command("docker", "rm", "-f", "cf-function-go-templating", "cf-function-auto-ready").Run()
		rendered, err = exec.Command("crossplane", full...).CombinedOutput()
	}
	return rendered, err
}

func TestAcceptanceAllStarterExamplesRender(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	store := cache.New(cache.DefaultRoot())
	nativeKinds, err := k8s.Kinds()
	if err != nil {
		t.Fatalf("failed to load native k8s kinds: %v", err)
	}

	exs := examples.All()
	if len(exs) == 0 {
		t.Fatal("no starter examples found")
	}

	// Warm cache for any missing provider sources across all examples
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, ex := range exs {
		for _, src := range ex.Sources {
			if _, err := store.Load(src); err != nil {
				t.Logf("fetching provider schema for %s into cache: %v", src, err)
				lockPath := filepath.Join(cache.DefaultRoot(), ".cf.lock")
				if _, _, err := store.FetchAndSave(ctx, lockPath, src, nil); err != nil {
					unavailable(t, "failed to fetch provider schema %s: %v", src, err)
				}
			}
		}
	}

	for _, ex := range exs {
		t.Run(ex.ID, func(t *testing.T) {
			b, err := blueprint.Parse([]byte(ex.YAML))
			if err != nil {
				t.Fatalf("failed to parse blueprint YAML: %v", err)
			}
			if err := b.Validate(); err != nil {
				t.Fatalf("blueprint validation failed: %v", err)
			}

			// Resolve CRDs from cache store + native kinds
			crds, err := cache.LoadSources(store, b, "")
			if err != nil {
				t.Fatalf("failed to load sources: %v", err)
			}
			crds = append(crds, nativeKinds...)

			// Generate artifacts (Composition, XRD, functions.yaml, ProviderConfig)
			tempDir := t.TempDir()
			outputs, err := emit.Generate(b, crds, tempDir)
			if err != nil {
				t.Fatalf("emit.Generate failed: %v", err)
			}

			var compPath, fnsPath, xrdPath string
			for _, o := range outputs {
				if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", filepath.Dir(o.Path), err)
				}
				if err := os.WriteFile(o.Path, o.Body, 0o644); err != nil {
					t.Fatalf("write file %s: %v", o.Path, err)
				}
				switch {
				case filepath.Base(filepath.Dir(o.Path)) == "compositions":
					compPath = o.Path
				case filepath.Base(filepath.Dir(o.Path)) == "xrds":
					xrdPath = o.Path
				case filepath.Base(o.Path) == "functions.yaml":
					fnsPath = o.Path
				}
			}

			if compPath == "" {
				t.Fatal("no composition generated")
			}
			if fnsPath == "" {
				t.Fatal("no functions.yaml generated")
			}
			if xrdPath == "" {
				t.Fatal("no XRD generated")
			}

			// Synthesize sample XR
			xrBytes, err := emit.SampleXR(b)
			if err != nil {
				t.Fatalf("emit.SampleXR failed: %v", err)
			}
			xrPath := filepath.Join(tempDir, "xr.yaml")
			if err := os.WriteFile(xrPath, xrBytes, 0o644); err != nil {
				t.Fatalf("write xr.yaml: %v", err)
			}

			// Render composition using crossplane composition render
			rendered, err := renderComposition(t, xrPath, compPath, fnsPath, "--xrd", xrdPath, "--timeout", "5m")
			if err != nil {
				t.Fatalf("crossplane composition render failed: %v\n%s", err, rendered)
			}

			got := string(rendered)

			// Assert render succeeds and contains no "<no value>" or render errors
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(got, bad) {
					t.Errorf("rendered output contains %q — a missing field reached a live resource shape\n---\n%s", bad, got)
				}
			}

			// Also validate rendered resources against CRD schemas
			if err := emit.ValidateRenderedWithBlueprint(rendered, crds, b); err != nil {
				t.Errorf("ValidateRenderedWithBlueprint failed: %v\n---\n%s", err, got)
			}
		})
	}
}

// TestAllStarterExamplesRender is an alias ensuring compliance with the verbatim brief function name.
func TestAllStarterExamplesRender(t *testing.T) {
	TestAcceptanceAllStarterExamplesRender(t)
}
