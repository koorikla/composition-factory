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

func TestCF203InitDocsAndDefaultConsistency(t *testing.T) {
	// 1. Verify docs/cli.md accurately documents doc.cf.yaml as the default scaffold path
	// and does not claim blueprint.cf.yaml is the default.
	cliMdPath := filepath.Join("..", "..", "docs", "cli.md")
	if _, err := os.Stat(cliMdPath); os.IsNotExist(err) {
		cliMdPath = filepath.Join("docs", "cli.md")
	}
	content, err := os.ReadFile(cliMdPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", cliMdPath, err)
	}
	text := string(content)

	if strings.Contains(text, "`blueprint.cf.yaml` by default") || strings.Contains(text, "blueprint.cf.yaml by default") {
		t.Errorf("docs/cli.md mistakenly states 'blueprint.cf.yaml by default'")
	}
	if strings.Contains(text, "defaults to `blueprint.cf.yaml`") || strings.Contains(text, "defaults to blueprint.cf.yaml") {
		t.Errorf("docs/cli.md mistakenly states default is blueprint.cf.yaml")
	}
	if !strings.Contains(text, "`doc.cf.yaml` by default") && !strings.Contains(text, "doc.cf.yaml by default") {
		t.Errorf("docs/cli.md should document 'doc.cf.yaml by default'")
	}
	if !strings.Contains(text, "defaults to `doc.cf.yaml`") && !strings.Contains(text, "defaults to doc.cf.yaml") {
		t.Errorf("docs/cli.md should document 'defaults to `doc.cf.yaml`'")
	}

	// 2. Verify cf help init output and InitCmd default
	var stdout, stderr bytes.Buffer
	cli := CLI{}
	parser, err := kong.New(&cli, append(kongOptions(), kong.Writers(&stdout, &stderr))...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"help", "init"})
	if err != nil {
		t.Fatalf("parse 'help init': %v (stderr: %s)", err, stderr.String())
	}
	if err := ctx.Run(); err != nil {
		t.Fatalf("run 'help init': %v", err)
	}
	helpOutput := stdout.String()
	if strings.Contains(helpOutput, "blueprint.cf.yaml") {
		t.Errorf("cf help init mistakenly mentions blueprint.cf.yaml: %s", helpOutput)
	}

	// 3. Verify cf init scaffolds doc.cf.yaml by default in working directory
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
	if !strings.Contains(out, "next: cf kinds, cf provider add, or cf gen doc.cf.yaml") {
		t.Errorf("expected hint to mention doc.cf.yaml, got: %s", out)
	}
	data, err := os.ReadFile("doc.cf.yaml")
	if err != nil {
		t.Fatalf("expected doc.cf.yaml to exist: %v", err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		t.Fatalf("scaffolded doc.cf.yaml parse error: %v", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("scaffolded doc.cf.yaml validate error: %v", err)
	}
}
