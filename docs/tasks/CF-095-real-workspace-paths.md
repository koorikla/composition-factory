# CF-095 — The top bar and drawer show paths that do not exist: blueprints/<name>.cf.yaml and compositions/<name>.yaml, while the real files are <served path> and compositions/<xrd plural>.<group>.yaml

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-095 — The top bar and drawer show paths that do not exist: blueprints/<name>.cf.yaml and compositions/<name>.yaml, while the real files are <served path> and compositions/<xrd plural>.<group>.yaml.` |
| **Worktree** | `.worktrees/CF-095` on branch `CF-095-real-workspace-paths` |
| **May write** | `internal/api/`, `web-proto/`, `tests/` |
| **Merges after** | nothing |

## Symptom

The top bar and output drawer breadcrumbs display paths that do not match the real filesystem files:
1. `web-proto/js/regions/output.js` synthesises the top bar crumb as:
   `blueprints/<b><metadata.name>.cf.yaml</b>`
   even when `cf serve` was started with a different file (e.g. `./my-blueprint.yaml` or `.testrun/doc.cf.yaml`). The real served file path is never fetched or shown.
2. In the output drawer tree explorer (`buildTree()`):
   - The composition path is synthesised as `compositions/<metadata.name>.yaml` instead of the real generated path `compositions/<plural>.<group>.yaml`.
   - The definition path is synthesised as `xrds/<metadata.name>.yaml` instead of the real generated path `xrds/<plural>.<group>.yaml`.
   - The blueprint path is synthesised as `blueprints/<metadata.name>.cf.yaml`.

When users copy these paths or check their workspace, the synthesised files do not exist.

## Contract

1. **Backend Exposes Real Served Blueprint Path**:
   - `GET /api/version` returns `blueprint: string` containing the served blueprint path (cleaned and made relative to the server's working directory if within it, otherwise clean path).

2. **Output Drawer Uses Real Generated & Served Paths**:
   - In `buildTree()`:
     - For `comp`: use the real output path from generation (matched via `matchOutput("comp")` or derived from `doc.spec.xrd.plural + "." + doc.spec.xrd.group + ".yaml"`).
     - For `xrd`: use the real output path from generation (matched via `matchOutput("xrd")` or derived from `doc.spec.xrd.plural + "." + doc.spec.xrd.group + ".yaml"`).
     - For `bp`: use the real served blueprint path (from `/api/version`).
     - Tree items and drawer breadcrumb (`#ebPath`) must display the real paths.

3. **Top Bar Crumb Reflects Real Blueprint Path**:
   - `drawTopbar(doc)` formats `el.crumb` using the real served blueprint path (e.g. `dir/<b>filename</b>` if in a subdirectory, or `<b>filename</b>` if in the root), falling back gracefully to `<name>.cf.yaml` if not yet loaded.

4. **Verification Gates**:
   - `make lint` passes.
   - `make lint-strict` passes.
   - `make test-race` passes.
   - `make test-e2e` passes.

## Acceptance Test

Write Playwright test `tests/cf095-real-workspace-paths.spec.js`:
1. Start canvas with a served blueprint path (e.g. `doc.cf.yaml` or `.testrun-*/doc.cf.yaml`).
2. Verify `#crumb` displays the real served blueprint path (e.g. `doc.cf.yaml`), not `blueprints/...`.
3. Select composition in the drawer tree; verify `#ebPath` displays `compositions/<plural>.<group>.yaml` (e.g. `compositions/xqueues.example.org.yaml` or matching the actual output path).
4. Select definition in the drawer tree; verify `#ebPath` displays `xrds/<plural>.<group>.yaml`.
