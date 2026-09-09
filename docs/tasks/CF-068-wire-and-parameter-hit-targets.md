# CF-068 — Wire hit targets are a 2.25 px stroke and parameter dots are 7×7 px

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-068 — Wire hit targets are a 2.25 px stroke and parameter dots are 7×7 px.` |
| **Worktree** | `.worktrees/CF-068` on branch `CF-068-wire-and-parameter-hit-targets`, branched from `main` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom

XR parameter dots are 7×7 CSS px stacked closely together, failing target size recommendations.
Wires on the canvas are selected by `pointer-events:stroke` on a thin 2.25 px path, making clicking or selecting a wire frustrating.
Both `onCwClick` (`canvas.js:985`) and `onContextMenu` (`canvas.js:844`) look for a `.wire-hit` element that `drawWires` never emits.

## Requirements

1. **Wire Hit Areas (`canvas.js`)**:
   - In `drawWires()`, render an invisible / transparent wide stroke path with class `wire-hit` (e.g. `stroke="transparent" stroke-width="14"` or `16px` with `pointer-events="stroke"`) along each wire's path before the drawn visual stroke.
   - Attach `data-wire-idx` to `.wire-hit` so clicking or right-clicking anywhere near the wire easily selects or opens context menu for the wire.
2. **Parameter and Port Hit Targets**:
   - Ensure the clickable/draggable area of port dots (`.port .d`) has an adequate hit target (e.g. pseudo-element `::before` or padding providing at least 16–20px interactive hit area without distorting the visual dot).
3. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/` verifying that clicking slightly off the drawn wire stroke (e.g. 5–6px away) successfully selects the wire and that `.wire-hit` exists in the SVG DOM.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice88-wire-hit-targets.spec.js
```
