# CF-090 — The Generate toast and apply instructions name only `compositions/`, omitting XRDs, functions, and provider configs

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 (UX scale: the interface states something false; following instructions leaves cluster broken) |
| **Closes** | `CF-090 — The Generate toast says Output written to compositions · Apply: kubectl apply -f compositions while three of the four files land outside compositions/. [V]` |
| **Worktree** | `.worktrees/CF-090` on branch `CF-090-generate-toast-names-real-dir-and-files` |
| **May write** | `internal/api/generate.go`, `web-proto/js/regions/output.js`, `tests/cf090-generate-toast-covers-all-outputs.spec.js` (new) |
| **Merges after** | `CF-089` (shared `web-proto/` area) |

## Symptom

When a user clicks "Generate" (writing manifests to disk), the notification toast and next-steps banner announce:
`Output written to compositions · Apply: kubectl apply -f compositions · Package: cf package`

However, generation writes four artifacts across different directories:
1. `compositions/<xrd>.yaml`
2. `xrds/<xrd>.yaml`
3. `functions.yaml`
4. `providerconfigs/<provider>.yaml`

Following the command instructed in the UI (`kubectl apply -f compositions`) applies the Composition *without* its XRD, functions, or provider configuration, causing Crossplane reconciliation to fail on the cluster. Furthermore, the button tooltip and confirmation modal say destination is `.`, without disclosing the real workspace output directory.

## Evidence

On `f45c2a8`:
`POST /api/generate` with `{write: true}` returns `outputs` with paths:
- `compositions/xqueues.platform.sparky.ee.yaml`
- `functions.yaml`
- `xrds/xqueues.platform.sparky.ee.yaml`
- `providerconfigs/aws.yaml`

In `web-proto/js/regions/output.js:440-446`:
`first = result.outputs[0].path` (`compositions/...`)
`outPath = parts.join("/")` -> `"compositions"`.
The toast at line 452 outputs: `Output written to compositions · Apply: kubectl apply -f compositions`.
Three of the four written files are outside `compositions/`.

## Location

- `internal/api/generate.go:126-130`: `handleGenerate` writes outputs to `srv.OutDir`, but only returns `outputs` and `written`. It does not return `outDir` in the response JSON.
- `web-proto/js/regions/output.js:440-466`: `updateNextSteps` derives `outPath` from `result.outputs[0].path`, which evaluates to `"compositions"`.
- `web-proto/js/regions/output.js:747-750`: confirm prompt references `(outDir || "output directory")`.

## Acceptance test

Write this test **first**, verbatim in `tests/cf090-generate-toast-covers-all-outputs.spec.js`, and watch it fail before changing production code:

```js
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('generate banner and toast instruct an apply command that covers all written files', async ({ page }) => {
  await page.goto('/')
  // Accept the generate confirmation dialog
  page.on('dialog', dialog => dialog.accept())

  await page.click('#dtabs button[data-tab="comp"]')
  await page.click('#gen-btn')

  // The output next-steps banner must be visible
  const banner = page.locator('#out-next-steps')
  await expect(banner).toBeVisible()

  // Must not claim output is only written to "compositions"
  await expect(banner).not.toContainText('Output written to compositions')
  // The apply instruction must cover all written manifests recursively or at root
  await expect(banner).toContainText('kubectl apply -R -f')
})
```

**Fails today with:**
```
Error: expect(locator).not.toContainText(expected)
Locator: locator('#out-next-steps')
Expected substring: not "Output written to compositions"
Received string: "✓ Output written to compositions · Apply: kubectl apply -f compositions · Package: cf package"
```

## Contract

- `POST /api/generate` response must include `"outDir": srv.OutDir`.
- When manifests are written, the banner and toast must name the real target directory (`outDir` or `.` if current directory).
- The suggested apply command must apply recursively across the output directory (`kubectl apply -R -f <outDir>` or `kubectl apply -R -f .`), ensuring that XRDs, functions, Compositions, and provider configs are all applied.
- The button tooltip and confirmation modal must clearly show the target directory.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- Changing Crossplane packaging logic (`cf package`).

## Handover

Branch `CF-090-generate-toast-names-real-dir-and-files`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
