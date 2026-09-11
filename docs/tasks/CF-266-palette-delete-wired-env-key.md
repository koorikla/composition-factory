# CF-266 — Deleting a wired environment key from the palette rail hard-aborts instead of offering to unwire

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: palette environment key lifecycle & cascading unwire consistency) |
| **Closes** | `CF-266 — Deleting a wired environment key from the palette rail hard-aborts instead of offering to unwire` |
| **Worktree** | `.worktrees/CF-266` on branch `CF-266-palette-delete-wired-env-key` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/cf266-palette-delete-wired-env-key.spec.js` |
| **Merges after** | `CF-264` |

## Symptom

In the left rail palette under the Environment section (`[data-r="shared"]`), clicking the delete button (`[data-env-del]`) on an environment key currently wired to any resource field immediately blocks the deletion with an inline error in `#lrail`:
`delete environment key "<key>": still referenced by wire <wires>`
The deletion hard-aborts without presenting any confirmation dialog or offer to cascade-unwire referencing fields.

In contrast, the Inspector drawer (`web-proto/js/regions/inspector.js:1120-1138`) confirms with:
`Environment key "<key>" is wired into ${wires.length} field(s). Delete it and unwire all referencing fields?`
and cleanly cascades deletion using `deleteEnvKeyFromDoc(d, keyName)` which calls `cleanEnvRefs`.

## Mechanism

In `web-proto/js/regions/palette.js:1230-1245`:
```javascript
    const edel = e.target.closest("[data-env-del]");
    if (edel) {
      const k = edel.getAttribute("data-env-del");
      const wires = findEnvWires(store.state.doc, k);
      if (wires.length > 0) {
        envErr = 'delete environment key "' + k + '": still referenced by wire ' + wires.join(", ");
        drawRail();
        return;
      }
      if (!window.confirm("Delete environment key $env." + k + "?")) return;
      envErr = null;
      store.replaceDoc(function (d) {
        deleteEnvKeyFromDoc(d, k);
      });
      return;
    }
```
When `wires.length > 0`, the handler unconditionally sets `envErr` and returns. However, `deleteEnvKeyFromDoc(d, k)` in `web-proto/js/utils.js:225-245` already invokes `cleanEnvRefs(d, k)`. The palette handler should prompt the user to confirm cascading unwiring when wires exist rather than hard-aborting.

## Contract

1. In `web-proto/js/regions/palette.js`, when clicking `[data-env-del]`:
   - If `wires.length > 0`:
     Prompt confirmation with message matching the Inspector:
     `Environment key "${k}" is wired into ${wires.length} field(s). Delete it and unwire all referencing fields?`
     If confirmed, clear `envErr` and invoke `store.replaceDoc(d => deleteEnvKeyFromDoc(d, k))`.
   - If `wires.length === 0`:
     Prompt `Delete environment key $env.${k}?` as before.
2. Verify that deleting a wired environment key from the palette rail cleanly removes the key, unwires referencing fields on the canvas, and leaves no error banner.
3. Playwright test in `tests/cf266-palette-delete-wired-env-key.spec.js` verifies this flow.

## Acceptance Test

Playwright test in `tests/cf266-palette-delete-wired-env-key.spec.js`:
1. Launch canvas with a blueprint having an environment key (e.g. `regionKey`) wired to `q.fields.region`.
2. Switch to the Shared tab in the palette rail.
3. Click `[data-env-del="regionKey"]`.
4. Accept the unwire confirmation dialog.
5. Verify no error message appears in the rail.
6. Verify the environment key is deleted and referencing wire is cleanly removed.

## Verification

```sh
make lint && make lint-strict && make test
CF_E2E_PORT=25715 npx playwright test tests/cf266-palette-delete-wired-env-key.spec.js
```

## Handover

Branch `CF-266-palette-delete-wired-env-key`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
