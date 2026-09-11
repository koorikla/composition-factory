# CF-178 — canvas.js couples pure dependency-tree layout with 600 lines of drag-to-wire DOM logic

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: architectural refactoring) |
| **Closes** | `CF-178 — canvas.js couples pure dependency-tree layout with 600 lines of drag-to-wire DOM logic` |
| **Worktree** | `.worktrees/CF-178` on branch `CF-178-canvas-modularization` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/canvas/layout.js`, `web-proto/js/regions/canvas/drag-to-wire.js`, `tests/` |
| **Merges after** | CF-179 |

## Symptom

`web-proto/js/regions/canvas.js` (2,224 lines) embeds a pure topological dependency-tree layout algorithm alongside pan/zoom viewport handling, context menus, and a 600-line drag-to-wire state machine. Testing layout calculations currently requires running full browser Playwright tests rather than fast node/unit tests.

## Evidence

In `web-proto/js/regions/canvas.js`:
- Lines 354-526: Pure topological auto-layout algorithm.
- Lines 1104-1688: Drag-to-wire state machine, port geometry matching, wire creation dialogs.

## Contract

1. **Extract pure layout**: Move the topological DAG layout calculation into `web-proto/js/regions/canvas/layout.js`. The layout function is a pure calculation taking node dimensions, resources, and wire connections, returning coordinate mappings without touching the DOM.
2. **Extract drag-to-wire state machine**: Move drag-to-wire interaction handling, port snapping, and wire menu dispatch into `web-proto/js/regions/canvas/drag-to-wire.js`.
3. **Preserve native ES modules**: Use standard ES module imports (`import { ... } from './canvas/layout.js'`), zero bundler dependencies.
4. **Pass all E2E tests**: All existing canvas interactions, wire drawing, card dragging, and zoom/pan Playwright tests must pass without regression.

## Acceptance Test

Create a pure unit test `tests/unit/canvas-layout.test.js` or node test validating that the extracted `computeLayout` arranges interdependent resource nodes left-to-right according to dependency order without DOM dependencies, and verify `make test-e2e` passes.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-178-canvas-modularization`, committed, not pushed, not merged. Final report includes passing runs of the new layout unit test and full Playwright e2e suite.
