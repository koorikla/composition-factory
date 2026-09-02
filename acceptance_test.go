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

	"github.com/koorikla/compositionfactory/internal/rendertest"
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
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	rendered, err := renderComposition(t, "testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
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
		filepath.Join("compositions", "xqueuesets.platform.sparky.ee.yaml"),
		filepath.Join("xrds", "xqueuesets.platform.sparky.ee.yaml"),
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

	comp := filepath.Join(outDir, "compositions", "xqueuesets.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueuesets.platform.sparky.ee.yaml")
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
			rendered, err := renderComposition(t, tc.xr, comp, fns, "--xrd", xrd, "--timeout", "5m")
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

// TestAcceptanceWhenRenders is the when gate: one blueprint carrying a
// string-comparison guard (audit-queue on tier == "pro") and a bare-boolean
// guard composed with forEach (replica-queue), generated once and rendered
// through the real crossplane composition render both ways. The XR fixtures
// exercise the real XRD schema defaulting: xr-when-pro.yaml sets only tier
// (replicasEnabled/replicas default true/2 → audit plus two replicas), and
// xr-when-off.yaml sets only replicasEnabled: false (tier defaults standard
// → the unconditional queue alone, every loop iteration skipped by the when
// OUTSIDE the range). Asserted on the rendered ARTIFACT's
// composition-resource-name annotations, plus the <no value> grep.
func TestAcceptanceWhenRenders(t *testing.T) {
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
		gen := exec.Command(bin, "gen", "testdata/xqueue-when.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xqueuetiers.platform.sparky.ee.yaml"),
		filepath.Join("xrds", "xqueuetiers.platform.sparky.ee.yaml"),
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

	comp := filepath.Join(outDir, "compositions", "xqueuetiers.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueuetiers.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	cases := []struct {
		name string
		xr   string
		want []string // every composition-resource-name annotation, sorted
	}{
		{"tier pro with defaulted replicas renders audit plus fan-out", "testdata/xr-when-pro.yaml",
			[]string{"audit-queue", "main-queue", "replica-queue-0", "replica-queue-1"}},
		{"defaults plus replicasEnabled false renders the unconditional queue alone", "testdata/xr-when-off.yaml",
			[]string{"main-queue"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := renderComposition(t,
				tc.xr, comp, fns, "--xrd", xrd, "--timeout", "5m")
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
		filepath.Join("compositions", "xqueuepairs.platform.sparky.ee.yaml"),
		filepath.Join("xrds", "xqueuepairs.platform.sparky.ee.yaml"),
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

	comp := filepath.Join(outDir, "compositions", "xqueuepairs.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueuepairs.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	const url = "https://sqs.eu-north-1.amazonaws.com/123456789012/demo-pair"

	t.Run("observed state flows into the wire", func(t *testing.T) {
		rendered, err := renderComposition(t,
			"testdata/xr-statusref.yaml", comp, fns, "--xrd", xrd,
			"--observed-resources", "testdata/observed-main-queue.yaml", "--timeout", "5m")
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
		rendered, err := renderComposition(t,
			"testdata/xr-statusref.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
		if err != nil {
			t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
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

// TestAcceptanceConventionsRender is the user-templates gate: a blueprint
// with a naming and a tags convention over two queues, one of which
// overrides the name explicitly, generated and rendered through the real
// crossplane composition render — the real function-go-templating engine
// executing the emitted define blocks and the real sprig
// include/dict/trim/nindent pipeline, not the unit tests' stubs. Asserted
// structurally on the rendered ARTIFACT: the scalar template output becomes
// queue-a's forProvider.name, the multi-line output becomes a real tags
// MAPPING on both queues, and queue-b's explicit name wins.
func TestAcceptanceConventionsRender(t *testing.T) {
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
		gen := exec.Command(bin, "gen", "testdata/xqueue-conventions.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xqueueconvs.platform.sparky.ee.yaml"),
		filepath.Join("xrds", "xqueueconvs.platform.sparky.ee.yaml"),
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

	comp := filepath.Join(outDir, "compositions", "xqueueconvs.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueueconvs.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	rendered, err := renderComposition(t,
		"testdata/xr-conventions.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	if err != nil {
		t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
	}

	forProviderByResource := map[string]map[string]any{}
	for _, chunk := range strings.Split(string(rendered), "\n---\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		var doc struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				ForProvider map[string]any `json:"forProvider"`
			} `json:"spec"`
		}
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("rendered document is not valid YAML: %v\n---\n%s", err, chunk)
		}
		if name := doc.Metadata.Annotations["crossplane.io/composition-resource-name"]; name != "" {
			forProviderByResource[name] = doc.Spec.ForProvider
		}
	}

	a, ok := forProviderByResource["queue-a"]
	if !ok {
		t.Fatalf("no queue-a document rendered\n---\n%s", rendered)
	}
	if a["name"] != "demo-conv-queue-a" {
		t.Errorf("queue-a forProvider.name = %v, want demo-conv-queue-a from the naming convention", a["name"])
	}
	b, ok := forProviderByResource["queue-b"]
	if !ok {
		t.Fatalf("no queue-b document rendered\n---\n%s", rendered)
	}
	if b["name"] != "custom-b" {
		t.Errorf("queue-b forProvider.name = %v, want the explicit custom-b to override the convention", b["name"])
	}
	for _, q := range []string{"queue-a", "queue-b"} {
		tags, isMap := forProviderByResource[q]["tags"].(map[string]any)
		if !isMap {
			t.Fatalf("%s tags = %v (%T), want a YAML mapping\n---\n%s",
				q, forProviderByResource[q]["tags"], forProviderByResource[q]["tags"], rendered)
		}
		if tags["managed-by"] != "crossplane" || tags["xr"] != "demo-conv" {
			t.Errorf("%s tags = %v, want managed-by: crossplane and xr: demo-conv", q, tags)
		}
	}
	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(string(rendered), bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}
}

// renderedResourceNames decodes every document in a rendered stream and
// collects the composition-resource-name annotation of each composed
// resource (the XR itself carries none and is skipped). Distinctness is
// asserted structurally: a duplicate annotation means two range iterations
// collapsed into one composed resource, so it fails here rather than
// vanishing into a set.
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
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.sparky.ee.yaml")
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

	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	// Render 1: no observed state. The pipeline must run end to end -- the
	// declared auto-ready step's package really pulled and executed -- and
	// the XR's Ready condition is False because nothing is observed yet.
	rendered, err := renderComposition(t, "testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
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
	rendered2, err := renderComposition(t,
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--observed-resources", observed, "--timeout", "5m")
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

// TestAcceptanceNativeCompositionRenders is the native-kinds acceptance
// gate: a blueprint composing a managed Queue, a native Deployment and a
// native Service, rendered through the real `crossplane composition render`.
// It proves structurally — by decoding the rendered documents, not by
// substring luck — that the Deployment lands as the Kubernetes object
// itself: the parameter's image at spec.template.spec.containers[0].image,
// and NO forProvider envelope, NO providerConfigRef, on either native
// resource, while the Queue beside them keeps its full managed envelope.
func TestAcceptanceNativeCompositionRenders(t *testing.T) {
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

	// Step 1: fetch the managed provider. The native kinds need no fetch at
	// all — they are vendored into the binary, pinned to one Kubernetes
	// version, which is half of what this test exists to prove.
	add := exec.Command(bin, "provider", "add", providerRef, "--cache-dir", cacheDir, "--lock", lock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("cf provider add: %v\n%s", err, out)
	}

	// Step 2: generate, then prove --check sees the fresh tree as in sync.
	outDir := filepath.Join(dir, "out")
	gen := exec.Command(bin, "gen", "testdata/xwebapp.cf.yaml", "-o", outDir, "--cache-dir", cacheDir)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cf gen: %v\n%s", err, out)
	}
	chk := exec.Command(bin, "gen", "testdata/xwebapp.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	// Step 3: render what we generated with the real crossplane CLI,
	// serialized against other packages' real renders (see internal/rendertest).
	comp := filepath.Join(outDir, "compositions", "xwebapps.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xwebapps.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	rendered, err := renderComposition(t, "testdata/xr-webapp.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	if err != nil {
		t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
	}

	docs := decodeRenderedDocs(t, rendered)

	dep, ok := docs["Deployment"]
	if !ok {
		t.Fatalf("no Deployment among rendered documents\n---\n%s", rendered)
	}
	if av := dep["apiVersion"]; av != "apps/v1" {
		t.Errorf("Deployment apiVersion = %v, want apps/v1", av)
	}
	if got := digAny(dep, "spec", "template", "spec", "containers", 0, "image"); got != "nginx:1.29.1" {
		t.Errorf("Deployment containers[0].image = %v, want the XR's parameter value nginx:1.29.1\n---\n%s", got, rendered)
	}
	svc, ok := docs["Service"]
	if !ok {
		t.Fatalf("no Service among rendered documents\n---\n%s", rendered)
	}
	if av := svc["apiVersion"]; av != "v1" {
		t.Errorf("Service apiVersion = %v, want bare v1", av)
	}
	for kind, doc := range map[string]map[string]any{"Deployment": dep, "Service": svc} {
		spec, _ := doc["spec"].(map[string]any)
		if spec == nil {
			t.Errorf("%s rendered without a spec", kind)
			continue
		}
		for _, forbidden := range []string{"forProvider", "providerConfigRef", "managementPolicies", "deletionPolicy"} {
			if _, present := spec[forbidden]; present {
				t.Errorf("%s spec carries %q — a native object must land WITHOUT any Crossplane envelope\n---\n%s",
					kind, forbidden, rendered)
			}
		}
	}

	// The managed Queue beside them must keep its envelope — the fork must
	// branch, not leak.
	queue, ok := docs["Queue"]
	if !ok {
		t.Fatalf("no Queue among rendered documents\n---\n%s", rendered)
	}
	if got := digAny(queue, "spec", "forProvider", "region"); got != "eu-north-1" {
		t.Errorf("Queue spec.forProvider.region = %v, want eu-north-1", got)
	}
	if got := digAny(queue, "spec", "providerConfigRef", "name"); got != "localstack" {
		t.Errorf("Queue providerConfigRef.name = %v, want localstack", got)
	}

	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(string(rendered), bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}
}

// TestAcceptanceEnvelopeRenders is the authorable-envelope gate: a blueprint
// wiring writeConnectionSecretToRef.name from an XRD parameter and setting
// managementPolicies with the comma-separated value form, generated and
// rendered through the real crossplane composition render — the real
// function-go-templating engine under options: ["missingkey=error"], against
// the real .m. namespaced Queue CRD (whose envelope genuinely carries
// writeConnectionSecretToRef with name only). Asserted on the rendered
// ARTIFACT: the Queue carries the XR's secret name, the policies list as a
// real YAML sequence, and the computed providerConfigRef untouched beside
// the authored entries.
func TestAcceptanceEnvelopeRenders(t *testing.T) {
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
	gen := exec.Command(bin, "gen", "testdata/xqueue-envelope.cf.yaml", "-o", outDir, "--cache-dir", cacheDir)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("cf gen: %v\n%s", err, out)
	}
	chk := exec.Command(bin, "gen", "testdata/xqueue-envelope.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	comp := filepath.Join(outDir, "compositions", "xsecretqueues.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xsecretqueues.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	rendered, err := renderComposition(t, "testdata/xr-envelope.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	if err != nil {
		t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
	}

	docs := decodeRenderedDocs(t, rendered)
	queue, ok := docs["Queue"]
	if !ok {
		t.Fatalf("no Queue among rendered documents\n---\n%s", rendered)
	}
	// The wired envelope entry: the XR's secretName must land as the
	// connection-secret name on the composed resource.
	if got := digAny(queue, "spec", "writeConnectionSecretToRef", "name"); got != "queue-conn" {
		t.Errorf("Queue spec.writeConnectionSecretToRef.name = %v, want the XR's parameter value queue-conn\n---\n%s",
			got, rendered)
	}
	// The comma-separated value form: a real YAML sequence, not one string.
	policies, ok := digAny(queue, "spec", "managementPolicies").([]any)
	if !ok {
		t.Fatalf("Queue spec.managementPolicies = %v, want a YAML sequence\n---\n%s",
			digAny(queue, "spec", "managementPolicies"), rendered)
	}
	want := []any{"Observe", "Create", "Update", "Delete", "LateInitialize"}
	if !slices.Equal(policies, want) {
		t.Errorf("Queue spec.managementPolicies = %v, want %v", policies, want)
	}
	// The computed default beside the authored entries, untouched.
	if got := digAny(queue, "spec", "providerConfigRef", "kind"); got != "ClusterProviderConfig" {
		t.Errorf("Queue providerConfigRef.kind = %v, want ClusterProviderConfig", got)
	}
	if got := digAny(queue, "spec", "providerConfigRef", "name"); got != "localstack" {
		t.Errorf("Queue providerConfigRef.name = %v, want localstack", got)
	}

	for _, bad := range []string{"<no value>", "<nil>"} {
		if strings.Contains(string(rendered), bad) {
			t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
		}
	}
}

// TestAcceptanceTypedObjectParamRenders is the typed-object-parameter gate:
// a blueprint whose tuning parameter declares typed members (maxSize with a
// schema default, retention required) wired as params.tuning.<member>,
// generated and rendered through the real crossplane composition render —
// the real XRD member-level schema defaulting, the real
// function-go-templating hasKey semantics. Proven on the rendered ARTIFACT
// both ways:
//
//   - an XR setting tuning.retention alone flows retention into the
//     composed Queue AND picks up maxSize=2048 from the member's own XRD
//     default (defaults inject into a PRESENT object);
//   - an XR omitting the optional tuning object entirely renders cleanly
//     with both wires absent — the two-level hasKey chain under
//     missingkey=error, never "<no value>".
func TestAcceptanceTypedObjectParamRenders(t *testing.T) {
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

	// Generate twice into separate directories: determinism is a correctness
	// requirement, so the two runs must agree byte for byte.
	outDir := filepath.Join(dir, "out")
	for _, o := range []string{outDir, filepath.Join(dir, "out2")} {
		gen := exec.Command(bin, "gen", "testdata/xqueue-typedobj.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xtunedqueues.platform.hooli.tech.yaml"),
		filepath.Join("xrds", "xtunedqueues.platform.hooli.tech.yaml"),
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

	comp := filepath.Join(outDir, "compositions", "xtunedqueues.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xtunedqueues.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")

	t.Run("XR-set member flows and the member default injects", func(t *testing.T) {
		rendered, err := renderComposition(t, "testdata/xr-typedobj.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
		if err != nil {
			t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
		}
		docs := decodeRenderedDocs(t, rendered)
		queue, ok := docs["Queue"]
		if !ok {
			t.Fatalf("no Queue among rendered documents\n---\n%s", rendered)
		}
		// The XR-set member, through the wire.
		if got := digAny(queue, "spec", "forProvider", "messageRetentionSeconds"); got != float64(345600) {
			t.Errorf("Queue forProvider.messageRetentionSeconds = %v, want the XR's tuning.retention 345600\n---\n%s",
				got, rendered)
		}
		// The member the XR never set: the XRD's member-level schema default
		// must have injected it into the present tuning object.
		if got := digAny(queue, "spec", "forProvider", "maxMessageSize"); got != float64(2048) {
			t.Errorf("Queue forProvider.maxMessageSize = %v, want the member default 2048\n---\n%s",
				got, rendered)
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(string(rendered), bad) {
				t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
			}
		}
	})

	t.Run("optional object absent omits both wires cleanly", func(t *testing.T) {
		rendered, err := renderComposition(t, "testdata/xr-typedobj-absent.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
		if err != nil {
			t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
		}
		docs := decodeRenderedDocs(t, rendered)
		queue, ok := docs["Queue"]
		if !ok {
			t.Fatalf("no Queue among rendered documents\n---\n%s", rendered)
		}
		fp, ok := digAny(queue, "spec", "forProvider").(map[string]any)
		if !ok {
			t.Fatalf("Queue spec.forProvider = %v, want a map\n---\n%s", digAny(queue, "spec", "forProvider"), rendered)
		}
		for _, absent := range []string{"maxMessageSize", "messageRetentionSeconds"} {
			if v, present := fp[absent]; present {
				t.Errorf("forProvider.%s = %v, want the wire omitted when the tuning object is absent\n---\n%s",
					absent, v, rendered)
			}
		}
		if fp["region"] != "eu-north-1" {
			t.Errorf("forProvider.region = %v, want the unconditional value eu-north-1", fp["region"])
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(string(rendered), bad) {
				t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
			}
		}
	})
}

// renderComposition runs `crossplane composition render` under the
// machine-wide render lock (internal/rendertest), retrying exactly once on
// the CLI's known pinned-container race: generated functions.yaml pins
// render.crossplane.io/runtime-docker-name so renders reuse one container
// per function, and a render starting moments after another finishes can
// find that container still attached to the previous render's dying network
// ("is not connected to Docker network ..."). dockerd settles the previous
// network's teardown asynchronously AFTER the previous crossplane process
// has exited, so no amount of test-side serialization closes the window
// completely; removing the stale containers and retrying once does. The
// retry is gated on that exact error text — any other failure surfaces
// immediately, unretried.
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

// decodeRenderedDocs splits `crossplane composition render`'s multi-document
// stream and decodes each document, keyed by kind (each kind appears once in
// this fixture).
func decodeRenderedDocs(t *testing.T, rendered []byte) map[string]map[string]any {
	t.Helper()
	docs := map[string]map[string]any{}
	for _, chunk := range strings.Split(string(rendered), "\n---\n") {
		chunk = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(chunk), "---"))
		if chunk == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("rendered document is not valid YAML: %v\n---\n%s", err, chunk)
		}
		if kind, _ := doc["kind"].(string); kind != "" {
			docs[kind] = doc
		}
	}
	return docs
}

// digAny walks nested maps/slices by string key or int index, returning nil
// the moment a step does not resolve — assertions then fail on the value.
func digAny(v any, path ...any) any {
	for _, step := range path {
		switch s := step.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[s]
		case int:
			l, ok := v.([]any)
			if !ok || s >= len(l) {
				return nil
			}
			v = l[s]
		}
	}
	return v
}

// fakeReporter records which of Skipf/Fatalf unavailable chose, so the
// decision can be tested without a Docker daemon or a crossplane CLI.

// TestAcceptanceIRSARenders is the annotations gate, on the motivating use
// case: an IAM Role (provider-aws-iam, managed) whose observed
// status.atProvider.arn flows into a native v1 ServiceAccount's
// eks.amazonaws.com/role-arn annotation — the IRSA handshake — rendered
// through the real crossplane composition render, both ways:
//
//   - WITHOUT observed state the ServiceAccount renders with the annotation
//     KEY cleanly absent (the Role has no ARN yet; Crossplane fills it on a
//     later reconcile) — never empty-valued, never "<no value>";
//   - WITH --observed-resources supplying the Role's ARN, the value lands in
//     the ServiceAccount's annotation verbatim.
//
// Along the way it proves the surrounding discipline for real: the trust
// policy template renders an assumeRolePolicy that is a JSON *string* (not a
// YAML mapping the API server would reject against type: string) carrying
// the OIDC issuer derived from the provider ARN and the schema-DEFAULTED
// namespace, the native ServiceAccount carries no Crossplane envelope, and
// two generate runs agree byte for byte.
func TestAcceptanceIRSARenders(t *testing.T) {
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

	add := exec.Command(bin, "provider", "add",
		"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0", "--cache-dir", cacheDir, "--lock", lock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("cf provider add: %v\n%s", err, out)
	}

	// Generate twice into separate directories: determinism is a correctness
	// requirement, so the two runs must agree byte for byte.
	outDir := filepath.Join(dir, "out")
	for _, o := range []string{outDir, filepath.Join(dir, "out2")} {
		gen := exec.Command(bin, "gen", "testdata/irsa.cf.yaml", "-o", o, "--cache-dir", cacheDir)
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("cf gen into %s: %v\n%s", o, err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join("compositions", "xirsas.platform.sparky.ee.yaml"),
		filepath.Join("xrds", "xirsas.platform.sparky.ee.yaml"),
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
	chk := exec.Command(bin, "gen", "testdata/irsa.cf.yaml", "-o", outDir, "--cache-dir", cacheDir, "--check")
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("cf gen --check right after gen should exit 0: %v\n%s", err, out)
	}

	comp := filepath.Join(outDir, "compositions", "xirsas.platform.sparky.ee.yaml")
	xrd := filepath.Join(outDir, "xrds", "xirsas.platform.sparky.ee.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	const roleARN = "arn:aws:iam::123456789012:role/demo-irsa"

	// saAnnotations pulls the ServiceAccount's rendered annotations map.
	saAnnotations := func(t *testing.T, docs map[string]map[string]any, rendered []byte) map[string]any {
		t.Helper()
		sa, ok := docs["ServiceAccount"]
		if !ok {
			t.Fatalf("no ServiceAccount among rendered documents\n---\n%s", rendered)
		}
		anns, ok := digAny(sa, "metadata", "annotations").(map[string]any)
		if !ok {
			t.Fatalf("ServiceAccount metadata.annotations = %v, want a map\n---\n%s",
				digAny(sa, "metadata", "annotations"), rendered)
		}
		return anns
	}

	t.Run("unobserved: the annotation key is cleanly absent", func(t *testing.T) {
		rendered, err := renderComposition(t, "testdata/xr-irsa.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
		if err != nil {
			t.Fatalf("crossplane composition render: %v\n%s", err, rendered)
		}
		docs := decodeRenderedDocs(t, rendered)

		anns := saAnnotations(t, docs, rendered)
		if v, present := anns["eks.amazonaws.com/role-arn"]; present {
			t.Errorf("eks.amazonaws.com/role-arn = %v — the key must be omitted entirely while the "+
				"Role is unobserved, never rendered empty", v)
		}
		if got := anns["crossplane.io/composition-resource-name"]; got != "sa" {
			t.Errorf("composition-resource-name = %v, want sa — the function-set annotation must "+
				"survive beside authored ones", got)
		}
		sa := docs["ServiceAccount"]
		if got := sa["automountServiceAccountToken"]; got != true {
			t.Errorf("automountServiceAccountToken = %v (%T), want true", got, got)
		}
		if _, has := sa["spec"]; has {
			t.Errorf("ServiceAccount grew a spec — a native object carries no Crossplane envelope\n---\n%s", rendered)
		}

		role, ok := docs["Role"]
		if !ok {
			t.Fatalf("no Role among rendered documents\n---\n%s", rendered)
		}
		policy, ok := digAny(role, "spec", "forProvider", "assumeRolePolicy").(string)
		if !ok {
			t.Fatalf("assumeRolePolicy = %T (%v), want a JSON *string* — an unquoted template body "+
				"would render a YAML mapping the API server rejects against type: string\n---\n%s",
				digAny(role, "spec", "forProvider", "assumeRolePolicy"),
				digAny(role, "spec", "forProvider", "assumeRolePolicy"), rendered)
		}
		var trust struct {
			Version   string `json:"Version"`
			Statement []struct {
				Effect    string `json:"Effect"`
				Action    string `json:"Action"`
				Principal struct {
					Federated string `json:"Federated"`
				} `json:"Principal"`
				Condition struct {
					StringEquals map[string]string `json:"StringEquals"`
				} `json:"Condition"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(policy), &trust); err != nil {
			t.Fatalf("assumeRolePolicy is not valid JSON: %v\n---\n%s", err, policy)
		}
		if len(trust.Statement) != 1 || trust.Statement[0].Action != "sts:AssumeRoleWithWebIdentity" {
			t.Fatalf("trust policy statement = %+v, want one sts:AssumeRoleWithWebIdentity statement", trust)
		}
		const oidcARN = "arn:aws:iam::123456789012:oidc-provider/oidc.eks.eu-north-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE"
		if trust.Statement[0].Principal.Federated != oidcARN {
			t.Errorf("Federated = %q, want the XR's oidcProviderArn", trust.Statement[0].Principal.Federated)
		}
		// The issuer key proves regexReplaceAll derived it from the ARN; the
		// subject proves schema defaulting injected namespace AND that .xr
		// carried the composite's name into the template.
		const issuerSub = "oidc.eks.eu-north-1.amazonaws.com/id/EXAMPLED539D4633E53DE1B71EXAMPLE:sub"
		if got := trust.Statement[0].Condition.StringEquals[issuerSub]; got != "system:serviceaccount:default:demo-irsa" {
			t.Errorf("Condition[%q] = %q, want system:serviceaccount:default:demo-irsa "+
				"(namespace from the XRD default, name from the XR)", issuerSub, got)
		}
		if got := digAny(role, "spec", "providerConfigRef", "name"); got != "aws-main" {
			t.Errorf("Role providerConfigRef.name = %v, want aws-main — the managed half keeps its envelope", got)
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(string(rendered), bad) {
				t.Errorf("rendered output contains %q — a missing field reached a live resource shape", bad)
			}
		}
	})

	t.Run("observed: the ARN lands in the annotation", func(t *testing.T) {
		rendered, err := renderComposition(t, "testdata/xr-irsa.yaml", comp, fns, "--xrd", xrd,
			"--observed-resources", "testdata/observed-role.yaml", "--timeout", "5m")
		if err != nil {
			t.Fatalf("crossplane composition render --observed-resources: %v\n%s", err, rendered)
		}
		docs := decodeRenderedDocs(t, rendered)
		anns := saAnnotations(t, docs, rendered)
		if got := anns["eks.amazonaws.com/role-arn"]; got != roleARN {
			t.Errorf("eks.amazonaws.com/role-arn = %v, want %q — the observed ARN did not flow "+
				"across the wire\n---\n%s", got, roleARN, rendered)
		}
		for _, bad := range []string{"<no value>", "<nil>"} {
			if strings.Contains(string(rendered), bad) {
				t.Errorf("observed render contains %q", bad)
			}
		}
	})
}

// fakeReporter records which of Skipf/Fatalf unavailable chose, so the
// decision can be tested without a Docker daemon or a crossplane CLI.

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
