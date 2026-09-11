# CF-177 — inspector.js is a 2400-line monolith coupling XRD rendering, expression preview, and form mutations

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P3 (UX scale: architectural refactoring) |
| **Closes** | `CF-177 — inspector.js is a 2400-line monolith coupling XRD rendering, expression preview, and form mutations` |
| **Worktree** | `.worktrees/CF-177` on branch `CF-177-inspector-modularization` |
| **May write** | `web-proto/js/regions/inspector.js`, `web-proto/js/regions/inspector/xrd.js`, `web-proto/js/regions/inspector/preview.js`, `web-proto/js/regions/inspector/events.js`, `tests/` |
| **Merges after** | CF-179 |

## Symptom

`web-proto/js/regions/inspector.js` (3,240 lines) combines XRD composite form rendering, CEL/Gotpl expression preview, and large form mutation event handlers in a single massive file. Modifying field behavior incurs high cognitive load and elevated bug risks across unrelated UI sections.

## Evidence

In `web-proto/js/regions/inspector.js`:
- XRD composite definition forms and plural/group/kind rendering.
- Expression preview logic (`/api/preview-expression`) and syntax hint overlays.
- Envelope, annotation, and field mutation handlers (`commitEnvelopeValue`, `onBoxChange`).

## Contract

1. **Modularize sub-domains**:
   - `web-proto/js/regions/inspector/xrd.js`: XRD definition form rendering, metadata fields, version/scope pickers, and plural derivation.
   - `web-proto/js/regions/inspector/preview.js`: CEL / Gotpl live preview dispatch, expression formatting, and template evaluation feedback.
   - `web-proto/js/regions/inspector/events.js`: Form change listeners, field commit debounce, and store dispatch.
2. **Preserve native ES imports**: Use relative ES module imports, maintaining zero-build architecture.
3. **Pass all E2E tests**: Full inspector test suite (XRD edits, parameter bindings, raw template previews, annotations, env keys) must pass without regression.

## Verification

```sh
make lint && make lint-strict && make test
rm -rf .testrun* test-results && make test-e2e
```

## Handover

Branch `CF-177-inspector-modularization`, committed, not pushed, not merged. Final report includes passing runs of the full test suite.
