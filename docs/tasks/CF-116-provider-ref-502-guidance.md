# CF-116 — A mistyped provider ref in SOURCES shows `Server unavailable (HTTP 502 Bad Gateway) ... The backend server may be restarting or unreachable`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: misleading error states something false about backend server availability) |
| **Closes** | `CF-116 — A mistyped provider ref in SOURCES shows Server unavailable (HTTP 502 Bad Gateway): fetch "…": GET https://ghcr.io/v2/…: MANIFEST_UNKNOWN … The backend server may be restarting or unreachable.` |
| **Worktree** | `.worktrees/CF-116` on branch `CF-116-provider-ref-502-guidance` |
| **May write** | `internal/api/providers.go`, `web-proto/js/api.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When a user enters a nonexistent or mistyped provider ref in the SOURCES tab (e.g. `ghcr.io/nope/does-not-exist:v0.0.1`), `internal/api/providers.go:202` returns HTTP 502 Bad Gateway with:
`{"error":"fetch \"ghcr.io/nope/does-not-exist:v0.0.1\": GET https://ghcr.io/v2/nope/does-not-exist/manifests/v0.0.1: MANIFEST_UNKNOWN: manifest unknown"}`.

In the browser client, `web-proto/js/api.js:67-72` labels every 502/503/504 as:
```
Server unavailable (HTTP 502 Bad Gateway): fetch "ghcr.io/...": ... The backend server may be restarting or unreachable.
```
The advice is completely false: the `cf serve` backend server is running fine and fully reachable; it is the upstream registry fetch that failed because the package was not found or the ref was invalid. Furthermore, the raw internal Go registry error reaches the canvas user directly.

## Evidence

Documented in `docs/comp-runs/2026-09-09-fix-verification.md:179-185`:
```
POST /api/providers {"ref":"ghcr.io/nope/does-not-exist:v0.0.1"} →
{"error":"fetch \"ghcr.io/nope/does-not-exist:v0.0.1\": GET https://ghcr.io/v2/nope/does-not-exist/manifests/v0.0.1: MANIFEST_UNKNOWN: manifest unknown"} http=502

UI (#region-palette .warnbar):
Server unavailable (HTTP 502 Bad Gateway): fetch "ghcr.io/nope/does-not-exist:v0.0.1": GET https://ghcr.io/v2/nope/does-not-exist/manifests/v0.0.1: MANIFEST_UNKNOWN: manifest unknown. The backend server may be restarting or unreachable.
```

## Acceptance Test

Write this test in a new spec `tests/cf116-provider-ref-error.spec.js`:

```js
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('mistyped provider ref in SOURCES shows package not found guidance, not backend server restarting', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#src-add-input', 'ghcr.io/nope/does-not-exist:v0.0.1')
  await page.click('#src-add-btn')

  const alert = page.locator('#region-palette [role="alert"], #region-palette .warnbar, #toast').first()
  await expect(alert).toBeVisible({ timeout: 15000 })
  const text = await alert.textContent()
  expect(text).not.toMatch(/backend server may be restarting/i)
  expect(text).toMatch(/not found|cannot find package|package.*not found|manifest unknown/i)
})
```

## Contract

- When a provider cannot be fetched due to registry lookup failure (`MANIFEST_UNKNOWN`, 404 from upstream, etc.), the error surfaced to the user must state that the package was not found at that ref or could not be fetched from the registry.
- It must NEVER tell the user "The backend server may be restarting or unreachable" when the failure is an upstream package lookup failure.
- In `web-proto/js/api.js`, if a 502 response carries a message indicating an upstream/registry fetch failure (or if the server provides structured error/guidance), format the error clearly as a package fetch error.
- All existing tests in `tests/slice86-friendly-error-diagnostics.spec.js` must continue to pass.
- `make lint && make lint-strict && make test-race` must pass cleanly.

## Verification

```sh
make lint && make lint-strict && make test-race
npx playwright test tests/cf116-provider-ref-error.spec.js tests/slice86-friendly-error-diagnostics.spec.js
```

## Handover

Branch `CF-116-provider-ref-502-guidance`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
