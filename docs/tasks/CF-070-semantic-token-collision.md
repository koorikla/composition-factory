# CF-070 — `--shared` and `--warn` are the same hex, so a shared binding and a warning are the same colour

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-070 — --shared and --warn are the same hex, so a shared binding and a warning are the same colour.` |
| **Worktree** | `.worktrees/CF-070` on branch `CF-070-semantic-token-collision`, branched from `main` |
| **May write** | `web-proto/css/proto.css`, `docs/design/canvas-prototype.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

In `proto.css` and `canvas-prototype.html`:
```css
/* light */
--shared: #877200;
--warn:   #877200;

/* dark */
--shared: #D6AB33;
--warn:   #D6AB33;
```
Contrast between `--shared` and `--warn`: 1.00:1.
In the generated YAML viewer, `.code .tm` (template) and `.code .sh` (shared binding) render in the exact same colour.
On the canvas, a shared wire is painted with `--shared`, while warning states / indicators also reuse the exact same colour.
The semantic tokens must be pairwise distinct in both themes, while maintaining zero token drift between `proto.css` and `canvas-prototype.html`.

## Requirements

1. **Differentiate `--warn` and `--shared`**:
   - Keep `--shared` as gold/amber (`#877200` light / `#D6AB33` dark) or update `--warn` to a distinct amber/orange warning hue (e.g. `--warn: #B45309` or `#C2410C` in light, `--warn: #F59E0B` or `#FB923C` in dark, ensuring >= 4.5:1 contrast against surface backgrounds).
   - Ensure `--shared` and `--warn` have distinct hues/hex codes across both themes.
   - Synchronize changes identically with `docs/design/canvas-prototype.html` to maintain zero token drift.
2. **Automated Tests**:
   - In `tests/slice67-theme-native-controls.spec.js` (or dedicated test), verify that `--shared` and `--warn` are distinct in both light and dark themes.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice67-theme-native-controls.spec.js
```
