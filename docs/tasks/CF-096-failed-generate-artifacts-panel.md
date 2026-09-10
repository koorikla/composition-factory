# CF-096 — While generation has failed (error, 0 lines) the ARTIFACTS panel still announces 6 files with tabs for composition, definition, functions, package and rbac

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `CF-096 — While generation has failed (error, 0 lines) the ARTIFACTS panel still announces 6 files with tabs for composition, definition, functions, package and rbac.` |
| **Worktree** | `.worktrees/CF-096` on branch `CF-096-failed-generate-artifacts-panel` |
| **May write** | `web-proto/js/store.js`, `web-proto/js/regions/output.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When blueprint generation fails (e.g. on the first load of a blueprint with an uncached source, or after an edit that fails generation):
- The header chip shows `error`.
- The code viewer shows `0 lines · deterministic`.
- However, the output drawer's file explorer still announces `6 files` (or more) in the tree count header, and renders tabs/tree items for `composition.yaml`, `definition.yaml`, `functions.yaml`, `package.yaml`, and `rbac` as if they were successfully generated and exist on disk.
- Clicking any of these tabs shows empty or broken content because the artifacts do not exist.

A failed generate must empty or grey the artifact list rather than list files that do not exist.

## Contract

1. **Store Clears or Invalidates `lastGenerate` on Error**:
   - In `web-proto/js/store.js`:
     When `generate(write)` catches an error, clear `this.state.lastGenerate = null` (or record generation failure state) so stale or ghost outputs from prior runs are not retained as valid generated files.

2. **Output Region Reflects Failed Generation State**:
   - In `web-proto/js/regions/output.js`:
     - When generation has failed (or `lastGenerate` is null / generation errored):
       - Generated engine artifacts (`comp`, `xrd`, `fns`, templates, providerconfigs) must either be greyed out/disabled (with `aria-disabled="true"` / disabled styling) or emptied from the list.
       - The tree explorer file count must not announce non-existent files (e.g. only counting the blueprint file `1 file` or `0 generated files`).
       - Generated tabs in `#tabs` must be disabled or greyed out when generation has failed.
       - If the user was viewing a generated tab that failed to generate, the viewport should display clear error feedback or fall back to the blueprint tab.

3. **Verification Gates**:
   - `make lint` passes.
   - `make lint-strict` passes.
   - `make test-race` passes.
   - `make test-e2e` passes.

## Acceptance Test

Write Playwright test `tests/cf096-failed-generate-artifacts.spec.js`:
1. Modify the blueprint to introduce a generate failure (e.g. set an invalid engine or syntax that causes `POST /api/generate` to return 400).
2. Wait for generate to fail (status chip shows `error`).
3. Verify that the output drawer file explorer does not announce 6 generated files.
4. Verify that non-existent generated artifact tabs/tree items are greyed out/disabled.
