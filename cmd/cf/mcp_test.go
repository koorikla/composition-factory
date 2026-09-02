package main

import (
	"context"
	"os"
	"path/filepath"
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

// TestMCPRunScaffoldsMissingBlueprint pins that a missing blueprint path
// is scaffolded automatically on startup, matching `cf serve` behavior.
func TestMCPRunScaffoldsMissingBlueprint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so stdio transport does not block

	bpPath := filepath.Join(t.TempDir(), "missing.cf.yaml")
	c := &MCPCmd{Blueprint: bpPath, Out: t.TempDir(), CacheDir: t.TempDir(), Lock: filepath.Join(t.TempDir(), ".cf.lock")}
	_ = c.run(ctx)

	if _, err := os.Stat(bpPath); err != nil {
		t.Fatalf("expected missing blueprint to be scaffolded on startup: %v", err)
	}
}
