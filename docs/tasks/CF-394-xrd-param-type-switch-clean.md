# CF-394 — Switching parameter type to object preserves enum and default, causing HTTP 400 and blocking type change

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-394 — Switching parameter type to object preserves enum and default, causing HTTP 400 and blocking type change` (#285) |
| **Worktree** | `.worktrees/CF-394` on branch `CF-394-xrd-param-type-switch-clean`, branched from `main` |
| **May write** | `web-proto/js/regions/inspector/xrd.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When changing a parameter's type to `object` in the XRD inspector:
If the parameter previously had `enum` choices or a `default` value, the updated parameter continues to preserve `enum` and `default`.
The backend rejects the resulting blueprint with an HTTP 400 validation error (such as `spec.xrd.parameters.<param>: enum is not valid for type "object"`), causing an error banner to display and preventing the user from switching parameter type.

## Mechanism

In `web-proto/js/regions/inspector/xrd.js:636`, `paramFrom(existing, patch)` copies existing properties:
```javascript
export function paramFrom(existing, patch) {
  var p = {
    type: (existing && existing.type) || "string",
    required: !!(existing && existing.required),
    enum: (existing && existing.enum) || null,
    default: (existing && existing.default) || "",
    description: (existing && existing.description) || "",
    properties: (existing && existing.properties) || null,
  };
  Object.keys(patch).forEach(function (k) { p[k] = patch[k]; });
  return p;
}
```
Unlike `memberHandler["data-mtype"]` in `events.js:954-955` which explicitly clears `properties` when switching away from object, and deletes `default` and `enum` when switching to object, `paramFrom` leaves `enum` and `default` populated when type changes to `object`, and leaves `properties` populated when type changes away from `object`.

## Contract

1. In `web-proto/js/regions/inspector/xrd.js:paramFrom`:
   - If `p.type === "object"`, set `p.enum = null` and `p.default = ""`.
   - If `p.type !== "object"`, set `p.properties = null`.
2. Add a Playwright e2e test verifying that switching a parameter with enum choices and a default to `object` succeeds without backend validation errors or error banners.
3. Pass all gates: `make lint && make lint-strict && make test-race && make test-e2e`.
