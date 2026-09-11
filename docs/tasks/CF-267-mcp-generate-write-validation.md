# CF-267 — MCP generate write delegates with write:false, bypassing CheckRequiredFields and writing draft files

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: MCP tool parity with CLI/API required field validation) |
| **Closes** | `CF-267 — MCP generate write delegates with write:false, bypassing CheckRequiredFields and writing draft files` |
| **Worktree** | `.worktrees/CF-267` on branch `CF-267-mcp-generate-write-validation` |
| **May write** | `internal/mcp/tools.go`, `internal/api/generate.go`, `internal/mcp/tools_test.go` |
| **Merges after** | `CF-266` |

## Symptom

The MCP tool `generate` with `{"write": true}` bypasses CRD required-fields validation (`emit.CheckRequiredFields`) and writes invalid compositions to disk with unconfigured resources, even though both the CLI (`cf gen`) and the HTTP API (`POST /api/generate` with `{"write": true}`) reject the exact same blueprint with an error.

This violates `AGENTS.md` Section 1:
- "All emission of Crossplane artifacts is implemented strictly in `internal/emit`. The CLI, HTTP API, and MCP server are thin bridges calling `internal/emit`. Never create duplicate or parallel emission logic. Every interface must generate 100% byte-identical, deterministic YAML."
- "Strict CRD Schema Validation: Provider CRD OpenAPI schemas (`spec.forProvider`) are authoritative. Field paths and types must match the schema. Invalid or unknown field paths must fail loudly with nearest-match suggestions rather than silently dropping fields at deploy time."

## Mechanism

1. In `internal/mcp/tools.go:629-637`, MCP's `generate` tool intentionally calls `POST /api/generate` with `{"write": false}` so that MCP can check output paths against its workspace boundary before writing:
   ```go
   func (s *server) generate(_ context.Context, _ *sdk.CallToolRequest, in generateInput) (*sdk.CallToolResult, any, error) {
       status, body, _, err := s.call(http.MethodPost, "/api/generate", []byte(`{"write":false}`))
   ```
2. In `internal/api/generate.go:104-108`, the HTTP API equates `!req.Write` with draft preview mode:
   ```go
       var genOpts []emit.GenerateOption
       if !req.Write {
           genOpts = append(genOpts, emit.WithDraftPreview())
       }
       outputs, err := emit.Generate(b, crds, srv.OutDir, genOpts...)
   ```
3. In `internal/emit/emit.go:55-63` and `internal/emit/plan.go:146-149`, `WithDraftPreview()` switches validation to `CheckRequiredFieldsDraft`, which skips required field validation for any resource with `len(r.Fields) == 0`.
4. As a result, MCP `generate` with `write: true` triggers `WithDraftPreview()`, receives incomplete/invalid outputs, passes workspace checks, writes unconfigured resources to disk via `s.writeOutputs(resp.Outputs)`, and returns `{"written": true}`, bypassing the validation enforced by `cf gen` and direct `POST /api/generate {"write": true}`.

## Contract

1. In `internal/api/generate.go`:
   - Decouple draft preview mode from `!req.Write`. Introduce an explicit request field (e.g. `DraftPreview bool` or query flag) or only enable `WithDraftPreview()` when explicitly requested for canvas live preview.
   - When `req.Write == false` and `DraftPreview == false` (e.g. validation-only generation check), execute full production `CheckRequiredFields` validation.
2. In `internal/mcp/tools.go`:
   - Ensure `s.generate(...)` invokes `/api/generate` in non-draft mode (or validates required fields) so that blueprints missing required fields return an error and write nothing to disk.
3. Verify that calling MCP `generate` with `write: true` on an unconfigured resource fails with an error and does not write draft compositions to disk.

## Acceptance Test

Go unit test in `internal/mcp/tools_test.go`:
1. Call MCP `generate` with `write: true` on a blueprint containing an unconfigured AWS SQS `Queue` resource (`fields: {}`).
2. Verify that `generate` returns an error naming missing required field `region` and writes zero files into the workspace output directory.
3. Call MCP `generate` with `write: true` on a fully valid blueprint with all required fields present; verify files are written cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-267-mcp-generate-write-validation`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
