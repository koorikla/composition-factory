# CF-085 — The canvas replaces every card element when web fonts finish loading

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-085 — The canvas replaces every card element when web fonts finish loading, so a click or drag in flight lands on a detached node.` |
| **Worktree** | `.worktrees/CF-085` on branch `CF-085-canvas-rebuild-on-font-load`, **branched from `origin/wip-2026-09-09-uncommitted` (419bb60), not from `main`** |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/index.html`, `web-proto/css/proto.css` |
| **Merges after** | nothing |

**Start from the right base.** The regression does not exist on `main`. It lives on
`wip-2026-09-09-uncommitted`, which carries the canvas UX work and the vendored fonts
that were held back from `bba9d78` precisely because of this bug. Branch from there.
When this is fixed, that whole branch becomes landable — which is the point of the task.

## Symptom

`render()` rebuilds the entire canvas rather than patching it, and the decision to
re-lay is driven by measured card geometry. Web fonts load *after* first paint, so when
the faces land every card's width and height change at once, the next render runs, and
every card DOM node is replaced. Anything holding a reference to a card — a click that
has begun, a drag in progress — is now pointing at a detached element.

For a user on a slow connection this is the click-reliability class this repository has
already fought twice: the card you pressed is gone by the time the press completes.

It was latent until the fonts were vendored. `web-proto/index.html` used to pull IBM
Plex from `fonts.googleapis.com`; in a test browser that request never resolved, so
metrics never changed after first paint and the layout signature stayed stable. Local
`@font-face` files load quickly and asynchronously, which is what exposed it.

## Evidence

On `wip-2026-09-09-uncommitted` (419bb60), from a clean scratch dir:

```sh
$ rm -rf .testrun* test-results && make test-e2e
  2 failed
    tests/slice33-stable-canvas-dom.spec.js:12:1 › selecting one card leaves every other card element connected
    tests/slice40-interaction-stability.spec.js:23:1 › a drag survives a doc change that lands mid-gesture
  1 skipped
  179 passed (1.3m)
```

Reproduced twice, both full-suite runs. The failing assertion is the invariant itself:

```
expect(await dl.evaluate(el => el.isConnected)).toBe(true)  // not rebuilt
  Expected: true
  Received: false
```

Both pass **in isolation**, 3 runs out of 3:

```sh
$ npx playwright test tests/slice33-stable-canvas-dom.spec.js
  2 passed (1.3s)      # and again, and again
```

The same full suite on `origin/main` is green — `179 passed`, 0 failed — so this is
introduced by the branch, not pre-existing.

Narrowed pairing (Playwright runs files alphabetically, so slice32 precedes slice33):

```sh
$ npx playwright test tests/slice32-status-on-cards.spec.js tests/slice33-stable-canvas-dom.spec.js
  1 failed   3 passed          # on the wip branch
  4 passed                     # on origin/main
```

Reverting `web-proto/index.html` alone makes that **pair** pass, but does **not** make
the full suite pass. A second contributing factor is unaccounted for; finding it is part
of this task.

## Location

- `web-proto/js/regions/canvas.js:447` — `canvasEl.innerHTML = h;`. Every render
  replaces the whole subtree, so any render detaches every card.
- `web-proto/js/regions/canvas.js:364` — `let lastLayoutSig = "";  // measured-size
  signature; re-lay only on change`.
- `web-proto/js/regions/canvas.js:455` — the signature is built from
  `el.offsetWidth + "x" + el.offsetHeight`, which is exactly what a font swap changes.
- `web-proto/js/regions/canvas.js:458` — `anyUnplaced` gates the re-layout on that
  signature differing.
- `web-proto/index.html` — the three removed `fonts.googleapis.com` / `fonts.gstatic.com`
  lines; `web-proto/css/proto.css` now declares local `@font-face` faces.
- Nothing under `web-proto/js/` references `document.fonts`. There is no font gating
  anywhere.

## Acceptance test

**The tests already exist. Do not write new ones, and do not weaken these.**
`tests/slice33-stable-canvas-dom.spec.js` and `tests/slice40-interaction-stability.spec.js`
are the definition of done. They must pass **in a full `make test-e2e` run**, not only
when run alone — running them alone is what hides this bug.

**Fails today with:** the output quoted under Evidence, reproduced twice.

## Dead end already burned — do not repeat it

Forcing the re-measure to happen early does **not** work:

```js
// tried in init(), does not fix it
document.fonts.ready.then(function () { lastLayoutSig = ""; render(); });
```

`render()` *is* the rebuild, so calling it on `fonts.ready` only moves the detachment to
a different moment — often straight into the window the test clicks in. Verified: the
`slice32 → slice33` pair still failed with this in place. The fix has to stop the
rebuild, not reschedule it.

## Contract

When this is done:

- Selecting a card must not replace any other card's DOM element. A reference taken
  before the selection must still satisfy `isConnected` after it.
- A geometry re-measure — from fonts loading, a resize, or any other cause — must update
  card position and size **in place**. It must not replace card elements.
- The full `make test-e2e` suite passes from a clean scratch dir, with no test weakened,
  skipped or deleted. A leftover `.testrun*` directory masks this failure, so always
  `rm -rf .testrun* test-results` first.
- Card layout must still be correct once fonts have loaded: the auto-placement pass
  exists because cards are content-sized, and it must still produce a non-overlapping
  dependency layout with the final metrics, not the fallback 220px ones.
- If you find the second factor is not font-related at all, say so in the handover with
  the evidence. The contract is the invariant, not the diagnosis in this brief.

## Verification

```sh
make lint && make test-race
rm -rf .testrun* test-results && make test-e2e     # full suite, clean scratch, twice
```

Run the full suite **twice**. A single green run does not distinguish a fix from the
ordering luck that makes these pass in isolation.

## Out of scope

- The provider-add locking optimisation reverted in `1ab0e64` — that is CF-082's area.
- The eslint gate and the JS lint fixes riding on the same branch. They are held back
  for packaging reasons, not because they are broken; do not change their behaviour.
- The wider canvas UX findings CF-049 through CF-072. This task fixes one invariant.
