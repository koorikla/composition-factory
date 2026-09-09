# CF-059 — Raw Go errors and HTTP statuses reach the user verbatim

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-059 — Raw Go errors and HTTP statuses reach the user verbatim.` |
| **Worktree** | `.worktrees/CF-059` on branch `CF-059-friendly-error-diagnostics`, branched from `main` |
| **May write** | `web-proto/js/api.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/output.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

Raw Go errors, HTTP status codes, and coordinate-system errors reach the user without helpful explanations:
- `api.js:49` surfaces `network error: Failed to fetch`.
- `api.js:60` falls back to a bare `502 Bad Gateway` or raw status code.
- `inspector.js:327` shows the literal `operation failed`.
- Server messages arrive addressed in the blueprint coordinate system (`spec.resources[3] …`) rather than human canvas names.
The pattern to follow is already in `diagnoseError` (`output.js:368-399`): keep the server text and append actionable guidance / translation.

## Requirements

1. **User-Friendly Network and API Errors (`api.js`)**:
   - Translate network errors (`Failed to fetch`) into clear, helpful guidance (e.g. "Cannot connect to cf server. Ensure 'cf serve' is running at http://127.0.0.1:8080").
   - For 502/503/504 errors, append helpful advice (e.g. server restarting or proxy issue).
2. **Inspector Operation Failure Messages (`inspector.js`)**:
   - Replace literal "operation failed" with a meaningful explanation of what failed and why (retrieving the store's error details or action context, e.g. "Failed to update field").
   - If an error references internal coordinates like `spec.resources[i]`, map it to the card's readable resource name where possible.
3. **Automated E2E Test**:
   - Add automated Playwright tests in `tests/` asserting that user-facing errors provide actionable guidance rather than bare HTTP/Go errors.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice86-friendly-error-diagnostics.spec.js
```
