package main_test

import (
	"fmt"
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

// requireEnv, when set to "1", turns every prerequisite skip in this file
// into a failure.
//
// A skip reads as a pass. `go test` prints ok for a package whose only test
// skipped, CI goes green, and the one gate that proves the generated YAML
// actually renders can quietly stop running -- on a runner where the
// crossplane CLI install step silently failed, or where the Docker daemon is
// unavailable, or after a refactor moves the binary. That is a vacuous gate:
// the acceptance test's whole job is to be the thing that would have caught
// what the unit tests cannot, and a green build that never ran it is worse
// than no gate at all, because it is believed.
//
// Lane A (`make test`, -short) still needs to skip on a laptop with no
// Docker, so the behaviour is opt-in rather than default. Lane B in
// .github/workflows/ci.yml sets CF_REQUIRE_ACCEPTANCE=1, which is the
// assertion that the test RAN.
const requireEnv = "CF_REQUIRE_ACCEPTANCE"

// reporter is the slice of *testing.T this file's skip/fail decision needs.
// It exists so that decision is testable without running the acceptance test
// itself -- see TestUnavailableFailsWhenAcceptanceIsRequired.
type reporter interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// unavailable reports a missing prerequisite: a skip normally, a hard
// failure when CF_REQUIRE_ACCEPTANCE=1 says this run must not be vacuous.
func unavailable(t reporter, format string, args ...any) {
	t.Helper()
	if os.Getenv(requireEnv) == "1" {
		t.Fatalf(requireEnv+"=1 but a prerequisite is missing: "+format, args...)
		// testing.T.Fatalf never returns (it calls runtime.Goexit), but the
		// explicit return keeps this correct for any reporter that does --
		// without it, a fatal would fall through and also skip.
		return
	}
	t.Skipf(format, args...)
}

// requireTool skips the test when a binary or daemon is unavailable, so Lane A
// stays green on runners without Docker -- unless CF_REQUIRE_ACCEPTANCE=1.
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

func TestAcceptanceXQueueRenders(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()

	// Build to the repo's bin/cf — the Makefile's own build target — rather
	// than a bespoke name or t.TempDir(). The honest reason: the acceptance
	// gate should exercise the artifact we actually ship, built the way we
	// actually build it ("test what you ship"), not a throwaway copy under a
	// name and location no developer or CI job ever produces. This may
	// overwrite a developer's local `make build` output; that's fine, it's
	// the same program from the same source, and bin/ is gitignored either
	// way. (It also happens to route around a network quirk specific to one
	// dev sandbox, where outbound access is allowlisted per exact executable
	// path rather than by directory or process — bin/cf is the one path
	// that's ever been approved there, because it's what `make build`
	// produces. That is not the reason for this shape; it's just what
	// surfaced the value of testing the real build output instead of an
	// ad-hoc one.)
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	bin := filepath.Join(repoRoot, "bin", "cf")
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

// TestAcceptancePipelineAutoReadyRenders is the blueprint-declared-pipeline
// acceptance gate: a blueprint whose spec.pipeline declares an explicit
// auto-ready step (the overwhelmingly common declared-pipeline case) must
// generate a Composition that the real `crossplane composition render`
// accepts, with function-auto-ready pulled from the OSS registry the
// blueprint names -- gated exactly the way TestAcceptanceXQueueRenders is,
// since both the provider add and the function pull need the network.
//
// Two renders, because a single render cannot show auto-ready doing
// anything: on a first render there are no observed resources, so every
// composed resource is unready with or without the step. The second render
// feeds back an observed main-queue whose own Ready condition is True --
// exactly what a provider would report once the queue exists -- and THAT is
// the input function-auto-ready translates into composed-resource readiness.
// With the step, the XR's Ready condition comes back True/Available; without
// it, the same render leaves Ready False ("Unready resources: main-queue"),
// verified by hand while writing this test.
func TestAcceptancePipelineAutoReadyRenders(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()

	// Build to bin/cf for the same test-what-you-ship reason
	// TestAcceptanceXQueueRenders documents.
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	bin := filepath.Join(repoRoot, "bin", "cf")
	if out, err := exec.Command("go", "build", "-o", bin, "./cmd/cf").CombinedOutput(); err != nil {
		t.Fatalf("build cf: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(dir, "cache")
	lock := filepath.Join(dir, ".cf.lock")

	add := exec.Command(bin, "provider", "add", providerRef, "--cache-dir", cacheDir, "--lock", lock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("cf provider add: %v\n%s", err, out)
	}

	outDir := filepath.Join(dir, "out")
	gen := exec.Command(bin, "gen", "testdata/xqueue-pipeline.cf.yaml", "-o", outDir, "--cache-dir", cacheDir)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cf gen: %v\n%s", err, out)
	}

	chk := exec.Command(bin, "gen", "testdata/xqueue-pipeline.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	// The emitted Composition is pinned byte-for-byte: determinism is a
	// correctness requirement, and this is the one acceptance-level golden
	// for a blueprint-declared pipeline.
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.hooli.tech.yaml")
	compBytes, err := os.ReadFile(comp)
	if err != nil {
		t.Fatalf("read emitted composition: %v", err)
	}
	const golden = "testdata/xqueue-pipeline.composition.golden.yaml"
	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, compBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
	} else {
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read golden: %v (set CF_UPDATE_GOLDEN=1 to create it)", err)
		}
		if string(compBytes) != string(want) {
			t.Errorf("emitted Composition differs from %s\n--- got ---\n%s", golden, compBytes)
		}
	}

	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	// Render 1: no observed state. The pipeline must run end to end -- the
	// declared auto-ready step's package really pulled and executed -- and
	// the XR's Ready condition is False because nothing is observed yet.
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
		"Unready resources: main-queue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, got)
		}
	}
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(got, bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}

	// Render 2: the provider has since reported the queue Ready. auto-ready
	// must translate that observed condition into composed-resource
	// readiness, flipping the XR's own Ready condition to True -- the render
	// without the step leaves it False on identical input.
	observed := filepath.Join(dir, "observed.yaml")
	if err := os.WriteFile(observed, []byte(`apiVersion: sqs.aws.m.upbound.io/v1beta1
kind: Queue
metadata:
  annotations:
    crossplane.io/composition-resource-name: main-queue
  name: demo-9e55b0411c5a
  namespace: default
status:
  conditions:
  - type: Ready
    status: "True"
    reason: Available
    lastTransitionTime: "2024-01-01T00:00:00Z"
`), 0o644); err != nil {
		t.Fatalf("write observed resources: %v", err)
	}
	render2 := exec.Command("crossplane", "composition", "render",
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--observed-resources", observed, "--timeout", "5m")
	rendered2, err := render2.CombinedOutput()
	if err != nil {
		t.Fatalf("crossplane composition render (observed): %v\n%s", err, rendered2)
	}
	got2 := string(rendered2)
	if strings.Contains(got2, "Unready resources") {
		t.Errorf("auto-ready did not mark the observed-Ready queue ready\n---\n%s", got2)
	}
	if !strings.Contains(got2, "type: Ready") || !strings.Contains(got2, "reason: Available") {
		t.Errorf("XR's Ready condition did not become True/Available under auto-ready\n---\n%s", got2)
	}
}

// fakeReporter records which of Skipf/Fatalf unavailable chose, so the
// decision can be tested without a Docker daemon or a crossplane CLI.
type fakeReporter struct {
	skipped, failed bool
	msg             string
}

func (f *fakeReporter) Helper() {}
func (f *fakeReporter) Skipf(format string, args ...any) {
	f.skipped, f.msg = true, fmt.Sprintf(format, args...)
}
func (f *fakeReporter) Fatalf(format string, args ...any) {
	f.failed, f.msg = true, fmt.Sprintf(format, args...)
}

// TestUnavailableFailsWhenAcceptanceIsRequired pins the gate itself. Without
// this behaviour, .github/workflows/ci.yml's Lane B goes green whether or
// not the acceptance test ever ran, because a skip is reported as a pass and
// nothing downstream distinguishes the two.
func TestUnavailableFailsWhenAcceptanceIsRequired(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		wantFatal bool
	}{
		{"unset: a missing tool is a skip, so Lane A stays green without Docker", "", false},
		{"0: still a skip", "0", false},
		{"1: a missing tool is a failure, so a vacuous Lane B cannot pass", "1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(requireEnv, tt.env)
			var f fakeReporter
			unavailable(&f, "crossplane not installed")
			if f.failed != tt.wantFatal || f.skipped == tt.wantFatal {
				t.Fatalf("with %s=%q: failed=%v skipped=%v, want failed=%v",
					requireEnv, tt.env, f.failed, f.skipped, tt.wantFatal)
			}
			if !strings.Contains(f.msg, "crossplane not installed") {
				t.Errorf("message = %q, want it to name the missing prerequisite", f.msg)
			}
		})
	}
}
