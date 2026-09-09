# CF-067 — The light-theme code viewer fails AA on four of five syntax colours

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-067 — The light-theme code viewer fails AA on four of five syntax colours.` |
| **Worktree** | `.worktrees/CF-067` on branch `CF-067-light-theme-code-syntax-contrast`, branched from `main` |
| **May write** | `web-proto/css/proto.css`, `docs/design/canvas-prototype.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

In light theme on `--sunk` `#D8E0EA`:
- template syntax (`.code .tm`): 3.55:1 (fails AA)
- shared binding (`.code .sh`): 3.55:1 (fails AA)
- comments (`.code .co`): 3.68:1 (fails AA)
- keys (`.code .k`): 4.46:1 (fails AA)
All syntax colors must achieve >= 4.5:1 contrast against their background `--sunk` `#D8E0EA` (or code background).

## Requirements

1. **Adjust Light Theme Code Syntax Accents**:
   - Derive dark enough light-mode syntax colors against `--sunk` (`#D8E0EA`) to achieve >= 4.5:1 contrast.
   - Synchronize changes with `docs/design/canvas-prototype.html` to maintain zero token drift.
2. **Automated Tests**:
   - Add automated contrast assertions in `tests/slice67-theme-native-controls.spec.js` (or dedicated test) verifying all syntax colors achieve >= 4.5:1 WCAG AA contrast.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice67-theme-native-controls.spec.js
```
