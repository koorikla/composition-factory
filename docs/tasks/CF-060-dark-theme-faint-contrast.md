# CF-060 — The dark theme's `--faint` was never re-derived; 30 AA failures, including the Generate button at 2.69:1

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-060 — The dark theme's --faint was never re-derived; 30 AA failures, including the Generate button at 2.69:1.` |
| **Worktree** | `.worktrees/CF-060` on branch `CF-060-dark-theme-faint-contrast`, branched from `main` |
| **May write** | `web-proto/css/proto.css`, `docs/design/canvas-prototype.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

Every ink token inverts between themes except `--faint` (light: `#67727F`, dark: `#6A747F` — 45.1% to 45.7% lightness).
In dark mode, `--faint` lands at **3.64:1 on `--surface`** (`#161B22`), 3.36:1 on `--surface-2` (`#1C222B`), failing WCAG AA (minimum 4.5:1 required).
Furthermore, `#fff` text is hardcoded against themed accents that were lightened for dark mode without adjusting contrast:
- `.btn.pri` (the **Generate** button) uses `#fff` on `--wire-xrd` (`#5CA0F5`), yielding only **2.69:1** contrast. In dark mode, `.btn.pri` should either use a darker background that provides >=4.5:1 against `#fff`, or dark text (e.g. `var(--ground)` or `#0B0F14`) when on lightened accent, or adjust `--wire-xrd` for primary buttons.
- `.fan` (fan-out badge) uses `#fff` on `--shared` (`#D6AB33`), yielding only **2.16:1** contrast at 9px.
- Light theme also has contrast issues when `--faint` or accents are used against light surfaces.
All tokens are inherited between `web-proto/css/proto.css` and `docs/design/canvas-prototype.html` with zero token drift, so both files must be kept in sync.

## Requirements

1. **Re-derive dark theme `--faint`**:
   - In `web-proto/css/proto.css` and `docs/design/canvas-prototype.html`:
   - Adjust `--faint` in dark theme (`@media (prefers-color-scheme:dark)` and `:root[data-theme="dark"]`) so it achieves at least 4.5:1 contrast against dark `--surface` (`#161B22`), `--surface-2` (`#1C222B`), and `--ground` (`#0C1015`) (e.g., `#8E99A8` or higher lightness ~60-65%).
2. **Fix `.btn.pri` and `.fan` contrast in dark mode**:
   - For `.btn.pri`: in dark mode, ensure text contrast is >= 4.5:1. If `--wire-xrd` is `#5CA0F5`, dark ink (`#0C1015` or `var(--ground)`) achieves ~8:1 against `#5CA0F5`; alternatively, keep `.btn.pri` with dark text or adjust appropriately.
   - For `.fan`: ensure text on `.fan` has >= 4.5:1 contrast in both light and dark modes.
3. **Keep Zero Token Drift**:
   - Both `web-proto/css/proto.css` and `docs/design/canvas-prototype.html` must remain identical in token definitions.
4. **Automated Verification**:
   - Add a test or assertion verifying that `--faint` and primary button contrast meet WCAG AA (4.5:1).

## Verification

```sh
npm run lint:js
make lint
make test
make test-e2e
```
