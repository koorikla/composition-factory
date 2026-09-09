# CF-063 — At 1280×720 the palette is 179 px and truncates every long kind name to the same prefix

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-063 — At 1280×720 the palette is 179 px and truncates every long kind name to the same prefix.` |
| **Worktree** | `.worktrees/CF-063` on branch `CF-063-palette-kind-truncation`, branched from `main` |
| **May write** | `web-proto/css/proto.css`, `web-proto/js/regions/palette.js`, `docs/design/canvas-prototype.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

At 1280×720, `#region-palette` is 180 px wide and truncates every long kind name to the same prefix (e.g. `bucket` yields `BucketAnaly…`, `BucketCorsC…`, and two rows reading `BucketObject…`).
The left column `#lrail` / `#region-palette` is not resizable or wide enough to distinguish kinds, and dragging the edge previously selected text.
Upjet CRD kinds typically have long names. Users must be able to tell kind rows apart.

## Requirements

1. **Palette Width & Responsiveness**:
   - Ensure the left palette column allows comfortable reading of kind names, or supports column resizing (drag handle between palette and canvas), or smart tooltip / wrap / suffix display so two similar kinds can be clearly distinguished.
   - Ensure tooltip (`title`) on each kind row displays full kind name, apiVersion, and provider.
   - Maintain zero token drift between `web-proto/css/proto.css` and `docs/design/canvas-prototype.html`.
2. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/` verifying kind distinction at 1280×720 viewport.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice87-palette-kind-truncation.spec.js
```
