# CF-158 — Card positions are not persisted; a reload auto-lays-out and can put a card half under the inspector

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: polish / layout stability) |
| **Closes** | `CF-158 — Card positions are not persisted; a reload auto-lays-out and can put a card half under the inspector` |
| **Worktree** | `.worktrees/CF-158` on branch `CF-158-card-positions-persistence` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/store.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

After manually arranging resource cards on the canvas, reloading the page reruns auto-layout and resets all positions. Furthermore, initial auto-layout or card placement can position cards partially underneath the right-hand inspector panel.

## Evidence

In usability run M1 (`docs/ux-runs/2026-09-11-m1-s3-first-contact.md` P3-2 and screenshot 28):
- User-arranged card coordinates are lost upon browser refresh.
- Auto-layout places nodes across the entire window width without reserving space for the open inspector drawer.

## Contract

1. **Position Persistence**: When a user drags a resource card, its coordinates `(x, y)` are persisted (e.g. in `localStorage` keyed by document identifier, or blueprint layout metadata). On page reload, cards restore to their persisted coordinates instead of resetting to default layout.
2. **Inspector Boundary Constraint**: Auto-layout and initial node drops must compute available canvas width taking the right-hand inspector panel into account, ensuring no card is positioned underneath the inspector.
3. **Reset Affordance**: Clicking the Layout / Tidy button (`#layout-btn`) clears manual offsets and re-runs pure topological layout.

## Acceptance Test

Write Playwright test `tests/cf158-card-positions-persistence.spec.js`:
1. Open canvas, drag a resource card to coordinates `(450, 320)`.
2. Reload page via `page.reload()`.
3. Assert card position remains at `(450, 320)`.
4. Assert no card bounding box overlaps the bounding client rect of the inspector drawer.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-158-card-positions-persistence`, committed, not pushed, not merged. Final report includes failing and passing runs of the acceptance test.
