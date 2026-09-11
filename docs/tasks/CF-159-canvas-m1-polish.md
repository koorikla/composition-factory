# CF-159 — Canvas polish from the M1 run: ⌂ does nothing with a card off-screen, Generate warns of overwriting an empty directory, kind tooltip lingers after a drop, inspector scrolls horizontally on long paths

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: consolidated polish suite) |
| **Closes** | `CF-159 — Canvas polish from the M1 run: ⌂ does nothing with a card off-screen, Generate warns of overwriting an empty directory, kind tooltip lingers after a drop, inspector scrolls horizontally on long paths` |
| **Worktree** | `.worktrees/CF-159` on branch `CF-159-canvas-m1-polish` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/palette.js`, `web-proto/js/main.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom & Four Polish Moments

From M1 usability run (`docs/ux-runs/2026-09-11-m1-s3-first-contact.md` items P3-1, P3-3, P3-4, P3-6):
1. **Reset View (`zoom-reset` / ⌂)**: Simply resetting coordinates to `(0, 0, 1)` fails when cards are placed off-screen or far to the right/bottom; ⌂ should fit and center all cards within the visible canvas viewport.
2. **Generate Confirmation Dialog**: Generate dialog warns about "overwriting existing files" even when the target directory is empty or does not yet exist.
3. **Palette Kind Tooltip**: Hover tooltips from the KINDS catalogue linger over the canvas after dropping a new resource card until the mouse moves.
4. **Inspector Path Truncation**: Long property paths in the inspector cause horizontal scrolling or awkward layout wrapping (e.g. `ule[0].blockedEncryptionTypes`).

## Contract

1. **Fit-to-View on Reset**: Clicking `#zoom-reset` calculates the bounding box enclosing all canvas nodes and computes `(x, y, k)` such that all cards are centered and fully visible within the canvas viewport.
2. **Accurate Overwrite Warning**: Query directory state or suppress overwrite warnings when the target output path contains no pre-existing files.
3. **Dismiss Palette Tooltips on Drop**: Hide/remove catalogue tooltip popovers on `dragstart` and `drop`.
4. **Clean Path Formatting in Inspector**: Ensure long field paths use ellipsis truncation (`text-overflow: ellipsis`) with full native `title` tooltip, avoiding horizontal scrollbars.

## Acceptance Test

Write Playwright test `tests/cf159-canvas-m1-polish.spec.js` asserting all four moments:
- Zoom reset centers cards that were placed out of viewport.
- Generate with empty output directory omits overwrite warning.
- Kind drag-and-drop clears tooltip immediately.
- Inspector handles long field path without horizontal scroll container expansion.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-159-canvas-m1-polish`, committed, not pushed, not merged. Final report includes failing and passing runs of the acceptance test suite.
