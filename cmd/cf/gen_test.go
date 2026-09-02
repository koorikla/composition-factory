package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

const genBlueprint = `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xqueue}
spec:
  sources:
    - provider: example.org/provider-test:v2
  xrd:
    group: platform.sparky.ee
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: {type: string, required: true}
      maxMessageSize: {type: integer}
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        maxMessageSize: {from: params.maxMessageSize}
`

// genCRDs is the cached provider schema these tests generate against.
//
// Properties is populated (it used to be null) because the Composition
// emitter now resolves every blueprint field path against the CRD's
// spec.forProvider schema, so that a typo'd field name is an error here
// rather than a field the API server silently prunes on apply. A CRD with no
// schema at all can no longer back a generated Composition, and a fixture
// that pretended otherwise was testing a shape `cf provider add` never
// produces.
const genCRDs = `[{"Group":"sqs.aws.m.upbound.io","Kind":"Queue","Plural":"queues","Scope":"Namespaced","Categories":["managed"],` +
	`"Versions":[{"Name":"v1beta1","Served":true,"Storage":true,"Properties":{"spec":{"properties":{"forProvider":{` +
	`"required":["region"],"properties":{"region":{"type":"string"},"maxMessageSize":{"type":"integer"}}}}}}}]}]`

// seed writes a blueprint and a pre-populated schema cache.
//
// It seeds the cache through cache.Store's own Save method rather than
// hand-constructing the on-disk layout: Store's directory-naming scheme
// (internal/cache/store.go's slug) hashes the full ref into the directory
// name for collision-freedom, so any hand-picked path here would silently
// drift out of sync with whatever cache.New(cacheDir).Load reads back in
// GenCmd.run. Going through Save keeps this fixture correct regardless of
// how that internal scheme evolves.
func seed(t *testing.T) (dir, bpPath, cacheDir string) {
	t.Helper()
	dir = t.TempDir()
	bpPath = filepath.Join(dir, "xqueue.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(genBlueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir = filepath.Join(dir, "cache")

	var crds []schema.CRD
	if err := json.Unmarshal([]byte(genCRDs), &crds); err != nil {
		t.Fatal(err)
	}
	pkg := &xpkg.Package{Ref: "example.org/provider-test:v2", Digest: "sha256:test"}
	if err := cache.New(cacheDir).Save(pkg, crds); err != nil {
		t.Fatal(err)
	}
	return dir, bpPath, cacheDir
}

func TestGenWritesFiles(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	cmd := &GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}
	var buf bytes.Buffer
	if err := cmd.Run(&buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, p := range []string{
		"xrds/xqueues.platform.sparky.ee.yaml",
		"compositions/xqueues.platform.sparky.ee.yaml",
		"functions.yaml",
		// genBlueprint's one source, provider-test, is not upjet-family-shaped
		// (internal/emit/providerconfigs.go's providerFamily), so it is its
		// own family "test".
		"providerconfigs/test.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

// TestGenCheckDetectsProviderConfigDrift covers --check on the
// providerconfigs/<family>.yaml files the same way TestGenCheckExitCodes
// covers functions.yaml: they are ordinary entries in emit.Generate's output
// list (internal/emit/emit.go), so GenCmd's existing --check loop needs no
// special case for them -- this test is here to prove that wiring actually
// holds for the new file kind, not just assert it by inspection.
func TestGenCheckDetectsProviderConfigDrift(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer

	if err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(out, "providerconfigs", "test.yaml")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("providerconfigs/test.yaml was not written by a plain `cf gen`: %v", err)
	}

	code, _ := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if code != 0 {
		t.Fatalf("in-sync check before any edit: code=%d, want 0", code)
	}

	if err := os.WriteFile(target, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	code, _ = (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if code != 2 {
		t.Errorf("drift check after hand-editing providerconfigs/test.yaml: code=%d, want 2", code)
	}
	if !strings.Contains(buf.String(), "drift: "+target) {
		t.Errorf("check output = %q, want it to name %q", buf.String(), target)
	}
}

func TestGenCheckExitCodes(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer

	// Generate once so the tree is in sync.
	if err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatal(err)
	}
	// In sync -> code 0.
	code, err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if err != nil || code != 0 {
		t.Fatalf("in-sync check: code=%d err=%v, want 0/nil", code, err)
	}
	// Hand-edit a generated file -> drift, code 2.
	target := filepath.Join(out, "functions.yaml")
	if err := os.WriteFile(target, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ = (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if code != 2 {
		t.Errorf("drift check: code=%d, want 2 (distinct from 1, which means the tool failed)", code)
	}
}

// TestGenCheckWithNoPriorRunIsDrift covers the primary real-world --check
// scenario, not just the hand-tampered-file one above: a contributor edits
// or adds a blueprint and forgets to run `cf gen` at all, so the output
// directory never existed. CI depends on this reporting drift (exit code
// 2), not a tool error (1) and not a false "in sync" (0). Asserts on the
// exit code run() actually returns, not on a wrapper's err or a shell
// pipeline's status -- the trap a plain `cf ... | sed ...; echo $?` falls
// into by reporting sed's exit code instead of cf's.
func TestGenCheckWithNoPriorRunIsDrift(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out") // deliberately never created

	var buf bytes.Buffer
	code, err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if err != nil {
		t.Fatalf("run: %v, want nil -- a never-generated tree is drift, not a tool error", err)
	}
	if code != 2 {
		t.Fatalf("code = %d, want 2 (drift) when the output directory was never generated", code)
	}

	msg := buf.String()
	for _, want := range []string{
		filepath.Join(out, "xrds", "xqueues.platform.sparky.ee.yaml"),
		filepath.Join(out, "compositions", "xqueues.platform.sparky.ee.yaml"),
		filepath.Join(out, "functions.yaml"),
	} {
		if !strings.Contains(msg, "drift: "+want) {
			t.Errorf("check output = %q, want it to name missing file %q", msg, want)
		}
	}
}

// TestGenCheckMissingOneFileNamesOnlyThatFile covers a generated file going
// missing after a real, successful `cf gen` (an accidental delete, a
// partial checkout -- anything short of a full hand-edit). It must still
// report drift (2) and must name the specific missing file, while
// continuing to report the other two, untouched outputs as in sync --
// precision matters here: reporting everything as drifted just because one
// file vanished would make the message useless for tracking down what
// actually happened.
func TestGenCheckMissingOneFileNamesOnlyThatFile(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")

	var genBuf bytes.Buffer
	if err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}).Run(&genBuf); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(out, "functions.yaml")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code, err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if err != nil {
		t.Fatalf("run: %v, want nil", err)
	}
	if code != 2 {
		t.Fatalf("code = %d, want 2 (drift) when a generated file is missing", code)
	}

	msg := buf.String()
	if !strings.Contains(msg, "drift: "+missing) {
		t.Errorf("check output = %q, want it to name the missing file %q", msg, missing)
	}
	for _, stillInSync := range []string{
		filepath.Join(out, "xrds", "xqueues.platform.sparky.ee.yaml"),
		filepath.Join(out, "compositions", "xqueues.platform.sparky.ee.yaml"),
	} {
		if strings.Contains(msg, "drift: "+stillInSync) {
			t.Errorf("check output = %q, want %q reported in sync, not drifted -- only functions.yaml was removed", msg, stillInSync)
		}
	}
}

func TestGenGroupSuffix(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")

	var genBuf bytes.Buffer
	cmd := &GenCmd{
		Blueprint:   bp,
		Out:         out,
		CacheDir:    cacheDir,
		GroupSuffix: "cf-testworktree",
	}
	if err := cmd.Run(&genBuf); err != nil {
		t.Fatal(err)
	}

	xrdPath := filepath.Join(out, "xrds", "xqueues.platform.sparky.ee.cf-testworktree.yaml")
	data, err := os.ReadFile(xrdPath)
	if err != nil {
		t.Fatalf("expected XRD at %s, got err: %v", xrdPath, err)
	}
	if !strings.Contains(string(data), "group: platform.sparky.ee.cf-testworktree") {
		t.Errorf("XRD group mismatch in output: %s", string(data))
	}

	compPath := filepath.Join(out, "compositions", "xqueues.platform.sparky.ee.cf-testworktree.yaml")
	compData, err := os.ReadFile(compPath)
	if err != nil {
		t.Fatalf("expected Composition at %s, got err: %v", compPath, err)
	}
	if !strings.Contains(string(compData), "apiVersion: platform.sparky.ee.cf-testworktree/v1alpha1") {
		t.Errorf("Composition compositeTypeRef mismatch in output: %s", string(compData))
	}
}

func TestGenEmitsRBACForNonPreGrantedKinds(t *testing.T) {
	dir := t.TempDir()
	bpPath := filepath.Join(dir, "k8s-ingress.cf.yaml")
	bpContent := `
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata: {name: xingress}
spec:
  xrd:
    group: platform.example.org
    kind: XIngress
    plural: xingresses
    version: v1alpha1
    scope: Namespaced
  resources:
    - name: web-ing
      kind: Ingress
      provider: k8s
      fields:
        spec.rules[0].host: {value: "example.com"}
`
	if err := os.WriteFile(bpPath, []byte(bpContent), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	cmd := &GenCmd{
		Blueprint: bpPath,
		Out:       out,
		CacheDir:  filepath.Join(dir, "cache"),
	}
	if err := cmd.Run(&buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rbacPath := filepath.Join(out, "rbac.yaml")
	rbacData, err := os.ReadFile(rbacPath)
	if err != nil {
		t.Fatalf("expected rbac.yaml at %s, got err: %v", rbacPath, err)
	}
	if !strings.Contains(string(rbacData), "kind: ClusterRole") {
		t.Errorf("rbac.yaml missing ClusterRole: %s", string(rbacData))
	}
	if !strings.Contains(string(rbacData), "ingresses") {
		t.Errorf("rbac.yaml missing ingresses rule: %s", string(rbacData))
	}
	if !strings.Contains(buf.String(), "warning: composed native Kubernetes kinds require cluster RBAC permissions") {
		t.Errorf("output missing RBAC warning, got: %s", buf.String())
	}
}

func TestGenValidateFlag(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")

	var buf bytes.Buffer
	cmd := &GenCmd{
		Blueprint: bp,
		Out:       out,
		CacheDir:  cacheDir,
		Validate:  true,
	}
	code, err := cmd.run(&buf)
	// If crossplane CLI is not installed in the test environment, validate fails with PATH error
	if err != nil {
		if !strings.Contains(err.Error(), "crossplane CLI") && !strings.Contains(err.Error(), "render") {
			t.Errorf("unexpected error: %v", err)
		}
		if code != 1 {
			t.Errorf("code = %d, want 1 on tool failure", code)
		}
	}
}

// TestGenCheckDetectsExtraStaleFiles covers --check when the output directory
// contains unexpected/stale files not produced by the blueprint.
func TestGenCheckDetectsExtraStaleFiles(t *testing.T) {
	dir, bp, cacheDir := seed(t)
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer

	// Generate once so the tree is in sync.
	if err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatal(err)
	}

	// Add an extra/stale file in out/
	staleFile := filepath.Join(out, "stale.yaml")
	if err := os.WriteFile(staleFile, []byte("stale content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	code, err := (&GenCmd{Blueprint: bp, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
	if err != nil || code != 2 {
		t.Fatalf("check with stale file: code=%d err=%v, want code 2 (drift)", code, err)
	}
	if !strings.Contains(buf.String(), "drift: "+staleFile) {
		t.Errorf("check output = %q, want it to report drift for stale file %q", buf.String(), staleFile)
	}
}
