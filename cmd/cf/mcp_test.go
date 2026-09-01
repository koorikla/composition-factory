package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/cache"
)

// mcpDefaults applies kong's declared flag defaults onto c — the same
// mechanism serve_test.go's defaults helper uses (see its doc comment), for
// MCPCmd.
func mcpDefaults(t *testing.T, c *MCPCmd) {
	t.Helper()
	k, err := kong.New(c, kongOptions()...)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	ctx, err := kong.Trace(k, nil)
	if err != nil {
		t.Fatalf("kong.Trace: %v", err)
	}
	if err := ctx.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}
}

// TestMCPDefaultsMatchServe pins that `cf mcp` and `cf serve` resolve the
// same defaults for the flags they share — the two front doors are meant to
// be interchangeable views of one workspace, and a divergent default cache
// dir or lockfile would silently give them different provider worlds.
func TestMCPDefaultsMatchServe(t *testing.T) {
	var m MCPCmd
	mcpDefaults(t, &m)

	if m.Out != "." {
		t.Errorf("Out default = %q, want %q", m.Out, ".")
	}
	if m.Lock != ".cf.lock" {
		t.Errorf("Lock default = %q, want %q", m.Lock, ".cf.lock")
	}
	if m.CacheDir != cache.DefaultRoot() {
		t.Errorf("CacheDir default = %q, want cache.DefaultRoot() = %q", m.CacheDir, cache.DefaultRoot())
	}
}

// TestMCPRunReportsAMissingBlueprint pins startup failure shape: a
// blueprint path that cannot be read fails before any transport is opened,
// with blueprint.Load's own error — the same message `cf serve` and `cf gen`
// give for the same mistake.
func TestMCPRunReportsAMissingBlueprint(t *testing.T) {
	c := &MCPCmd{Blueprint: "does-not-exist.cf.yaml", Out: t.TempDir(), CacheDir: t.TempDir(), Lock: ".cf.lock"}
	err := c.run(context.Background())
	if err == nil {
		t.Fatal("run succeeded with a missing blueprint")
	}
	if !strings.Contains(err.Error(), "read blueprint") {
		t.Errorf("error = %q, want blueprint.Load's own read failure", err)
	}
}
