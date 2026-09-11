# CF-228 — MCP generate tool with write:true fails to prune orphaned files in managed output directories

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/mcp/tools.go`, `internal/mcp/server_test.go`, `docs/tasks/CF-228-mcp-generate-prune.md`

## Background & Problem
`internal/mcp/tools.go:580-595` (`writeOutputs`) writes generated output files directly via `os.WriteFile`, but omits pruning orphaned files. In contrast, `cmd/cf/gen.go:161` and `internal/api/generate.go:125` both prune stale files residing within managed output scopes (`compositions/`, `xrds/`, `providerconfigs/`, `runtime/`, `templates/`, `environmentconfigs/`, `functions.yaml`, `rbac.yaml`) using `emit.PruneOrphanedFiles`. Consequently, running MCP `generate` with `{"write": true}` leaves orphaned files on disk after resource or XRD renames, violating the contract that MCP generation matches `cf gen` and the HTTP API.

### Root Cause
In `internal/mcp/tools.go:580-595`:
```go
func (s *server) writeOutputs(outputs []generateOutput) error {
	for _, out := range outputs {
		if err := s.ws.check(out.Path); err != nil {
			return err
		}
	}
	for _, out := range outputs {
		if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out.Path, []byte(out.Body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
```
`writeOutputs` does not invoke `emit.PruneOrphanedFiles(s.ws.out, ...)`.

## Expected Behavior & Contract
1. In `internal/mcp/tools.go`:
   - After successfully writing the output files in `writeOutputs`, construct the expected files map:
     ```go
     expected := make(map[string]bool, len(outputs))
     for _, out := range outputs {
         expected[out.Path] = true
     }
     if _, err := emit.PruneOrphanedFiles(s.ws.out, expected); err != nil {
         return err
     }
     ```
   - Any stale files residing within managed output scopes are pruned on disk when `write: true`.

## Acceptance Test
- In `internal/mcp/server_test.go`:
  - `TestGenerateWritePrunesOrphanedFiles`:
    1. Create a `stack` with `s.outDir`.
    2. Seed an orphaned file in a managed output scope, e.g., `filepath.Join(s.outDir, "xrds", "stale-xrd.yaml")`.
    3. Call the `generate` MCP tool with `{"write": true}`.
    4. Assert that the call succeeds (`written == true`), the valid engine outputs exist on disk, and the stale file `stale-xrd.yaml` has been pruned.
