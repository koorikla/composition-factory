# CF-050 — The core loop is pointer-only: a keyboard or touch user cannot place a kind or select a card

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P0 |
| **Closes** | `CF-050 — The core loop is pointer-only: a keyboard or touch user cannot place a kind or select a card.` |
| **Worktree** | `.worktrees/CF-050` on branch `CF-050-keyboard-touch-placement`, branched from `main` |
| **May write** | `web-proto/js/regions/palette.js`, `web-proto/js/regions/canvas.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom

Palette rows (`.kind` in `web-proto/js/regions/palette.js:243`) are `draggable` `<div>`s — 46 rows, 0 tabbable, 0 with a role, 0 focusable children — and `onDrop` (`canvas.js:1724`) is the only code that appends a resource.
`proto.css:677-678` hides a card's action buttons (`[data-act]`) until the card has `.sel`, while an unselected card has no tabindex/role/focusability. Thus a keyboard user cannot navigate the palette, cannot place a kind on the canvas, and cannot select a card to open its inspector or access its duplicate/delete buttons.
On touch devices, there are no touch handlers for placing a kind, and clicking/tapping a kind does nothing.

## Requirements

1. **Palette Kind Accessibility & Placement**:
   - Palette rows (`.kind`) must be focusable (`tabindex="0"`, `role="button"`, `aria-label="Add <kind>"`).
   - Keyboard `Enter` or `Space` on a `.kind` row places the kind on the canvas (using auto-placement or default canvas positioning, registering in blueprint and selecting the new card).
   - Pointer `click` / touch tap on a `.kind` row also places the kind on the canvas (or dispatches the addition), making drag-and-drop an optional shortcut rather than the sole path. Also fulfills CF-065!
2. **Canvas Card Selection & Keyboard Navigation**:
   - Canvas cards (`.node`) must be focusable (`tabindex="0"`, `role="region"`, `aria-label="<kind> resource <name>"`).
   - Focusing a card via Tab/keyboard and pressing `Enter` or `Space` selects the card (`S.select(name)`), opening its inspector.
   - Touching/tapping any part of a card selects it immediately.
3. **Card Actions**:
   - Ensure card action buttons (duplicate, delete) or context actions are accessible via keyboard when the card is focused or selected.
4. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/` verifying:
     - Tabbing into palette kind rows, pressing Enter adds the resource to the canvas and selects it.
     - Clicking a palette kind row adds the resource to the canvas.
     - Tabbing to a canvas card and pressing Enter/Space selects the card and opens inspector.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice85-keyboard-touch-placement.spec.js
```
