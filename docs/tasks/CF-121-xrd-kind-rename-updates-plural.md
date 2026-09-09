# CF-121 — Renaming the XRD kind in the inspector leaves plural stale and plural is not editable anywhere

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (UX scale: completable only by hand-editing raw YAML; generates files named after kind that no longer exists) |
| **Closes** | `CF-121 — Renaming the XRD kind in the inspector leaves plural at old value, the plural is shown as static text and is editable nowhere, so Generate names every file and the Composition after a kind that no longer exists.` |
| **Worktree** | `.worktrees/CF-121` on branch `CF-121-xrd-kind-rename-updates-plural` |
| **May write** | `web-proto/js/regions/inspector.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

In the canvas UI, selecting the XRD card and renaming `kind` (e.g. from `XApp` to `XPostgres`) in the inspector updates `spec.xrd.kind`.
However, `spec.xrd.plural` remains stuck at its old value (`xapps`).
The subtitle in the inspector displays `xapps.platform.example.org` as static text, and the plural is editable nowhere in the canvas UI.
When the user subsequently clicks Generate, every generated output file and the Composition itself are named after the stale plural (`xapps.platform.example.org.yaml`), producing manifests for a kind that no longer exists.

## Evidence

In Journey J2 finding F3 (`docs/ux-runs/2026-09-09-canvas-xpostgres-build.md`):
1. Click XRD card -> inspector kind field -> change `XApp` to `XPostgres`, press Enter.
2. Inspector subtitle stays `xapps.platform.example.org · v1alpha1`.
3. Subtitle is static text (`document.activeElement = BODY`).
4. `/api/blueprint` shows `"kind":"XPostgres","plural":"xapps"`.
5. Generate writes `out/compositions/xapps.platform.example.org.yaml` and `out/xrds/xapps.platform.example.org.yaml`.
6. Composition has `metadata.name: xapps.platform.example.org` but `compositeTypeRef.kind: XPostgres`.

## Location

- `web-proto/js/regions/inspector.js:2057-2064`:
  ```js
  var xrdFieldUpdaters = {
    xk: function (t) {
      var kv = t.value.trim();
      if (!kv) { render(); return; }
      op(function () {
        return store.replaceDoc(function (d) { d.spec.xrd.kind = kv; });
      }).then(function (r) { if (r === null) render(); });
    },
  ...
  ```
  Updating `xk` only assigns `d.spec.xrd.kind = kv`. If the previous plural was the derived plural of the old kind (or if re-deriving is expected when plural matches the pluralized form of the old kind, or re-deriving default plural whenever kind changes), plural is never updated.
- Note `inferPlural` or pluralization helper in `internal/adopt/adopt.go` or standard lowercase pluralization `kind.toLowerCase() + "s"` / plural rules. In JS, check if `pluralOf` or similar exists in web-proto or store.

## Acceptance test

Write this test **first**, verbatim in `tests/cf121-xrd-kind-rename-updates-plural.spec.js`, and watch it fail before changing production code:

```js
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('renaming XRD kind re-derives plural and updates subtitle', async ({ page, request }) => {
  await page.goto('/')
  // Select XRD card
  await page.click('#node-xrd')
  const kindInput = page.locator('#xk')
  await expect(kindInput).toBeVisible()

  await kindInput.fill('XPostgres')
  await kindInput.press('Enter')

  // Inspector subtitle should update from xapps.platform.example.org to xpostgreses (or xpostgres.platform.example.org)
  const subtitle = page.locator('.insp-t .g')
  await expect(subtitle).not.toContainText('xapps.')

  const res = await request.get(ENGINE + '/api/blueprint')
  const bp = await res.json()
  expect(bp.spec.xrd.kind).toBe('XPostgres')
  expect(bp.spec.xrd.plural).not.toBe('xapps')
})
```

## Contract

- In `web-proto/js/regions/inspector.js`:
  When the XRD `kind` input (`#xk`) is changed, re-derive the default plural for the new kind (e.g. if the existing plural was default/empty or matched the plural of the old kind, or re-derive via standard pluralization such as lowercase + 's' / 'es').
  Update `d.spec.xrd.plural` alongside `d.spec.xrd.kind`.
  Render the updated plural in the inspector subtitle immediately.
- The server `/api/blueprint` must persist the updated `plural`.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- Custom plural editor inputs (can be added later if needed; automatic re-derivation on rename solves the defect).

## Handover

Branch `CF-121-xrd-kind-rename-updates-plural`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran.
