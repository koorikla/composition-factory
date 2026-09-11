# CF-264 — Deleting a wired parameter from the palette rail fails backend validation instead of unwiring

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: parameter lifecycle & cascading unwire consistency) |
| **Closes** | `CF-264 — Deleting a wired parameter from the palette rail fails backend validation instead of unwiring` |
| **Worktree** | `.worktrees/CF-264` on branch `CF-264-palette-delete-wired-param` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/cf264-palette-delete-wired-param.spec.js` |
| **Merges after** | nothing |

## Symptom

When deleting a parameter using the delete button (`[data-param-del]`) in the left palette rail (Shared tab), the UI immediately prompts `Delete parameter $n?` and invokes `store.deleteParameter(n)`. If the parameter is wired into any composed resource fields, the backend rejects the deletion with HTTP 400 (`delete parameter "<n>": still referenced by resources "<res>"`), showing a red warnbar error and blocking the deletion.

In contrast, the Inspector (`web-proto/js/regions/inspector/events.js:299-318`) checks `fanOut(doc, pn) > 0`, warns the user (`Parameter "<pn>" is wired into N field(s). Delete it and unwire all referencing fields?`), and calls `cleanParamRefs(draft, pn)` via `store.replaceDoc(draft)` to cleanly cascade the deletion.

## Mechanism

In `web-proto/js/regions/palette.js:1222-1229`:
```javascript
    const pdel = e.target.closest("[data-param-del]");
    if (pdel) {
      const n = pdel.getAttribute("data-param-del");
      if (!window.confirm("Delete parameter $" + n + "?")) return;
      paramErr = null;
      store.deleteParameter(n);   // failure surfaces via the error topic below
      return;
    }
```
`palette.js` already calculates `fanOut(doc, n)` when rendering parameter cards in the rail. But the click handler ignores whether the parameter is wired, never asks to unwire referencing fields, and sends a bare `DELETE /api/parameters/:name` which the backend rejects if references exist.

## Contract

1. In `web-proto/js/regions/palette.js`, when deleting a parameter via `[data-param-del]`, check `fanOut(doc, n)`:
   - If `fo > 0`, confirm with message: `Parameter "${n}" is wired into ${fo} field(s). Delete it and unwire all referencing fields?` (matching the inspector message).
   - If confirmed when `fo > 0`, create a draft copy of the blueprint, delete the parameter from `draft.spec.xrd.parameters`, call `cleanParamRefs(draft, n)`, and save via `store.replaceDoc(draft)` (or import/call the shared `cleanParamRefs` logic).
   - If `fo === 0`, confirm with `Delete parameter $${n}?` and call `store.deleteParameter(n)`.
2. Ensure deleting a wired parameter from the palette rail cleanly removes the parameter and unwires referencing fields without any 400 error or warnbar message.
3. Playwright test in `tests/cf264-palette-delete-wired-param.spec.js` verifies that deleting a wired parameter from the palette rail unwires referencing fields and removes the parameter cleanly.

## Acceptance Test

Playwright test in `tests/cf264-palette-delete-wired-param.spec.js`:
1. Launch canvas with a blueprint containing a wired parameter (e.g. `params.region` wired to `q.fields.region`).
2. Switch to Shared rail tab.
3. Click `[data-param-del="region"]`.
4. Accept dialog confirming unwiring.
5. Verify no error warnbar is shown.
6. Verify parameter is removed from palette and canvas wire is removed.

## Verification

```sh
make lint && make lint-strict && make test
CF_E2E_PORT=25713 npx playwright test tests/cf264-palette-delete-wired-param.spec.js
```

## Handover

Branch `CF-264-palette-delete-wired-param`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
