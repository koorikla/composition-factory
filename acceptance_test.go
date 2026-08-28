package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// providerRef is the OSS provider used for the end-to-end acceptance run. It
// is pulled from ghcr.io (the OSS publisher, anonymous version enumeration)
// rather than xpkg.upbound.io: at matching versions the CRDs are byte
// identical (same upjet build, same .m. namespaced groups), and ghcr is this
// project's stated preference.
const providerRef = "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"

// requireTool skips the test when a binary or daemon is unavailable, so Lane A
// stays green on runners without Docker.
func requireTool(t *testing.T, name string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
	if len(args) > 0 {
		if err := exec.Command(name, args...).Run(); err != nil {
			t.Skipf("%s %v failed: %v", name, args, err)
		}
	}
}

func TestAcceptanceXQueueRenders(t *testing.T) {
	if testing.Short() {
		t.Skip("acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()

	// The compiled binary deliberately does NOT go in t.TempDir(): on at
	// least one dev sandbox, outbound network is gated on the executable's
	// own path, not on which process launched it — a binary built under a
	// temp dir times out reaching ghcr.io while the identical binary built
	// under the repo's bin/ (as `make build` already does) reaches it fine.
	// bin/ is gitignored, so this leaves the tree clean, and rebuilding a
	// ~14MB binary on every run is cheap. If you're tempted to "tidy" this
	// back into t.TempDir(): don't — that's what makes this test start
	// timing out on network fetches for no visible reason. The name
	// cf-acceptance (not cf) avoids colliding with a developer's own
	// `make build` output sitting in the same directory.
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	bin := filepath.Join(repoRoot, "bin", "cf-acceptance")
	if out, err := exec.Command("go", "build", "-o", bin, "./cmd/cf").CombinedOutput(); err != nil {
		t.Fatalf("build cf: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(dir, "cache")
	lock := filepath.Join(dir, ".cf.lock")

	// Step 1: fetch the provider. No cluster, no Docker.
	add := exec.Command(bin, "provider", "add", providerRef, "--cache-dir", cacheDir, "--lock", lock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("cf provider add: %v\n%s", err, out)
	}

	// Step 2: generate.
	outDir := filepath.Join(dir, "out")
	gen := exec.Command(bin, "gen", "testdata/xqueue.cf.yaml", "-o", outDir, "--cache-dir", cacheDir)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cf gen: %v\n%s", err, out)
	}

	// Step 3: --check must report in sync immediately after generating.
	chk := exec.Command(bin, "gen", "testdata/xqueue.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	// Step 4: render what we generated.
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	render := exec.Command("crossplane", "composition", "render",
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	rendered, err := render.CombinedOutput()
	if err != nil {
		t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
	}
	got := string(rendered)

	for _, want := range []string{
		"apiVersion: sqs.aws.m.upbound.io/v1beta1",
		"kind: Queue",
		"maxMessageSize: 2048",
		"region: eu-north-1",
		"kind: ClusterProviderConfig",
		"name: localstack",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, got)
		}
	}

	// The defect class that passes every other gate: a legal string that is
	// wrong. A missing field renders "<no value>" (or, via other template
	// paths, "<nil>") into a live managed resource, and because both are
	// legal YAML scalars the whole validate -> render -> validate pipeline
	// still exits 0.
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(got, bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}

	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile("testdata/xqueue.render.golden.yaml", rendered, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
	}
}
