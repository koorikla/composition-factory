# CF-285 — fanOut ignores parameters referenced in when or forEach guards, failing deletion with HTTP 409

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: XRD parameter reference tracking & safe deletion unwire flow) |
| **Closes** | `CF-285 — fanOut ignores parameters referenced in when or forEach guards, failing deletion with HTTP 409` |
| **Worktree** | `.worktrees/CF-285` on branch `CF-285-when-foreach-param-fanout` |
| **May write** | `web-proto/js/wires.js`, `tests/cf285-when-foreach-param-fanout.spec.js` |
| **Merges after** | |

## Symptom

When an XRD parameter is referenced only by a resource `when` condition (e.g. `when: "params.enableQueue"`) or `forEach` loop (e.g. `forEach: "params.replicas"`):
1. The fan-out badge in the XRD Inspector renders `×0`, and in the Shared rail in the palette it renders `0 bound`.
2. When the user attempts to delete the parameter:
   - In the XRD Inspector, clicking delete deletes without confirmation and sends `DELETE /api/blueprint/parameters/{name}`.
   - In the palette rail, clicking delete prompts a simple `Delete parameter ${name}?` without mentioning unwiring, then calls `DELETE /api/blueprint/parameters/{name}`.
3. The server rejects the deletion with `HTTP 409 Conflict: {"error":"delete parameter \"...\": still referenced by resources \"...\""}`.
4. An error toast or warnbar appears, and the parameter remains undeleted.

## Mechanism

In `web-proto/js/wires.js:130-163`:
`fanOutMap(doc)` and `fanOut(doc, param)` derive reference counts solely from `listWires(doc)`.

In `web-proto/js/wires.js:52-120`:
`listWires(doc)` inspects only `r.fields`, `r.envelope`, `r.annotations`, and `r.connectionSecret`. It does not inspect `r.when` or `r.forEach` (in contrast to `findEnvWires(doc, key)` in lines 216-221, which checks both).

Because `fanOut(doc, param)` returns 0:
1. Both `web-proto/js/regions/inspector/events.js:305-317` and `web-proto/js/regions/palette.js:1227-1242` check `if (fo > 0)`.
2. Since `fo === 0`, they bypass `cleanParamRefs(draft, pn)` and send `DELETE /api/blueprint/parameters/{pn}`.
3. The server refuses deletion with HTTP 409 because the resource condition still references the parameter.

(Note: `cleanParamRefs` in `web-proto/js/regions/inspector/xrd.js:123-128` already has the code to remove `r.when` and `r.forEach`, but it is bypassed because `fo === 0`.)

## Contract

1. In `web-proto/js/wires.js`:
   - Update `fanOutMap(doc)` (or `fanOut`) to count parameters referenced in `r.when` (via `isWhenReferencingParam` or `parseWhen`) and in `r.forEach` (via `isParamRef`).
2. Verify that:
   - Parameters referenced in `when` or `forEach` display accurate fan-out counts (`×1`, `1 bound`).
   - Clicking delete triggers the unwire confirmation dialog: `Parameter "<name>" is wired into 1 field. Delete it and unwire all referencing fields?`.
   - Accepting confirmation executes `cleanParamRefs`, unwiring `when` / `forEach` and successfully deleting the parameter without 409 errors.

## Acceptance Test

Playwright test in `tests/cf285-when-foreach-param-fanout.spec.js`:
1. Seed blueprint with boolean parameter wired into `work-queue.when: "params.enableQueue"`.
2. Verify fan-out displays `×1`.
3. Accept delete prompt and verify parameter is deleted and `when` condition is unwired.
4. Seed blueprint with integer parameter wired into `work-queue.forEach: "params.replicas"`.
5. Verify palette Shared rail displays `1 bound`.
6. Accept delete prompt and verify parameter is deleted and `forEach` loop is unwired.

## Verification

```sh
npm run lint:js
CF_E2E_PORT=8090 npx playwright test tests/cf285-when-foreach-param-fanout.spec.js
```
