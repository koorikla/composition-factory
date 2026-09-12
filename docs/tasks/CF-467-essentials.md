# CF-467 — Inspector needs a per-kind essentials form with expose-as-parameter and an env-variable repeater; a second env entry cannot be added from the GUI

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `#367` — `CF-467 — Inspector needs a per-kind essentials form with expose-as-parameter and an env-variable repeater; a second env entry cannot be added from the GUI` |
| **Worktree** | `.worktrees/prefill` on branch `prefill-fields` (slice 2 of `docs/superpowers/specs/2026-09-12-prefilled-fields-design.md`) |
| **May write** | `web-proto/js/profiles.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/regions/inspector/events.js`, `tests/cf467-essentials-form.spec.js` (new) |
| **Merges after** | CF-466 |

## Symptom

Selecting a Deployment shows a four-field workload card, then the 848-leaf schema list. The
schema addresses arrays only as `[0]`, so `containers[0].env[0]` is the only env entry the
inspector can ever set: there is no control that writes `env[1].name`. Exposing a field as
an XRD parameter takes the "+ new XRD parameter…" flow per field with no default carried
over. Provider kinds (Queue) show every required-looking leaf.

## Evidence

Screenshot of the All view on 1335360 (2026-09-12): rows
`spec.template.spec.containers[0].env[0].valueFrom.secretKeyRef…`,
`…envFrom[0].configMapRef.name`; paths truncate to `s…`/`spec…` in the pane width. No
`[1]` row exists and no control adds one. `grep -n "search" web-proto/js/regions/inspector.js`
finds only the text "expand via All / search" — there is no search.

## Location

`web-proto/js/regions/inspector.js:604-760` — `workloadPresetHtml`, the four-field card for
three kinds plus the Service card. `web-proto/js/regions/inspector/events.js:576-585` —
`wlSimpleFieldMap`. `web-proto/js/regions/inspector/events.js:224-250` — the new-parameter
flow the expose action reuses (`addParameter` then `setField`).

## Acceptance test

`tests/cf467-essentials-form.spec.js` exactly as given in
`docs/superpowers/plans/2026-09-12-prefilled-fields.md` Task 4.

**Fails today with:** not run by the brief author; the implementer pastes the first failing
run into the handover.

## Contract

- Every selected resource opens with an `.essentials` section before any field list. Rows
  come from the kind profile (design §2); a kind without a profile shows chain-required
  leaves, `region`, and set fields.
- Each row: label, path, type, a typed control (number input for integer/number, select for
  enum and boolean, text otherwise), the existing Val / Wire / Raw mode buttons, and for
  rows with a `param` name an expose button that creates the XRD parameter (type from the
  row, default = current literal, not required) and wires the field. A name collision
  appends the resource name in CamelCase.
- Workload kinds end with an env repeater: a row per `containers[0].env[i]` with NAME and a
  value control that accepts a literal or a wire; add appends index n; delete removes index i
  and renumbers the rest down.
- The app-label row keeps `input[data-wl-app]`, `button[data-wl-sync-app]`, `.workload-card`
  and the "Selectors Aligned" badge (slice63/65 specs depend on them).

## Verification

```sh
npx playwright test tests/cf467-essentials-form.spec.js tests/cf466-starter-deployment.spec.js tests/cf439-auto-scaffold-drop.spec.js tests/slice63-selectors-functions.spec.js tests/slice65-authoring-ux-enhancements.spec.js
make lint
```

## Out of scope

Field search and the manifest editor (CF-468); array repeaters beyond env.

## Handover

Commits on `prefill-fields`; failing and passing runs pasted on the issue.
