# CF-229 — Changing engine to an unsupported choice silently clears error toast and desynchronizes selector

## Severity & Scope
- **Severity**: P1
- **Scale**: ux
- **Touch set**: `web-proto/js/regions/output.js`, `tests/slice93-engine-selector-desync.spec.js`, `docs/tasks/CF-229-engine-selector-desync.md`

## Background & Problem
When switching the pipeline engine via the `#engineSel` select dropdown in the UI to an engine that is rejected by server validation (for example, selecting `kcl` or `python` on a blueprint containing `conventions` or template fields), the UI silently dismisses the server validation error toast within milliseconds and leaves the dropdown displaying the rejected engine, desynchronizing the visual UI from the actual persisted document state.

### Root Cause
In `web-proto/js/regions/output.js:1162-1178`:
```javascript
  if (el.engineSel) {
    el.engineSel.addEventListener("change", function () {
      var val = el.engineSel.value;
      store.replaceDoc(function (doc) {
        ...
      }).then(function () { store.generate(false); });
    });
  }
```
1. `store.replaceDoc` catches server 400 errors, fires `"error"` (which main.js listens to and displays `#canvas-error-toast`), and resolves to `null`.
2. The chained `.then(function () { store.generate(false); })` runs unconditionally.
3. `store.generate(false)` runs against unchanged document, succeeds, and fires `"generate"`.
4. In `main.js:102-104`, `"generate"` listener calls `clearErrorToast()`, instantly wiping the toast.
5. `#engineSel` remains set to the invalid option because `"doc"` event is never emitted when `replaceDoc` fails.

## Expected Behavior & Contract
1. In `web-proto/js/regions/output.js`:
   - When `store.replaceDoc` returns `null` (or falsy) in `#engineSel` change handler:
     - Do NOT call `store.generate(false)`.
     - Revert `el.engineSel.value` to the persisted document's engine (`(curDoc && curDoc.spec && curDoc.spec.emit && curDoc.spec.emit.engine) || "go-templating"`).
   - Likewise, in `#tplSource` change handler (`output.js:1181-1200`):
     - When `store.replaceDoc` returns falsy:
       - Do NOT call `store.generate(false)`.
       - Revert `el.tplSource.value` to the persisted document's templateSource (`(curDoc && curDoc.spec && curDoc.spec.emit && curDoc.spec.emit.templateSource) || "Inline"`).

## Acceptance Test
- `tests/slice93-engine-selector-desync.spec.js` passes.
