package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Step 4: render what we generated. The lock serializes real renders
	// across concurrently running test packages — they all reuse the same
	// runtime-docker-name containers (see internal/rendertest).
	comp := filepath.Join(outDir, "compositions", "xqueues.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xqueues.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	release := rendertest.Lock(t)
	render := exec.Command("crossplane", "composition", "render",
		"testdata/xr.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	rendered, err := render.CombinedOutput()
	release()
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
	comp := filepath.Join(outDir, "compositions", "xwebapps.platform.hooli.tech.yaml")
	xrd := filepath.Join(outDir, "xrds", "xwebapps.platform.hooli.tech.yaml")
	fns := filepath.Join(outDir, "functions.yaml")
	release := rendertest.Lock(t)
	render := exec.Command("crossplane", "composition", "render",
		"testdata/xr-webapp.yaml", comp, fns, "--xrd", xrd, "--timeout", "5m")
	rendered, err := render.CombinedOutput()
	release()
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
