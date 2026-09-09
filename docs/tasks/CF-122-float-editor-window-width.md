# CF-122 — Floating the editor window turns textarea into a 24 px-wide column

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (UX scale: hand-editing is impossible in the mode built for it) |
| **Closes** | `CF-122 — "Float editor window" turns the blueprint editor into a 24 px-wide textarea (one character per line); docked, it is 80 px tall for an 80-line document and opens scrolled to the end.` |
| **Worktree** | `.worktrees/CF-122` on branch `CF-122-float-editor-window-width` |
| **May write** | `web-proto/css/proto.css`, `web-proto/js/regions/output.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When editing the blueprint in the Generated drawer (`untitled.cf.yaml` / `blueprint.cf.yaml` tab -> click `edit`), clicking the float button (`⛶` / `#drawer-float-btn`) detaches the drawer into a floating window.
However, inside the floated window, the `#code-editor` textarea width collapses to 24px wide, rendering one character per line (e.g. `v`, `a`, `l`, `.` stacked vertically).
Additionally, when docked, `#code-editor` only gets 80px of height for an 80+ line document because `editor.style.height` is artificially capped to `Math.max(80, el.code.clientHeight - 36) + "px"` and opens scrolled to the bottom.

## Evidence

In Journey J2 finding F4 (`docs/ux-runs/2026-09-09-canvas-xpostgres-build.md`):
Textarea bounding box `{w:24, h:80}` floated vs `{w:684, h:80}` docked.
In floated mode, `.drawer.floated-panel` has `display: flex !important;` but lacks `flex-direction: column;`, so the children of `#region-output` (`#drawer-h` and `#drawer-body`) are arranged horizontally side-by-side! `#drawer-h` takes up horizontal space, squishing `#drawer-body` and collapsing `#code-editor` to 24px.
Notice in `web-proto/css/proto.css:545-546`:
`.pane.r.floated-panel` has `display: flex !important; flex-direction: column;`.
`.drawer.floated-panel` at line 562 only has `display: flex !important;` without `flex-direction: column;`!
Furthermore, `#code-viewport` is a flex container with `flex-direction: column;`, but `#code-editor` and `#code-editbar` are injected into `#code-viewport` where `#code-editor` has a hardcoded inline `style.height` calculated from `el.code.clientHeight - 36`, which prevents it from expanding to fill the container properly.

## Location

- `web-proto/css/proto.css:548-563`:
  ```css
  .drawer.floated-panel {
    position: fixed !important;
    ...
    display: flex !important;
    flex-direction: column; /* MISSING! */
  }
  ```
- `web-proto/js/regions/output.js:815-816` and `:830-832`:
  Inline style on `#code-editor` sets `flex: 1; min-height: 0; width: 100%;` but line 831 overrides height with `editor.style.height = Math.max(80, el.code.clientHeight - 36) + "px"`. In flex layout, when `#code-editor` is active, it should cleanly flex to fill the available height of `#code-viewport` without a cramped 80px limit, and scroll top (`editor.scrollTop = 0`).

## Acceptance test

Write this test **first**, verbatim in `tests/cf122-float-editor-window-width.spec.js`, and watch it fail before changing production code:

```js
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('floating the editor drawer maintains full width and fills viewport', async ({ page }) => {
  await page.goto('/')
  // Select blueprint tab in output drawer
  await page.click('#tabs button[data-t="bp"]')
  // Click edit button
  await page.click('#code-edit')
  const editor = page.locator('#code-editor')
  await expect(editor).toBeVisible()

  // Float the drawer
  await page.click('#drawer-float-btn')

  // Bounding box of editor must be comfortably wide (>= 400px), not 24px
  const box = await editor.boundingBox()
  expect(box).not.toBeNull()
  expect(box.width).toBeGreaterThan(400)
  expect(box.height).toBeGreaterThan(150)
})
```

## Contract

- In `web-proto/css/proto.css`:
  Add `flex-direction: column;` to `.drawer.floated-panel` so header `#drawer-h` and `#drawer-body` stack vertically, matching `.pane.r.floated-panel`.
- In `web-proto/js/regions/output.js`:
  Ensure `#code-editor` fills the available height and width of `#code-viewport`, is not constrained to 24px wide or 80px tall, and initializes with `editor.scrollTop = 0`.
- Docked and floated modes must both provide a usable editor with width >= 400px and adequate height.

## Verification

```sh
make lint && make lint-strict && make test-race
rm -rf .testrun* test-results && make test-e2e
```

## Out of scope

- Syntax highlighting inside the textarea.
- Auto-completion.

## Handover

Branch `CF-122-float-editor-window-width`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran.
