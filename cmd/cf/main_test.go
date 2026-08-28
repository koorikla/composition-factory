package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/cache"
)

func TestVersionCommand(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli, kong.Name("cf"), kong.Exit(func(int) {}),
		kong.Vars{"cachedir": cache.DefaultRoot()})
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
