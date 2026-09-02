package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
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
	opts := append(kongOptions(), kong.Exit(func(int) {}), kong.Writers(&out, &out))
	parser, err := kong.New(&cli, opts...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	exited := false
	opts = append(kongOptions(), kong.Exit(func(code int) { exited = true }), kong.Writers(&out, &out))
	parser, _ = kong.New(&cli, opts...)
	_, _ = parser.Parse([]string{"--version"})
	if !exited {
		t.Error("expected exit on --version")
	}
	if !strings.Contains(out.String(), "cf ") {
		t.Errorf("version output = %q, want it to contain %q", out.String(), "cf ")
	}
}
