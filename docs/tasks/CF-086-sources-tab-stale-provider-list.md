# CF-086 — The SOURCES tab keeps showing the provider list it fetched first, even after the document's sources change

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 (UX scale: the interface states something false) |
| **Closes** | `CF-086 — The SOURCES tab shows the provider list it fetched first; loading a starter example (or any doc change that swaps sources) leaves it stale until a page reload. [V]` |
| **Worktree** | `.worktrees/CF-086` on branch `CF-086-sources-tab-stale-provider-list` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/cf086-sources-tab-tracks-doc.spec.js` (new) |
| **Merges after** | nothing |

## Symptom

A user opens the SOURCES tab, then loads the "AWS RDS PostgreSQL" starter (or any other
starter, or saves an Edit-blueprint change that swaps `spec.sources`). The canvas shows the
RDS cards, the server serves provider-aws-rds, but "Installed Providers" still lists
whatever the tab showed before — provider-aws-s3 in the user's report, provider-aws-sqs
in the pristine test doc, or only `k8s` on a blank start. The catalogue's "installed"
badges are computed from the same stale list, so the same provider can show as
not-installed while its kinds sit on the canvas. Only a full page reload corrects it.

The user reported this as "CF-082 is still not fixed". CF-082 was server-side and is
fixed; this is the client caching the old answer.

## Evidence

Server side is correct (two runs, `bin/cf` at `f45c2a8`, scratch dir, rds+s3 cached):

```sh
$ curl -s -X POST $U/api/examples/s3-bucket/load >/dev/null; curl -s $U/api/providers
{"providers":[{"ref":"ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0","digest":"sha256:0814e9…","kinds":50}]}
$ curl -s -X POST $U/api/examples/rds-postgres/load >/dev/null; curl -s $U/api/providers
{"providers":[{"ref":"ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0","digest":"sha256:16c8fb…","kinds":44}]}
```

Browser, same binary on 8090, blank start: click SOURCES (shows "Installed Providers 1 —
k8s"), Examples → Load Blueprint on the RDS card. Canvas shows XPostgresInstance + Instance;
`curl /api/providers` lists provider-aws-rds; SOURCES still says "Installed Providers 1 —
k8s". Reload → correct.

The acceptance spec below, run twice from a worktree at `f45c2a8`, fails both times with
the rail still holding the pristine doc's provider while the server serves rds.

## Location

- `web-proto/js/regions/palette.js:58` — `let providers = null; // server-side cached providers, null = not loaded`.
- `palette.js:137` and `:727` — `if (providers === null) loadProviders();` — the list is fetched only while it is `null`.
- `palette.js:174-179` — `loadProviders()` sets it and never clears it on success.
- `palette.js:401-403` — the Sources view prefers `providers` over `doc.spec.sources` whenever it is non-null, so a stale cache wins over the live document.
- `palette.js:1088-1097` — the `"doc"` subscription recomputes `lastSourcesSig` and reloads kinds when sources change, but does not touch `providers`.
- The only invalidations are `palette.js:755` (CRD file add) and the add/remove-provider handlers around `:831-901`. Example load (`store.loadExample`), `PUT /api/blueprint`, Edit-blueprint save, undo/redo, and adopt all go through the `"doc"` emit and invalidate nothing.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any
production code. It is the definition of done; do not paraphrase it, do not weaken
an assertion to make it pass, and do not delete it if it turns out to be
inconvenient - if it is wrong, say so in the handover and stop.

```js
// tests/cf086-sources-tab-tracks-doc.spec.js
// CF-086 — The SOURCES tab keeps showing the provider list it fetched first,
// even after the document's sources change (loading a starter example here).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('sources tab lists the providers the server serves after a starter example is loaded', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  await page.click('#examplesBtn')
  await page.locator('button[data-load-id="rds-postgres"]').click()
  await expect(page.locator('#examplesOverlay')).toBeHidden()
  await expect(page.locator('.node[data-id="db-instance"]')).toBeVisible({ timeout: 15000 })

  const served = (await (await request.get(ENGINE + '/api/providers')).json()).providers.map(p => p.ref)
  expect(served).toContain('ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0')

  const rail = page.locator('#region-palette')
  await expect(rail).toContainText('provider-aws-rds', { timeout: 5000 })
  for (const ref of served) await expect(rail).toContainText(ref.split('/').pop().split(':')[0])
  await expect(rail).not.toContainText('provider-aws-sqs')
})
```

**Fails today with** (two runs, worktree at `f45c2a8`):

```
Error: expect(locator).toContainText(expected) failed
Locator: locator('#region-palette')
- Expected substring  -  1
+ Received string     + 13
- provider-aws-rds
+ … ProvidersFunctionsClusterInstalled Providers2+ Add CRDs from fileprovider-aws-sqs:v2.7.0sha256:2cfadf9f941f · 8 kindsk8s · 16 kindsAdd …
  1 failed
```

## Contract

- After any change of the live document — starter example load, `PUT /api/blueprint`,
  Edit-blueprint save, adopt/import, undo, redo — the SOURCES tab lists exactly the
  providers `GET /api/providers` returns for that document, with their kind counts, plus
  the native `k8s` row. No entry from a previous document survives.
- The catalogue's installed/not-installed state derives from that same fresh list.
- The native `k8s` row's kind count follows the same rule: after a starter load and after undo it must read the live count (J2 saw `k8s · 1 kinds` persist after undoing back to a document the server reported byte-identical to the one that showed `16 kinds`).
- The existing fallback (when `/api/providers` is unreachable the tab shows
  `doc.spec.sources`) keeps working.
- No extra `/api/providers` request when the document's sources did not change; the
  existing sources-signature guard in the `"doc"` subscription is the natural place.
- The full `make test-e2e` suite stays green; no spec weakened.

## Verification

```sh
make lint && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- Server-side reconciliation of `srv.Providers` with `spec.sources` (CF-082, archived).
- What the canvas shows when a declared source cannot be loaded (CF-087, CF-088).

## Handover

Branch `CF-086-sources-tab-stale-provider-list`, committed, not pushed, not merged. In
your final report: the failing run and the passing run of the acceptance test, both
pasted; every gate you ran; every judgement call you made where the brief was silent.
