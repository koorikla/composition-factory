package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	cfmcp "github.com/koorikla/compositionfactory/internal/mcp"
)

// MCPCmd serves the compositionfactory MCP server over stdio: the HTTP API's
// full authoring surface — schema browsing, blueprint editing, provider
// management, generation and the render check — as MCP tools for an agent.
// See internal/mcp's package comment for the architecture (every tool
// bridges into the exact handler `cf serve` exposes) and docs/mcp.md for how
// to register it in an MCP client.
//
// The transport is stdio and NOTHING in this command may write to stdout:
// stdout carries the JSON-RPC stream, and a stray print corrupts it. That is
// why Run takes no io.Writer the way every other subcommand does — there is
// deliberately no writer here to be tempted by. Diagnostics belong on
// stderr, where the SDK's own logging already goes.
//
// SECURITY: like `cf serve`, this server has no authentication and writes
// files on the caller's behalf — but unlike serve there is no listener at
// all, so nothing to bind beyond the process's own stdin/stdout. Writes are
// confined to the blueprint file and the --out workspace (plus the
// --cache-dir/--lock infrastructure paths add_provider maintains, exactly as
// serve's POST /api/providers does); the confinement itself is enforced in
// internal/mcp, not merely assumed here.
type MCPCmd struct {
	Blueprint string `help:"Path to the blueprint file the MCP tools read and edit." required:""`
	Out       string `short:"o" help:"Workspace directory that generate {write:true} writes into. With the blueprint file, the only path the tools can write." default:"."`
	CacheDir  string `help:"Schema cache directory." default:"${cachedir}"`
	Lock      string `help:"Lockfile path that add_provider pins newly added providers into." default:".cf.lock"`
}

// Run is MCPCmd's kong entry point. Like ServeCmd it ties the server's
// lifetime to SIGINT/SIGTERM; the usual client-driven exit is the MCP host
// closing this process's stdin, which ends the stdio transport's session.
func (c *MCPCmd) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return c.run(ctx)
}

// run does the actual work; split from Run for the same reason ServeCmd's
// is, so a test can drive shutdown by cancelling a context it controls.
func (c *MCPCmd) run(ctx context.Context) error {
	// Both workspace paths are resolved to absolute form up front: the write
	// gate compares absolute, cleaned paths (see internal/mcp/workspace.go),
	// and an MCP host launches this process with a working directory the
	// user never sees — a relative --out left relative would make the
	// confinement boundary depend on it silently.
	blueprintPath, err := filepath.Abs(c.Blueprint)
	if err != nil {
		return err
	}
	outDir, err := filepath.Abs(c.Out)
	if err != nil {
		return err
	}

	o, err := buildAPIOptions(blueprintPath, c.CacheDir, outDir, c.Lock, nil, false)
	if err != nil {
		return err
	}
	srv, err := cfmcp.New(o, version)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &sdk.StdioTransport{})
}
