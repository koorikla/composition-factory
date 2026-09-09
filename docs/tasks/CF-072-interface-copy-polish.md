# CF-072 — Interface copy: implementation words, a wrong count, and dead-end empty states

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 |
| **Closes** | `CF-072 — Interface copy: implementation words, a wrong count, and dead-end empty states.` |
| **Worktree** | `.worktrees/CF-072` on branch `CF-072-interface-copy-polish`, branched from `main` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/palette.js`, `web-proto/index.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

- Canvas empty state claims "14 native Kubernetes kinds" (`canvas.js`), while `nativeKinds` actually has 16, and the palette on the same screen reads 16 kinds.
- Palette shows "No kinds match." when nothing was searched if kinds list is empty or loading.
- Dead-end empty states: "No schema found for kind X." offers nothing, while the server already suggests `run: cf provider add %s`.

## Requirements

1. **Fix Native Kinds Count**:
   - Update canvas empty state to dynamically state the correct count or match 16 native Kubernetes kinds.
2. **Empty State & Guidance Copy**:
   - Improve search/filter empty states so they only say "No kinds match search query" when a query is actually entered.
   - When a schema is missing for a kind, provide actionable guidance: `run: cf provider add <provider>`.
3. **Automated E2E Test**:
   - Add automated Playwright tests asserting consistent counts and helpful copy.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice90-interface-copy-polish.spec.js
```
