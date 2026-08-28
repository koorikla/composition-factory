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
    group: platform.hooli.tech
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

const genCRDs = `[{"Group":"sqs.aws.m.upbound.io","Kind":"Queue","Plural":"queues","Scope":"Namespaced","Categories":["managed"],"Versions":[{"Name":"v1beta1","Served":true,"Storage":true,"Properties":null}]}]`

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
		"xrds/xqueues.platform.hooli.tech.yaml",
		"compositions/xqueues.platform.hooli.tech.yaml",
		"functions.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
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
		filepath.Join(out, "xrds", "xqueues.platform.hooli.tech.yaml"),
		filepath.Join(out, "compositions", "xqueues.platform.hooli.tech.yaml"),
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
		filepath.Join(out, "xrds", "xqueues.platform.hooli.tech.yaml"),
		filepath.Join(out, "compositions", "xqueues.platform.hooli.tech.yaml"),
	} {
		if strings.Contains(msg, "drift: "+stillInSync) {
			t.Errorf("check output = %q, want %q reported in sync, not drifted -- only functions.yaml was removed", msg, stillInSync)
		}
	}
}
