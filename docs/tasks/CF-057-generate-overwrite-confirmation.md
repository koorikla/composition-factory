# CF-057 — Generate overwrites files on disk and nothing says so beforehand

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-057 — Generate overwrites files on disk and nothing says so beforehand.` |
| **Worktree** | `.worktrees/CF-057` on branch `CF-057-generate-overwrite-confirmation`, branched from `main` |
| **May write** | `web-proto/index.html`, `web-proto/js/regions/output.js`, `internal/api/generate.go`, `internal/api/version.go`, `tests/` |
| **Merges after** | nothing |

## Symptom

The Generate button tooltip (`web-proto/index.html:30`) says:
`"Regenerate composition.yaml and definition.yaml from the blueprint now"`
When clicked, the handler (`generateNow` -> `store.generate(true)`) issues `POST /api/generate {"write": true}`.
The server writes every output path directly into `srv.OutDir` via `os.WriteFile`, with no confirmation, no destination path named in the tooltip, and no indication that existing files in `out` will be overwritten.
The word "write" only appears afterwards, in the output drawer banner or chip.

## Requirements

1. **Naming the Destination & Overwrite**:
   - The Generate button tooltip must explicitly state the output destination and that files will be written/overwritten (e.g., `"Write generated manifests to disk (overwrites files in output directory)"`).
   - If `/api/version` returns `outDir` (or from server state), display the output path in the tooltip (e.g., `Write generated manifests to ./out (overwrites existing files)`).
2. **Confirmation / Pre-flight**:
   - In `generateNow()` (`web-proto/js/regions/output.js`), before calling `store.generate(true)`, provide a clear user confirmation dialog or confirmation modal if files exist or naming the destination (or an explicit confirmation modal/window.confirm: `"Generate will write manifests to disk in '" + outDir + "', overwriting existing files.\n\nProceed?"` or if already confirmed in session).
3. **Automated E2E Test**:
   - Add a test verifying that the Generate button tooltip clearly warns of file overwrite and specifies the destination, and that clicking Generate handles confirmation.

## Verification

```sh
npm run lint:js
make lint
make test
make test-e2e
```
