# CF-089 — Adding a provider from SOURCES and then applying any full-document write silently drops that provider

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 (UX scale: lost work / silent data loss on document write) |
| **Closes** | `CF-089 — Adding a provider from SOURCES and then applying any full-document write (blueprint editor Apply, engine selector) silently drops that provider again. [V]` |
| **Worktree** | `.worktrees/CF-089` on branch `CF-089-adding-provider-from-sources-lost-on-write` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/cf089-add-provider-persists-across-writes.spec.js` (new) |
| **Merges after** | nothing |

## Symptom

A user adds a provider from the SOURCES tab (via the "+ Add" input or the catalogue "Add" button).
The server fetches the provider and writes the updated `spec.sources` to the blueprint file on disk.
However, when the user subsequently performs any full-document write in the browser (such as clicking
"Apply" in the Blueprint Editor drawer, changing the engine selector, or anything using `store.replaceDoc`),
the provider is silently dropped from the file on disk and evicted from the server's providers list.

## Evidence

Scripted repro on `f45c2a8`:
1. Start with pristine doc (contains provider-aws-sqs).
2. Add `ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0` via `POST /api/providers`.
3. `GET /api/providers` returns both sqs and s3.
4. Client editor drawer text or `store.state.doc` still lists only sqs because `store.loadDoc()` was never invoked after the add.
5. Clicking Apply in the editor PUTs `store.state.doc` without s3.
6. Server reconciles `srv.Providers` with `spec.sources`, evicting s3.
7. `GET /api/providers` returns only sqs. S3 is silently lost.

## Location

- `web-proto/js/regions/palette.js:869-879` (`#src-add-btn` handler): calls `api.addProvider(addRef)`, then `loadProviders()` and `loadKinds()`, but never synchronizes `store.state.doc` or triggers `store.loadDoc()`.
- `web-proto/js/regions/palette.js:888-903` (`button.cat-add` handler): same omission.
- `web-proto/js/regions/palette.js:832-838` (remove provider handler): same omission.

## Acceptance test

Write this test **first**, verbatim in `tests/cf089-add-provider-persists-across-writes.spec.js`, and watch it fail before changing production code:

```js
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('adding a provider from sources is not dropped by subsequent doc writes', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  const input = page.locator('#src-add-ref')
  await input.fill('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
  await page.click('#src-add-btn')

  await expect(page.locator('#region-palette')).toContainText('provider-aws-s3', { timeout: 10000 })

  // Open drawer and click Apply in the blueprint editor
  await page.click('#dtabs button[data-tab="bp"]')
  await page.click('#bp-apply-btn')

  const res = await request.get(ENGINE + '/api/providers')
  const json = await res.json()
  const refs = json.providers.map(p => p.ref)
  expect(refs).toContain('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
})
```

**Fails today with:**
```
Error: expect(received).toContain(expected)
Expected substring: "ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"
Received: ["ghcr.io/x/provider-aws-sqs:v2.7.0"]
```

## Contract

- Whenever a provider is added or removed via the SOURCES tab (both manual ref add and catalogue add, and remove), `store.loadDoc()` must be called so `store.state.doc` reflects the server's persisted blueprint sources.
- After the document is reloaded, `store.generate(false)` must run so the canvas updates its composition and clears any stale errors (also closing CF-091).
- Subsequent full-document writes (including Blueprint Editor Apply) must carry the newly updated `spec.sources` and never evict installed providers.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- Layout fixes in the blueprint editor drawer (CF-122).
- Server-side source fetch error formatting (already handled in CF-087).

## Handover

Branch `CF-089-adding-provider-from-sources-lost-on-write`, committed, not pushed, not merged. In your final report:
the failing run and the passing run of the acceptance test, both pasted; every gate
you ran; every judgement call you made where the brief was silent.
