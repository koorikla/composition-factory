# CF-393 — Canvas keydown listener ignores Ctrl+Y / Meta+Y, breaking standard Redo keyboard shortcut

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-393 — Canvas keydown listener ignores Ctrl+Y / Meta+Y, breaking standard Redo keyboard shortcut` (#284) |
| **Worktree** | `.worktrees/CF-393` on branch `CF-393-canvas-redo-shortcut`, branched from `main` |
| **May write** | `web-proto/js/main.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When a user deletes a resource, unlinks a wire, or modifies a parameter, and then presses `Ctrl+Z` (or `Cmd+Z`) to undo, pressing `Ctrl+Y` or `Meta+Y` (standard redo on Windows/Linux and secondary redo on macOS) does nothing. The redo button (`#redoBtn`) remains enabled and `store.canRedo()` remains true, but the keyboard shortcut is completely ignored. Users are forced to move the mouse to click the small topbar Redo button or know the non-standard `Shift+Ctrl+Z` combination.

## Mechanism

In `web-proto/js/main.js:270`, the global keydown listener exclusively checks:
```javascript
if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "z") return;
e.preventDefault();
if (e.shiftKey) store.redo(); else store.undo();
```
Because the condition requires `e.key.toLowerCase() === "z"`, any keydown event with `e.key === "y"` or `e.key === "Y"` is immediately returned and ignored.

## Acceptance Test

Verbatim in `tests/slice12-undo-redo.spec.js`:
Add tests asserting that `ControlOrMeta+y` triggers redo when canvas has focus, without stealing native behavior inside text input fields.

## Contract

1. In `web-proto/js/main.js`, update the keydown listener to trigger `store.redo()` on `(e.metaKey || e.ctrlKey)` when `e.key.toLowerCase() === "y"`.
2. Ensure text fields (`input`, `textarea`, `isContentEditable`) continue to retain their native browser key behavior.
3. Pass all gates: `make lint && make lint-strict && make test-race && make test-e2e`.
