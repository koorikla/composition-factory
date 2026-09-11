# CF-262 — Deleting or renaming an object parameter member when wired fails backend validation and blocks the UI

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: parameter lifecycle & object member cascading unwire) |
| **Closes** | `CF-262 — Deleting or renaming an object parameter member when wired fails backend validation and blocks the UI` |
| **Worktree** | `.worktrees/CF-262` on branch `CF-262-object-member-unwire` |
| **May write** | `web-proto/js/regions/inspector/events.js`, `web-proto/js/regions/inspector/xrd.js`, `tests/cf262-object-member-unwire.spec.js` |
| **Merges after** | `CF-264` |

## Symptom

When an object parameter has typed properties (members) wired into composed resource fields, envelope entries, or annotations (e.g. `from: params.cfg.region`), attempting to delete or rename that member in the canvas parameter inspector triggers an HTTP 400 rejection from `PUT /api/parameters/<paramName>`, displaying an error toast and reverting the change:
- **Delete**: `set parameter "cfg": resource "q" field "region": params.cfg declares no properties — a member reference needs a typed object; declare the member under its properties`
- **Rename**: `set parameter "cfg": resource "q" field "region": references unknown member "region" of params.cfg (declared members: awsRegion)`

The user is blocked from mutating object parameter members without manually finding and deleting each referencing wire first.

## Mechanism

1. In `web-proto/js/regions/inspector/events.js:285-297`:
   The delete button (`[data-mdel]`) removes the member from local properties and immediately calls `commitMembers(...)`. It performs no fan-out check across referencing fields and calls no reference cleanup.
2. In `web-proto/js/regions/inspector/events.js:875-903`:
   The member rename input (`[data-mname]`) updates the property key and immediately calls `commitMembers(...)` without cascading the new member path to referencing fields or annotations.
3. In `web-proto/js/regions/inspector/xrd.js:68-130`:
   `cleanParamRefs(draft, pn)` only removes top-level parameters from `draft.spec.xrd.parameters` and does not handle nested member unwiring or member renames.

## Contract

1. In `web-proto/js/regions/inspector/xrd.js`:
   - Extend reference cleanup or add `cleanMemberRefs(draft, paramName, memberPath)` to remove fields, envelope, and annotations referencing `params.<paramName>.<memberPath>`.
   - Add `renameMemberRefs(draft, paramName, oldMemberPath, newMemberPath)` to update referencing `from` paths when a member is renamed.
2. In `web-proto/js/regions/inspector/events.js`:
   - When clicking `[data-mdel]`:
     - Calculate fan-out for `params.<paramName>.<memberPath>`.
     - If `fo > 0`, prompt confirmation: `Member "${paramName}.${memberPath}" is wired into ${fo} field(s). Delete it and unwire all referencing fields?`.
     - Upon confirmation, cleanly unwire referencing fields in a draft and commit the member deletion without 400 error.
   - When editing `[data-mname]`:
     - Cascade the renamed path to all referencing fields, envelope, and annotations (or prompt to cascade).
3. Ensure mutating or deleting wired members updates the canvas cleanly and displays no backend 400 toast.

## Acceptance Test

Playwright test in `tests/cf262-object-member-unwire.spec.js`:
1. Load canvas with a blueprint having object parameter `cfg` with member `region` wired to `q.fields.region`.
2. Inspect parameter `cfg`.
3. Click delete member (`[data-mdel]`).
4. Accept unwire confirmation dialog.
5. Verify member is removed, no error toast is displayed, and wire on canvas is cleanly removed.
6. Test rename: with wired member `region`, rename to `awsRegion`; verify field wire updates to `params.cfg.awsRegion` without error.

## Verification

```sh
make lint && make lint-strict && make test
CF_E2E_PORT=25714 npx playwright test tests/cf262-object-member-unwire.spec.js
```

## Handover

Branch `CF-262-object-member-unwire`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
