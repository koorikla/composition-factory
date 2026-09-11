# CF-253 — replace_blueprint from a stale snapshot silently overwrites a concurrent client's persisted edit

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (scale: engine) |
| **Closes** | `CF-253 — replace_blueprint from a stale snapshot silently overwrites a concurrent client's persisted edit` (#122) |
| **Worktree** | `.worktrees/CF-253` on branch `CF-253-mcp-replace-concurrency` |
| **May write** | `internal/mcp/`, `internal/api/` |
| **Merges after** | nothing |

## Symptom

`PUT /api/blueprint` and the `mutate` helper honour `If-Match` only when the header is present (`internal/api/blueprint.go:146`, `:275`); the MCP bridge never sends one (`internal/mcp/bridge.go:42-44` deliberately sends no conditional headers) and `get_blueprint` returns the document with no ETag/revision, so an MCP client has no way to make a conditional replace. `replace_blueprint` is described as "send the complete document in the exact shape get_blueprint returns" — a read-modify-write with no precondition.

When two MCP clients (or an MCP client and a canvas user) edit the same blueprint, calling `replace_blueprint` with a stale snapshot silently erases intermediate edits without warning.

## Contract

1. MCP clients must have a mechanism for optimistic concurrency control on `replace_blueprint`.
2. `get_blueprint` returns revision / ETag information (either in the tool result metadata or as a `revision` / `metadata.resourceVersion` field).
3. `replace_blueprint` accepts an optional `revision` (or `if_match`) argument in its input schema. When provided, if the revision does not match the server's current blueprint revision, `replace_blueprint` is refused with a Precondition Failed error (status 412 / isError: true) naming the revision mismatch.
4. Backward compatibility: if `revision` is omitted, the call proceeds unconditionally (or existing workflows without revision continue to function).

## Acceptance Test

An automated unit/integration test in `internal/mcp/server_test.go` or `cmd/cf/mcp_test.go`:
`TestMCP_ReplaceBlueprint_StaleRevisionRejected`
1. Call `get_blueprint` to obtain initial state and revision.
2. Call `add_parameter` to modify the blueprint (advancing its revision).
3. Call `replace_blueprint` passing the initial blueprint with the old revision.
4. Assert that `replace_blueprint` returns an error (`isError: true` or error text containing precondition failed / revision mismatch) and the intermediate parameter is preserved.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-253-mcp-replace-concurrency`, committed, not pushed, not merged.
