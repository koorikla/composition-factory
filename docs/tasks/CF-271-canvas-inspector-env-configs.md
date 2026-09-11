# CF-271 — Canvas environment inspector ignores spec.environmentConfigs and desyncs pipeline step on edit

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (UX scale: environment configs blueprint IR synchronization) |
| **Closes** | `CF-271 — Canvas environment inspector ignores spec.environmentConfigs and desyncs pipeline step on edit` |
| **Worktree** | `.worktrees/CF-271` on branch `CF-271-canvas-inspector-env-configs` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/inspector.js`, `tests/cf271-canvas-inspector-env-configs.spec.js` |
| **Merges after** | `CF-272` |

## Symptom

When opening a Blueprint that declares `spec.environmentConfigs` (e.g. `[{name: "staging-env"}]`):
1. The canvas EnvironmentConfig node header and the inspector header display `EnvironmentConfig default` instead of `staging-env`, and the selection input displays `default`.
2. When the user edits the selection name in the inspector (e.g. changing it to `custom-env`), the inspector prepends/mutates an ad-hoc pipeline step inside `d.spec.pipeline`, but never updates `d.spec.environmentConfigs`.
3. This creates a direct divergence: `spec.environmentConfigs` keeps `staging-env`, while `spec.pipeline` has an inline step referencing `custom-env`.
4. Emitted files diverge: `environmentconfigs/staging-env.yaml` is written, but the Composition references `custom-env`. When applied to a cluster, Crossplane fails with: `pipeline step "environment-configs" returned a fatal result: cannot get selected environment configs: Required environment config "custom-env" not found`.

## Mechanism

1. In `web-proto/js/regions/canvas.js:168-178`, `getEnvConfigName` inspects `d.spec.pipeline` for a step named `function-environment-configs` or `environment-configs`, ignoring `d.spec.environmentConfigs`.
2. In `web-proto/js/regions/inspector.js:1035-1056`, `parseEnvSelection` only checks `doc.spec.pipeline`. When `doc.spec.environmentConfigs` is declared without an ad-hoc step in `pipeline`, it falls back to `{ mode: "Reference", name: "default", labels: "" }`.
3. In `web-proto/js/regions/inspector.js:1058-1098`, `updateEnvSelection` writes an ad-hoc pipeline step to `d.spec.pipeline` without synchronizing `d.spec.environmentConfigs`.

## Contract

1. In `web-proto/js/regions/canvas.js`:
   - Update `getEnvConfigName(d)` to first read from `d.spec.environmentConfigs` if populated.
2. In `web-proto/js/regions/inspector.js`:
   - Update `parseEnvSelection(doc)` to read from `doc.spec.environmentConfigs`.
   - Update `updateEnvSelection(doc, ...)` to synchronize the user's configuration directly into `doc.spec.environmentConfigs` (and maintain or clean `doc.spec.pipeline` so it does not inject redundant/conflicting ad-hoc pipeline steps).
3. Ensure that editing environment config reference or label selector in the inspector updates the blueprint document such that `cf gen` emits matching `environmentconfigs/*.yaml` manifests and Composition pipeline steps.

## Acceptance Test

Playwright test in `tests/cf271-canvas-inspector-env-configs.spec.js`:
1. Load canvas with a blueprint declaring `spec.environmentConfigs: [{name: "staging-env"}]`.
2. Verify canvas node and inspector render name `staging-env`.
3. Edit `#envSelName` to `production-env` and blur.
4. Verify `GET /api/blueprint` contains `spec.environmentConfigs: [{name: "production-env"}]`.
5. Trigger generation and verify emitted Composition pipeline step matches emitted EnvironmentConfig name `production-env`.

## Verification

```sh
make lint && make lint-strict && make test
CF_E2E_PORT=25715 npx playwright test tests/cf271-canvas-inspector-env-configs.spec.js
```

## Handover

Branch `CF-271-canvas-inspector-env-configs`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
