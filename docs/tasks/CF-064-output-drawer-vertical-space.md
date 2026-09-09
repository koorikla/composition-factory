# CF-064 — The output drawer takes 250 px of a 720 px viewport to show 142 px of YAML

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-064 — The output drawer takes 250 px of a 720 px viewport to show 142 px of YAML.` |
| **Worktree** | `.worktrees/CF-064` on branch `CF-064-output-drawer-vertical-space`, branched from `main` |
| **May write** | `web-proto/css/proto.css`, `web-proto/js/regions/output.js`, `docs/design/canvas-prototype.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

At 1280×720, the output drawer consumes 250 px (35%) of the 720 px viewport while `#code` inside it only receives 142 px (seven lines of code), with 43% of the drawer consumed by its own chrome/header.
Authoring surfaces (canvas and palette) are squeezed to 424 px height.

## Requirements

1. **Output Drawer Height & Layout**:
   - Reduce the default collapsed/expanded proportion of the output drawer on 720px viewports (e.g. default drawer height ~180-200px or compact header/tab strip, maximizing visible code area).
   - Ensure the canvas and palette retain at least 460-480px of vertical authoring space.
   - Maintain zero token drift between `proto.css` and `canvas-prototype.html`.
2. **Automated E2E Test**:
   - Add Playwright tests in `tests/slice91-output-drawer-sizing.spec.js` asserting proper proportioning at 1280×720.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice91-output-drawer-sizing.spec.js
```
