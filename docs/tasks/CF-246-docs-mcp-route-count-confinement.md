# CF-246 — docs/mcp.md: '36 HTTP routes' is 35, and the --out confinement claim omits the symlink case

## Severity & Scope
- **Severity**: P3
- **Scale**: engine
- **Touch set**: `docs/mcp.md`

## Background & Problem
In `docs/mcp.md`:
1. Line 5 claims "19 MCP tools bridging key operations from `cf serve`'s 36 HTTP routes". `internal/api/server.go` registers 35 routes, and the inventory table below lists 35 routes.
2. Lines 12-14 claim "`generate` checks every output path (absolute, cleaned, prefix) against `--out` before writing anything; a path outside it is refused with no files touched." As documented in `internal/mcp/workspace.go:41-47`, symlinks are deliberately not resolved, so symlinks placed inside `--out` are outside this threat model.

## Expected Behavior & Contract
1. Update `docs/mcp.md` to state 35 HTTP routes instead of 36.
2. Update the confinement description to clarify that path confinement checks lexical prefixes without resolving symlinks.
