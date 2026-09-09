# CF-101 — The Playwright suite runs its engine against the developer's real schema cache

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (engine scale: host state decides what the gate proves; the suite writes into the host cache) |
| **Closes** | `CF-101 — *(engine)* The Playwright suite runs against the developer's real schema cache: playwright.config.js:27 starts the engine without --cache-dir, so specs skip or pass depending on what the host has cached and make test-e2e writes provider-nop into ~/Library/Caches/compositionfactory.` |
| **Worktree** | `.worktrees/CF-101` on branch `CF-101-e2e-scratch-cache` |
| **May write** | `playwright.config.js`, `tests/helpers.js`, `tests/slice16-provider-remove.spec.js`, `tests/slice17-catalogue.spec.js`, `tests/cf101-e2e-engine-uses-scratch-cache.spec.js` (new), `.github/workflows/ci.yml` (e2e job only), `AGENTS.md` (§2, one line) |
| **Merges after** | nothing |

## Symptom

`make test-e2e` is documented as structurally isolated ("its own engine, its own
document, its own port"), but the engine it starts uses `cache.DefaultRoot()`, which is
the developer's `~/Library/Caches/compositionfactory`. Two consequences:

- A spec's outcome depends on what the host happens to have cached.
  `tests/slice17-catalogue.spec.js:26` skips when provider-nop "is already cached from
  a prior run"; `tests/slice16-provider-remove.spec.js:29` skips when provider-aws-s3 is
  *not* cached. Locally the suite runs warm and skips one; CI runs cold and skips the
  other; neither run proves both behaviours. This is one mechanism behind "e2e is the
  flaky job".
- Every local run writes provider entries into the developer's cache, so a test run
  changes what the human's own `cf serve` sees next.

## Evidence

`playwright.config.js:27` (webServer command), `6fda5b6`:

```
./bin/cf serve --addr 127.0.0.1:${port} --blueprint ${scratchDir}/doc.cf.yaml --out ${scratchDir}/out --lock ${scratchDir}/.cf.lock
```

No `--cache-dir` anywhere in `playwright.config.js`, `tests/helpers.js` or
`.github/workflows/ci.yml` (`grep -n cache` over the three: no hits). The two
host-state skips:

```
tests/slice16-provider-remove.spec.js:29:  test.skip(!(await s3row.count()), 's3 provider not cached')
tests/slice17-catalogue.spec.js:26:  test.skip(have.providers.some(p => p.ref.includes(REF_HINT)), 'nop already cached from a prior run')
```

The acceptance spec below fails twice on `6fda5b6` in a fresh worktree.

## Location

- `playwright.config.js:13-16` — `getWorkspace()` derives `port` and `scratchDir` from
  the worktree path; the engine command at `:27` passes blueprint, out and lock from
  `scratchDir` but not a cache dir.
- `cmd/cf/serve.go` — `--cache-dir` exists (`$CF_CACHE_DIR`), default `cache.DefaultRoot()`.
- `tests/helpers.js:15-28` — the same hash; nothing about the cache.
- `.github/workflows/ci.yml` e2e job — installs crossplane, pre-pulls function images,
  `make test-e2e`; the runner's cache is empty, so specs that load starters already
  fetch from ghcr over the network.

## Acceptance test

Write this test **first**, verbatim, and watch it fail before you change any
production code. It is the definition of done; do not paraphrase it, do not weaken
an assertion to make it pass, and do not delete it if it turns out to be
inconvenient - if it is wrong, say so in the handover and stop.

```js
// tests/cf101-e2e-engine-uses-scratch-cache.spec.js
// CF-101 — The e2e engine must run against a scratch schema cache, not the
// developer's ~/Library/Caches/compositionfactory: host state must not decide
// whether a spec passes, and the suite must not write into the host cache.
const { test, expect } = require('@playwright/test')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { execSync } = require('child_process')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

function scratchDir() {
  let toplevel = process.cwd()
  try { toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim() } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  return path.join(toplevel, `.testrun-${hash}`)
}

test.beforeEach(async ({ request }) => { await resetDoc(request) })

test('a provider added during the suite lands in the scratch cache, not the host cache', async ({ request }) => {
  const ref = 'ghcr.io/crossplane-contrib/provider-nop:v0.5.0'
  const r = await request.post(ENGINE + '/api/providers', { data: { ref } })
  expect(r.ok(), await r.text()).toBeTruthy()

  const cache = path.join(scratchDir(), 'cache')
  expect(fs.existsSync(cache), `expected the engine's cache under ${cache}`).toBe(true)
  const entries = fs.readdirSync(cache).filter(d => d.startsWith('provider-nop-'))
  expect(entries.length, `provider-nop entry in ${cache}`).toBe(1)
})
```

**Fails today with** (two runs, worktree at `6fda5b6`):

```
Error: expected the engine's cache under /Users/…/.worktrees/brief-101/.testrun-ef5d5b07/cache
expect(received).toBe(expected) // Object.is equality
Expected: true
Received: false
  1 failed
```

## Contract

- The e2e engine's schema cache lives under the suite's scratch dir
  (`.testrun-<hash>/cache`), created fresh by the webServer command like the rest of
  the scratch dir, and `make clean` removes it with the rest.
- No spec skips on host state. `slice16` gets the s3 provider by adding it (API or UI)
  inside the spec; `slice17` asserts the fresh-cache path unconditionally. Both run in
  CI and locally and exercise the same branches.
- A suite run leaves `~/Library/Caches/compositionfactory` untouched: state that in a
  comment in `playwright.config.js` and in `AGENTS.md` §2's port/isolation table.
- Cold-start cost is acceptable: provider adds fetch from ghcr.io (CI already does this
  for the starter loads). If wall-clock becomes a problem, seed the scratch cache from a
  checked-in fixture rather than from the host cache — never from the host cache.
- The full suite stays green in two consecutive clean runs (`rm -rf .testrun* test-results`).

## Verification

```sh
make lint && make test-race
rm -rf .testrun* test-results && make test-e2e     # twice
ls ~/Library/Caches/compositionfactory              # unchanged mtime list before/after
```

## Out of scope

- The demo recorder's engine (`scripts/record-demos/`), which has the same gap; note it
  in the handover.
- Any flake whose cause is canvas drag timing (CF-085 lineage), not cache state.

## Handover

Branch `CF-101-e2e-scratch-cache`, committed, not pushed, not merged. In your final
report: the failing run and the passing run of the acceptance test, both pasted; every
gate you ran; every judgement call you made where the brief was silent.
