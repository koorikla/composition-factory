package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"

	"github.com/koorikla/compositionfactory/internal/cache"
	cfmcp "github.com/koorikla/compositionfactory/internal/mcp"
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

// TestMCPRun_SurvivesMalformedInput pins that malformed input on the MCP
// stream does not cause the process to terminate prematurely (CF-250, #124).
func TestMCPRun_SurvivesMalformedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bpPath := filepath.Join(t.TempDir(), "blueprint.cf.yaml")
	c := &MCPCmd{Blueprint: bpPath, Out: t.TempDir(), CacheDir: t.TempDir(), Lock: filepath.Join(t.TempDir(), ".cf.lock")}

	clientInR, clientInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	transport := cfmcp.NewStreamTransport(clientInR, serverOutW)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- c.runWithTransport(ctx, transport)
	}()

	scanner := bufio.NewScanner(serverOutR)
	readLine := func() string {
		if !scanner.Scan() {
			t.Fatalf("expected output line from server: %v", scanner.Err())
		}
		return scanner.Text()
	}

	// 1. Initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n"
	if _, err := clientInW.Write([]byte(initReq)); err != nil {
		t.Fatalf("write initReq: %v", err)
	}
	initResp := readLine()
	if !strings.Contains(initResp, `"id":1`) {
		t.Fatalf("unexpected init response: %s", initResp)
	}

	// 2. Malformed line
	if _, err := clientInW.Write([]byte("not json at all\n")); err != nil {
		t.Fatalf("write bad line: %v", err)
	}
	resp1 := readLine()
	if !strings.Contains(resp1, `-32700`) {
		t.Fatalf("expected -32700 parse error, got: %s", resp1)
	}

	// 3. Ping after malformed line
	if _, err := clientInW.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n")); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	resp2 := readLine()
	if !strings.Contains(resp2, `"id":2`) || strings.Contains(resp2, "error") {
		t.Fatalf("expected valid ping response, got: %s", resp2)
	}

	_ = clientInW.Close()

	select {
	case err := <-serverDone:
		if err != nil && err != io.EOF && !strings.Contains(err.Error(), "closed") {
			t.Fatalf("unexpected server exit error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server timed out exiting after EOF")
	}
}
