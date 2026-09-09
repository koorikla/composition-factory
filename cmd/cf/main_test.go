package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func TestVersionCommand(t *testing.T) {
	var cli CLI
	opts := append(kongOptions(), kong.Exit(func(int) {}))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"version"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out bytes.Buffer
	ctx.BindTo(&out, (*io.Writer)(nil))
	if err := ctx.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "cf ") {
		t.Errorf("version output = %q, want it to contain %q", out.String(), "cf ")
	}
}

func TestVersionFlag(t *testing.T) {
	var cli CLI
	var out bytes.Buffer
	exited := false
	opts := append(kongOptions(), kong.Exit(func(code int) { exited = true }), kong.Writers(&out, &out))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	_, _ = parser.Parse([]string{"--version"})
	if !exited {
		t.Error("expected exit on --version")
	}
	if !strings.Contains(out.String(), "cf ") {
		t.Errorf("version output = %q, want it to contain %q", out.String(), "cf ")
	}
}

func TestCF113HelpCommandSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := CLI{}
	parser, err := kong.New(&cli, append(kongOptions(), kong.Writers(&stdout, &stderr))...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"help"})
	if err != nil {
		t.Fatalf("parse 'help': %v (stderr: %s)", err, stderr.String())
	}
	if err := ctx.Run(); err != nil {
		t.Fatalf("run 'help': %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage: cf") {
		t.Errorf("expected stdout to contain 'Usage: cf', got: %s", stdout.String())
	}
}

func TestCF113HelpSubcommandSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cli := CLI{}
	parser, err := kong.New(&cli, append(kongOptions(), kong.Writers(&stdout, &stderr))...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"help", "gen"})
	if err != nil {
		t.Fatalf("parse 'help gen': %v (stderr: %s)", err, stderr.String())
	}
	if err := ctx.Run(); err != nil {
		t.Fatalf("run 'help gen': %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage: cf gen") {
		t.Errorf("expected stdout to contain 'Usage: cf gen', got: %s", stdout.String())
	}
}

func TestCF113ProviderNameRemedyMentionsInit(t *testing.T) {
	badYAML := `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test
spec:
  xrd:
    group: platform.example.org
    version: v1alpha1
    kind: XApp
    plural: xapps
    scope: Namespaced
  resources:
    - name: bucket
      kind: Bucket
      provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0
`
	_, err := blueprint.Parse([]byte(badYAML))
	if err == nil {
		t.Fatal("expected error for missing providerName, got nil")
	}
	if !strings.Contains(err.Error(), "cf init") {
		t.Errorf("expected providerName error to mention 'cf init', got: %v", err)
	}
}
