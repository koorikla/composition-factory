package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestCatalogueCmd_RejectsInvalidType(t *testing.T) {
	invalidTypes := []string{"bogus", "providers", "functions"}
	for _, invalid := range invalidTypes {
		t.Run(invalid, func(t *testing.T) {
			cmd := &CatalogueCmd{
				Type: invalid,
			}
			var buf bytes.Buffer
			err := cmd.Run(&buf)
			if err == nil {
				t.Fatalf("CatalogueCmd.Run() with Type=%q succeeded, want error", invalid)
			}
			errMsg := err.Error()
			if !strings.Contains(errMsg, "invalid type") ||
				!strings.Contains(errMsg, "provider") ||
				!strings.Contains(errMsg, "function") {
				t.Fatalf("CatalogueCmd.Run() error = %q, want error mentioning invalid type and accepted values (provider, function)", errMsg)
			}
		})
	}
}

func TestCatalogueCmd_CLIInvocation_RejectsInvalidType(t *testing.T) {
	var cli CLI
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cli, append(kongOptions(), kong.Writers(&stdout, &stderr))...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := parser.Parse([]string{"catalogue", "--type", "bogus"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx.BindTo(&stdout, (*io.Writer)(nil))
	runErr := ctx.Run()
	if runErr == nil {
		t.Fatal("ctx.Run() with --type bogus succeeded, want error")
	}
	if !strings.Contains(runErr.Error(), "bogus") ||
		!strings.Contains(runErr.Error(), "provider") ||
		!strings.Contains(runErr.Error(), "function") {
		t.Fatalf("runErr = %q, want error naming 'bogus' and accepted values", runErr.Error())
	}
}

func TestCatalogueCmd_ValidTypesSucceed(t *testing.T) {
	for _, valid := range []string{"", "provider", "function"} {
		t.Run("type="+valid, func(t *testing.T) {
			cmd := &CatalogueCmd{
				Type: valid,
			}
			var buf bytes.Buffer
			if err := cmd.Run(&buf); err != nil {
				t.Fatalf("CatalogueCmd.Run() with Type=%q returned unexpected error: %v", valid, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("CatalogueCmd.Run() with Type=%q returned empty output", valid)
			}
		})
	}
}
