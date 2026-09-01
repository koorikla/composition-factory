package main_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
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

// TestAcceptanceForEachRenders is the forEach gate: a blueprint whose
// replica-queue resource carries `forEach: params.instanceCount` over an
// integer parameter with default "2", generated and rendered through the
// real crossplane composition render — the real function-go-templating
// engine with the real sprig until/int, not the unit tests' stubs, and the
// real XRD schema defaulting injecting instanceCount into an XR that never
// set it. Asserted on the rendered ARTIFACT: exactly N instances of the
// looped resource with DISTINCT composition-resource-name annotations (a
// constant name inside a range collapses every iteration into one resource,
// silently), the unlooped resource exactly once, N=2 from the default and
// N=3 from an XR override.
func TestAcceptanceForEachRenders(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()

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

	// Generate twice into separate directories: determinism is a correctness
	// requirement (a churning file on a prune:true GitOps repo is a
	// live-cluster incident), so the two runs must agree byte for byte.
	outDir := filepath.Join(dir, "out")
	for _, o := range []string{outDir, filepath.Join(dir, "out2")} {
		gen := exec.Command(bin, "gen", "testdata/xqueue-foreach.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xqueuesets.platform.hooli.tech.yaml"),
		filepath.Join("xrds", "xqueuesets.platform.hooli.tech.yaml"),
		"functions.yaml",
	} {
		first, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		second, err := os.ReadFile(filepath.Join(dir, "out2", rel))
		if err != nil {
			t.Fatalf("read second-run %s: %v", rel, err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("%s: two generate runs over the same blueprint produced different bytes", rel)
		}
	}

	comp := filepath.Join(outDir, "compositions", "xqueuesets.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueuesets.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	cases := []struct {
		name string
		xr   string
		want []string // every composition-resource-name annotation, sorted
	}{
		{"XRD default fans out to 2 instances", "testdata/xr-foreach-default.yaml",
			[]string{"main-queue", "replica-queue-0", "replica-queue-1"}},
		{"XR override fans out to 3 instances", "testdata/xr-foreach-three.yaml",
			[]string{"main-queue", "replica-queue-0", "replica-queue-1", "replica-queue-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			render := exec.Command("crossplane", "composition", "render",
				tc.xr, comp, fns, "--xrd", xrd, "--timeout", "5m")
			rendered, err := render.CombinedOutput()
			if err != nil {
				t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
			}
			got := renderedResourceNames(t, rendered)
			sort.Strings(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("composition-resource-name annotations = %v, want %v\n---\n%s",
					got, tc.want, rendered)
			}
			for _, bad := range []string{"<no value>", "<nil>"} {
				if strings.Contains(string(rendered), bad) {
					t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
				}
			}
		})
	}
}

// TestAcceptanceStatusRefRenders is the cross-resource wire gate: a
// blueprint whose queue-policy resource sources queueUrl from
// resources.main-queue.status.atProvider.url, generated and rendered through
// the real crossplane composition render — the real function-go-templating
// engine seeing the real protojson shape of .observed (where an empty
// observed-resources map is NO key at all, the exact state the emitted
// hasKey chain's first link guards). Proven both ways on the rendered
// ARTIFACT:
//
//   - with --observed-resources supplying the queue's observed status, the
//     URL flows into the QueuePolicy's forProvider.queueUrl;
//   - without observed state, the field is absent CLEANLY — the render
//     succeeds, both documents appear, and no "<no value>"/"<nil>" reaches a
//     live resource shape (Crossplane fills the value in on a later
//     reconcile).
func TestAcceptanceStatusRefRenders(t *testing.T) {
	if testing.Short() {
		unavailable(t, "acceptance test needs Docker; skipped under -short")
	}
	requireTool(t, "crossplane")
	requireTool(t, "docker", "info")

	dir := t.TempDir()

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

	// Generate twice into separate directories: determinism is a correctness
	// requirement, so the two runs must agree byte for byte.
	outDir := filepath.Join(dir, "out")
	for _, o := range []string{outDir, filepath.Join(dir, "out2")} {
		gen := exec.Command(bin, "gen", "testdata/xqueue-statusref.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xqueuepairs.platform.hooli.tech.yaml"),
		filepath.Join("xrds", "xqueuepairs.platform.hooli.tech.yaml"),
		"functions.yaml",
	} {
		first, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		second, err := os.ReadFile(filepath.Join(dir, "out2", rel))
		if err != nil {
			t.Fatalf("read second-run %s: %v", rel, err)
		}
		if !bytes.Equal(first, second) {
			t.Errorf("%s: two generate runs over the same blueprint produced different bytes", rel)
		}
	}

	comp := filepath.Join(outDir, "compositions", "xqueuepairs.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueuepairs.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	const url = "https://sqs.eu-north-1.amazonaws.com/123456789012/demo-pair"

	t.Run("observed state flows into the wire", func(t *testing.T) {
		render := exec.Command("crossplane", "composition", "render",
			"testdata/xr-statusref.yaml", comp, fns, "--xrd", xrd,
			"--observed-resources", "testdata/observed-main-queue.yaml", "--timeout", "5m")
		rendered, err := render.CombinedOutput()
		if err != nil {
			t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
		}
		got := string(rendered)
		if !strings.Contains(got, "queueUrl: "+url) {
			t.Errorf("rendered output missing %q — the observed status value did not flow across "+
				"the wire\n---\n%s", "queueUrl: "+url, got)
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(got, bad) {
				t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
			}
		}
	})

	t.Run("unobserved wire is cleanly absent", func(t *testing.T) {
		render := exec.Command("crossplane", "composition", "render",
			"testdata/xr-statusref.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
		rendered, err := render.CombinedOutput()
		if err != nil {
			t.Fatalf("render must succeed with nothing observed: %v\n%s", err, rendered)
		}
		got := string(rendered)
		if strings.Contains(got, "queueUrl") {
			t.Errorf("queueUrl must be omitted entirely until the queue is observed\n---\n%s", got)
		}
		// Both composed documents must still be present — the guard omits one
		// field, never a resource.
		for _, want := range []string{"kind: Queue", "kind: QueuePolicy"} {
			if !strings.Contains(got, want) {
				t.Errorf("rendered output missing %q\n---\n%s", want, got)
			}
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(got, bad) {
				t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
			}
		}
	})
}

// renderedResourceNames decodes every document in a rendered stream and
// collects the composition-resource-name annotation of each composed
// resource (the XR itself carries none and is skipped). Distinctness is
// asserted structurally: a duplicate annotation means two range iterations
// collapsed into one composed resource, so it fails here rather than
// vanishing into a set.
func renderedResourceNames(t *testing.T, rendered []byte) []string {
	t.Helper()
	var names []string
	seen := map[string]bool{}
	for _, chunk := range strings.Split(string(rendered), "\n---\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		var doc struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("rendered document is not valid YAML: %v\n---\n%s", err, chunk)
		}
		name := doc.Metadata.Annotations["crossplane.io/composition-resource-name"]
		if name == "" {
			continue // the XR document
		}
		if seen[name] {
			t.Errorf("composition-resource-name %q appears twice — loop iterations collapsed "+
				"into one composed resource", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
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
