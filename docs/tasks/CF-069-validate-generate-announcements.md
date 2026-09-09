# CF-069 — Nothing announces a Validate or Generate result, and the error text is hover-only

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-069 — Nothing announces a Validate or Generate result, and the error text is hover-only.` |
| **Worktree** | `.worktrees/CF-069` on branch `CF-069-validate-generate-announcements`, branched from `main` |
| **May write** | `web-proto/index.html`, `web-proto/js/regions/output.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom

When Validate or Generate completes, nothing announces the result to screen readers (`aria-live` is absent across the entire repository).
The `#valid` status chip is a `<span>` mutated with `textContent`, whose full error message was previously stored only in a `title` attribute.
The chip has a click handler and `cursor: pointer` without being keyboard focusable (`tabindex="0"`, `role="button"` or `role="status"`).

## Requirements

1. **Accessibility Announcements**:
   - Add an `aria-live="polite"` container (or attribute `#valid` with `role="status" aria-live="polite"`).
   - When Validate or Generate updates status, ensure screen readers are notified of the outcome ("Validation succeeded", "Render error: ...", "Generated N manifests").
2. **Keyboard Focus & Interaction**:
   - Ensure `#valid` is keyboard focusable (`tabindex="0"`, `role="button"` or `role="status"`) and pressing Enter/Space triggers its click action (expanding the drawer to reveal diagnostics).
3. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/` verifying `aria-live` presence, keyboard focusability, and activation.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice89-validate-generate-announcements.spec.js
```
