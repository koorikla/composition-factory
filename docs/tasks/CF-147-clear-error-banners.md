# CF-147 — Error banners and toasts in SOURCES and the inspector never clear on their own

## Severity & Scope
- **Severity**: P3
- **Scale**: ux
- **Touch set**: `web-proto/js/regions/palette.js`, `web-proto/js/regions/inspector.js`, `web-proto/js/main.js`, `tests/cf147-clear-error-banners.spec.js`

## Background & Problem
In UX runs M3 (F8) and M2 (F1):
1. In the SOURCES rail (`palette.js`), attempting to delete an installed provider that is still referenced by resources produces a refusal banner (`.warnbar[role="alert"]`). This banner stayed permanently across starter loads, undos, and searches because `providersErr` was never cleared on doc change, search input, or tab switch.
2. In the Inspector (`inspector.js`), validation or mutation errors produce a `.warnbar` banner and/or a toast. Inspector error state (`warnMsg`) was never cleared when the document changed (e.g. undo, redo, starter load, editor apply), nor on generate, nor when the user began typing into the form inputs to correct the issue.
3. In `main.js`, global error toasts attached to rejected actions should also clear when canvas selection changes.

## Contract & Expected Behavior
1. **Contract**: An error attached to an action clears on the next successful action in that surface (or on a doc change), and never outlives the document it described.
2. **SOURCES / Rail (`web-proto/js/regions/palette.js`)**:
   - In `store.subscribe("doc")`: reset `providersErr = null`, `paramErr = null`, `envErr = null`, `clusterErr = null`. Doc changes (starter load, undo, redo, import) must never inherit errors from prior document states.
   - In `setRail(r)`: clear `providersErr = null`, `paramErr = null`, `envErr = null`, `clusterErr = null` when switching tabs.
   - On `#cat-search` input or catalogue search results: clear `providersErr = null; drawRail();`.
   - On `#src-add-ref` input: if `providersErr` is set, clear `providersErr = null; drawRail();`.
   - On `#param-add-name` / `#param-add-default` input: clear `paramErr = null; drawRail();`.
   - On `#env-add-name` / `#env-add-default` input: clear `envErr = null; drawRail();`.
3. **Inspector (`web-proto/js/regions/inspector.js`)**:
   - In `store.subscribe("doc")`: reset `warnMsg = null`.
   - In `store.subscribe("generate")`: reset `warnMsg = null; render();`.
   - On user input in inspector (`box.addEventListener("input")`): if `warnMsg` is set, clear `warnMsg = null;` and remove `.warnbar` from `box` so errors clear immediately as the user edits inputs.
4. **Global Toasts (`web-proto/js/main.js`)**:
   - In `store.subscribe("selection")`: call `clearErrorToast()`.

## Acceptance Test
Create Playwright test `tests/cf147-clear-error-banners.spec.js`:
1. **SOURCES refusal banner clears on doc change and search**:
   - Open canvas with resources referencing a provider.
   - Switch to SOURCES tab. Attempt to remove the referenced provider -> refusal error banner appears.
   - Type in the `#cat-search` box -> refusal error banner is cleared.
   - Trigger refusal banner again -> select starter or trigger doc change (or undo) -> refusal error banner is cleared.
2. **Inspector error banner clears on doc change and input**:
   - Select a resource, attempt to add annotation with empty value -> error banner appears in inspector.
   - Type into the annotation value input -> error banner is cleared immediately.
   - Trigger inspector error again -> load starter or trigger doc change -> inspector error banner is cleared.
