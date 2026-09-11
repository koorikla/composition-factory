# CF-261 — Pipeline step form parses root keys only, displaying blank inputs and wiping nested fields on edit

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (scale:ux) |
| **Closes** | `CF-261 — Pipeline step form parses root keys only, displaying blank inputs and wiping nested fields on edit` (#149) |
| **Worktree** | `.worktrees/CF-261` on branch `CF-261-pipeline-step-nested-input` |
| **May write** | `web-proto/js/regions/inspector/xrd.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

In the Visual Canvas pipeline step inspector, when a pipeline step has a nested `input` YAML (such as `spec.filter` and `spec.extraField` under `spec:`):
1. The inspector form displays blank inputs for fields with paths like `spec.filter` because `parseInputYAML` only reads unindented lines at column 0 and parses `spec:` as an empty string `""`.
2. When any field in the form is edited or blurred, `setPathVal` and `serializeInputYAML` overwrite `step.input`, completely wiping all sibling and nested fields under `spec:`.

## Mechanism

- `web-proto/js/regions/inspector/xrd.js`: `parseInputYAML` explicitly ignores indented lines:
  ```javascript
  if (colonIdx !== -1 && line.indexOf(" ") !== 0 && line.indexOf("\t") !== 0)
  ```
  It has no recursive indentation or block parser. For `spec:`, `v` is `""`, and all children under `spec:` are dropped.
- When `getPathVal(parsedInp, "spec.filter")` evaluates, `parsedInp.spec` is `""` instead of an object, returning `""`.
- When an input field blurs, `inspector/events.js` calls `setPathVal(obj, path, val)` and `serializeInputYAML(obj)`, replacing `spec` with only the modified field and obliterating other nested keys.

## Contract

1. `parseInputYAML` in `web-proto/js/regions/inspector/xrd.js` must parse nested YAML structures (nested maps/objects, lists, multiline scalars, primitives).
2. `getPathVal(parseInputYAML(yaml), "spec.filter")` must correctly retrieve nested values.
3. `serializeInputYAML` must serialize nested objects and lists without losing nested structure.
4. Editing a nested field in a pipeline step preserves existing sibling fields, nested structures, and lists under `spec` and does not wipe them.

## Acceptance Test

A Playwright test spec (or node unit test spec) in `tests/`:
- Feeds a pipeline step input with nested fields under `spec`:
  ```yaml
  apiVersion: cel.fn.crossplane.io/v1alpha1
  kind: Filter
  spec:
    filter: "true"
    extraField: "preserved"
  ```
- Verifies that `parseInputYAML` parses `spec.filter` as `"true"` and `spec.extraField` as `"preserved"`.
- Modifies `spec.filter` to `"false"` using `setPathVal` and re-serializes with `serializeInputYAML`.
- Parses the result back and asserts that `spec.filter` is `"false"` AND `spec.extraField` remains `"preserved"`.
- End-to-end verifies that the canvas inspector loads the form with populated values for nested paths, and changes survive without wiping siblings.

## Verification

```sh
npm run lint:js
make lint && make lint-strict && make test
```
