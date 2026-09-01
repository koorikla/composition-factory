// Package mcp is the compositionfactory MCP server: the same authoring
// operations the HTTP API serves, exposed as MCP tools for an agent over
// stdio. It is built with the official MCP Go SDK
// (github.com/modelcontextprotocol/go-sdk), which owns the JSON-RPC framing
// and protocol-version negotiation.
//
// DESIGN: every tool is a thin bridge over the exact http.Handler api.New
// builds — a tool call becomes an in-process HTTP request against that
// handler, and the tool's result is that request's response body. This is
// deliberate, and it is the whole architecture of this package: the design
// spec's load-bearing rule (§3) is that HTTP and MCP are thin adapters over
// internal/emit, never parallel implementations. Re-implementing the
// operations here — even by extracting internal/api's handler cores into
// shared functions — would create a second copy of every status
// classification, every lost-update lock, every DisallowUnknownFields decode
// and every verbatim error message, all of which could then drift. Driving
// the one existing handler in process makes drift structurally impossible:
// the MCP caller passes the same validation gate and reads the same verbatim
// error strings as a browser on /api, because they are the same bytes from
// the same code.
//
// Error shape: a response with an HTTP error status surfaces its {"error":
// "..."} message VERBATIM as the tool error (the SDK reports it to the agent
// as an isError result with that text as content). The messages name the
// offending field path precisely — see internal/api's package comments —
// and paraphrasing or prefixing them here would throw that away.
//
// WRITES are confined to the declared workspace. The blueprint file and the
// --out directory are the only paths this server ever writes:
//
//   - Blueprint edits (replace_blueprint, the parameter tools) happen inside
//     internal/api's handlers, which write only to the blueprint path the
//     server was started with. No tool accepts a path.
//   - Generated output is gated here: generate always renders as a dry run
//     first, every returned path is checked against the --out workspace
//     (absolute, cleaned, prefix — see workspace.go), and only then are the
//     files written. blueprint.Validate's identifier rules already make an
//     escaping output path unconstructible (plural and group cannot contain
//     path separators or ".."), so the gate is defense in depth — but it is
//     the layer an agent-driven server must not delegate to a regex two
//     packages away.
//   - add_provider additionally writes the schema cache and the lockfile,
//     exactly as POST /api/providers does. Those live at fixed paths the
//     operator chose at launch (--cache-dir, --lock); no tool input can
//     redirect them, so they are server infrastructure like the blueprint
//     path, not agent-reachable write targets.
package mcp

import (
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koorikla/compositionfactory/internal/api"
)

// New builds the compositionfactory MCP server over o — the same Options
// api.New takes, because api.New is exactly what it wires the tools to.
// version is the cf build version, reported to the client during initialize.
//
// Concurrency: the SDK may dispatch tool calls concurrently. Every call
// bridges into the one api handler, whose own mutex already serializes
// blueprint mutations (see internal/api's server.mu). generate's
// render-then-write sequence is not under that lock, so a concurrent
// blueprint edit between the two steps could produce output of the earlier
// document — the same benign race a `cf gen` racing a hand edit has, and
// accepted for the same reason: output is fully regenerable, and `cf gen
// --check` reports exactly this as drift.
func New(o api.Options, version string) (*sdk.Server, error) {
	handler, err := api.New(o)
	if err != nil {
		return nil, err
	}
	ws, err := newWorkspace(o.OutDir)
	if err != nil {
		return nil, err
	}

	s := &server{handler: handler, ws: ws}
	srv := sdk.NewServer(&sdk.Implementation{
		Name:    "compositionfactory",
		Title:   "compositionfactory",
		Version: version,
	}, nil)
	s.register(srv)
	return srv, nil
}

// server carries what every tool handler needs: the one API handler the
// bridge drives, and the write gate for generated output.
type server struct {
	handler http.Handler
	ws      workspace
}
