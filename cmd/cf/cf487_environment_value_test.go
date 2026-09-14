// cmd/cf/cf487_environment_value_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCF487GenDoesNotSilentlyBlankOperatorEnvironmentValue pins the contract
// that regenerating an output directory must not destroy an operator-supplied
// environment value that the blueprint itself gives no value or default for,
// while reporting nothing but `wrote ...` and exiting 0.
//
// Either arm satisfies the contract: the value survives regeneration, or cf gen
// says it is discarding it, naming the file and the key. Silence plus loss is
// the failure.
func TestCF487GenDoesNotSilentlyBlankOperatorEnvironmentValue(t *testing.T) {
	dir, _, cacheDir := seed(t)
	bp := `
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
  environment:
    password:
      type: string
      description: Master password for PostgreSQL.
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {value: "us-east-1"}
`
	bpPath := filepath.Join(dir, "xqueue-env.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")

	var buf bytes.Buffer
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	target := filepath.Join(out, "environmentconfigs", "default.yaml")
	first, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("environmentconfigs/default.yaml was not written: %v", err)
	}
	if !strings.Contains(string(first), `password: ""`) {
		t.Fatalf("expected the emitted EnvironmentConfig to carry an empty slot for the declared key, got:\n%s", first)
	}

	// The operator fills in the value the blueprint declares and cf offers no
	// other place to supply.
	filled := strings.Replace(string(first), `password: ""`, `password: "operator-typed-this"`, 1)
	if err := os.WriteFile(target, []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after regenerate: %v", err)
	}
	report := buf.String()

	survived := strings.Contains(string(second), "operator-typed-this")
	// "Said so" is at minimum: something beyond the ordinary `wrote <path>`
	// line that names the key whose value is being discarded.
	announced := strings.Contains(report, "password") && strings.Contains(report, target)

	if !survived && !announced {
		t.Fatalf("cf gen discarded the operator's environment value at exit 0 with no mention of it.\n"+
			"environmentconfigs/default.yaml after regenerate:\n%s\ncf gen said:\n%s", second, report)
	}

	// If the value survives, the tree it leaves behind must not read as stale:
	// `cf gen --check` in CI would otherwise go red forever on every
	// EnvironmentConfig an operator has filled in.
	if survived {
		buf.Reset()
		code, err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir, Check: true}).run(&buf)
		if err != nil {
			t.Fatalf("check Run: %v", err)
		}
		if code != 0 {
			t.Errorf("cf gen --check reports drift on an output tree cf gen itself just left in place: code=%d, said: %s", code, buf.String())
		}
	}
}

func TestCF487BlueprintDefaultWinsOverExistingValue(t *testing.T) {
	dir, _, cacheDir := seed(t)
	bp := `
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
  environment:
    password:
      type: string
      default: "blueprint-default-password"
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {value: "us-east-1"}
`
	bpPath := filepath.Join(dir, "xqueue-env.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	target := filepath.Join(out, "environmentconfigs", "default.yaml")
	if err := os.WriteFile(target, []byte(`apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: default
data:
  password: "operator-typed-this"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after regenerate: %v", err)
	}
	if !strings.Contains(string(second), "blueprint-default-password") {
		t.Fatalf("expected blueprint default to win, got:\n%s", second)
	}
}

func TestCF487BlueprintDataWinsOverExistingValue(t *testing.T) {
	dir, _, cacheDir := seed(t)
	bp := `
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
  environment:
    password:
      type: string
  environmentConfigs:
    - name: default
      data:
        password: "blueprint-explicit-password"
  resources:
    - name: main-queue
      kind: Queue
      provider: example.org/provider-test:v2
      fields:
        region: {value: "us-east-1"}
`
	bpPath := filepath.Join(dir, "xqueue-env.cf.yaml")
	if err := os.WriteFile(bpPath, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	var buf bytes.Buffer
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	target := filepath.Join(out, "environmentconfigs", "default.yaml")
	if err := os.WriteFile(target, []byte(`apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: default
data:
  password: "operator-typed-this"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := (&GenCmd{Blueprint: bpPath, Out: out, CacheDir: cacheDir}).Run(&buf); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after regenerate: %v", err)
	}
	if !strings.Contains(string(second), "blueprint-explicit-password") {
		t.Fatalf("expected blueprint explicit data to win, got:\n%s", second)
	}
}
