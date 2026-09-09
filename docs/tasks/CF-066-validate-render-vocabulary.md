# CF-066 — The button says Validate, every result says "render", and the generate chip then erases it

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-066 — The button says Validate, every result says "render", and the generate chip then erases it.` |
| **Worktree** | `.worktrees/CF-066` on branch `CF-066-validate-render-vocabulary`, branched from `main` |
| **May write** | `web-proto/js/regions/output.js`, `web-proto/index.html`, `tests/` |
| **Merges after** | nothing |

## Symptom

The button is named "Validate", but results say "rendering…", "render ok · N resources", "render error".
Furthermore, a manual Validate result is immediately overwritten by background debounced preview generation (which also mutates `#valid`) 300 ms after the next keystroke, erasing the validation result.

## Requirements

1. **Unified Vocabulary**:
   - Harmonize the vocabulary so Validate outcomes match user expectations: "validating…", "valid · N resources" or "validation ok · N resources", "validation error: ...".
2. **Result Persistence**:
   - Ensure an explicit user validation outcome is clearly distinguishable or persists until the document is actually modified, rather than being instantly overwritten.
3. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/slice92-validate-vocabulary.spec.js`.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice92-validate-vocabulary.spec.js
```
