package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func runCF(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var cli CLI
	opts := append(kongOptions(), kong.Exit(func(int) {}))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	ctx.BindTo(&out, (*io.Writer)(nil))
	runErr := ctx.Run()
	return out.String(), runErr
}

func TestInitWritesAMinimalValidBlueprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blueprint.cf.yaml")

	if _, err := runCF(t, "init", path); err != nil {
		t.Fatalf("cf init: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cf init wrote no blueprint: %v", err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("cf init wrote a blueprint that does not parse: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("cf init wrote a blueprint that does not validate: %v", err)
	}
	if b.Spec.XRD.Kind == "" || b.Spec.XRD.Group == "" || b.Spec.XRD.Version == "" {
		t.Errorf("cf init wrote an XRD with no identity: %+v", b.Spec.XRD)
	}
	if _, ok := b.Spec.XRD.Parameters["providerName"]; !ok {
		t.Error("cf init omitted providerName, which every Namespaced XRD requires")
	}
}

func TestInitDefaultsToDocCFYaml(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	out, err := runCF(t, "init")
	if err != nil {
		t.Fatalf("cf init default: %v", err)
	}
	if !strings.Contains(out, "scaffolded doc.cf.yaml") {
		t.Errorf("expected output to mention scaffolded doc.cf.yaml, got: %s", out)
	}
	if _, err := os.Stat("doc.cf.yaml"); err != nil {
		t.Errorf("expected doc.cf.yaml to exist, got: %v", err)
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blueprint.cf.yaml")
	const existing = "existing: document\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runCF(t, "init", path); err == nil {
		t.Fatal("cf init overwrote an existing file")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Errorf("file was modified: %q", data)
	}
}
