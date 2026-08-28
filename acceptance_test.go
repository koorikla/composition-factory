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

	// Step 4: render what we generated — first WITHOUT observed state. The
	// status wire's source (main-queue's status.atProvider.url) does not
	// exist yet, exactly like a first reconcile, so the wired field must be
	// CLEANLY ABSENT: the QueuePolicy still renders, queueUrl is omitted, no
	// "<no value>", no render error. Crossplane fills it on a later
	// reconcile once the Queue is observed.
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
		"kind: QueuePolicy",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "queueUrl") {
		t.Errorf("queueUrl must be absent while main-queue is unobserved — the status wire's "+
			"guard failed open\n---\n%s", got)
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

	// Step 5: render again WITH observed state for main-queue. Now the wire
	// has a source, and the observed URL must flow into the QueuePolicy's
	// forProvider.queueUrl — the fixture's value, verbatim.
	renderObserved := exec.Command("crossplane", "composition", "render",
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m",
		"--observed-resources", "testdata/observed-queue.yaml")
	renderedObserved, err := renderObserved.CombinedOutput()
	if err != nil {
		t.Fatalf("crossplane composition render --observed-resources: %v\n%s", err, renderedObserved)
	}
	gotObserved := string(renderedObserved)
	const wiredURL = "queueUrl: https://sqs.eu-north-1.amazonaws.com/000000000000/demo-main-queue"
	if !strings.Contains(gotObserved, wiredURL) {
		t.Errorf("rendered output missing %q — the observed status value did not flow across "+
			"the wire\n---\n%s", wiredURL, gotObserved)
	}
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(gotObserved, bad) {
			t.Errorf("observed render contains %q\n---\n%s", bad, gotObserved)
		}
	}

	if os.Getenv("CF_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile("testdata/xqueue.render.golden.yaml", rendered, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
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
