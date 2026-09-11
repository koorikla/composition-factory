# Canvas UX QA Testing Session Report

- **Date:** 2026-09-11
- **Tester:** Canvas UX QA Tester
- **Target Environment:** `http://127.0.0.1:8090`
- **Scratch Directory:** `.testrun-ux`
- **Engine Command:** `./bin/cf serve --addr 127.0.0.1:8090 --blueprint .testrun-ux/doc.cf.yaml --out .testrun-ux/out --lock .testrun-ux/.cf.lock`

---

## 1. Executive Summary

A comprehensive automated and exploratory UX testing session was conducted on the Composition Factory web canvas UI across six primary test focus areas:
1. Console & Page Error Monitoring
2. Parameter Operations (Types, Rename, Delete, Unwire Confirmation)
3. Resource Operations (Catalogue, Inspector Modes, Wires, Unwire, ForEach, When)
4. Environment & Pipeline Operations (EnvironmentConfig inspector, Sync, Pipeline pinning)
5. Canvas Keyboard & State (Delete/Backspace, Escape, Undo/Redo)
6. Topbar & Modals (Engine selector, Starter blueprints dialog, Generate, Package)

### Findings Summary
- **Zero Uncaught Page Errors:** No unhandled JavaScript exceptions or `pageerror` events occurred across normal canvas interactions.
- **2 Verified UX Defects Discovered & Filed:**
  - **CF-284 (P2 / scale:ux):** `Canvas Escape key shortcut ignores active card selection and fails to clear selection` (Issue #172). Pinned in `tests/cf284-escape-clears-selection.spec.js`.
  - **CF-285 (P2 / scale:ux):** `fanOut ignores parameters referenced in when or forEach guards, failing deletion with HTTP 409` (Issue #173). Pinned in `tests/cf285-when-foreach-param-fanout.spec.js`.

---

## 2. Test Execution by Focus Area

### Area 1: Console & Page Error Monitoring
- Monitored `pageerror` and `console.error` listeners across all canvas interactions.
- Live preview auto-generation properly handles required field absences (e.g. after removing a wired required parameter) by reflecting render diagnostics in the bottom drawer rather than crashing the interface.

### Area 2: Parameter Operations
- **Type Addition:** Successfully tested adding `string`, `boolean`, `number`, `integer`, and `object` parameters via the `#param-add-form` in the Shared rail. The UI dynamically adapts input controls based on chosen type (hiding defaults/enums for objects, showing member builders).
- **Array Parameter Handling:** Verified that OpenAPI schema type `array` is intentionally excluded from `PARAM_TYPES` because backend Crossplane XRD emission does not support freeform array parameters in M1.
- **Renaming:** In the XRD inspector, renaming a parameter updates both the backend XRD definition and rewires all referencing resource fields across the document via `POST /api/blueprint/parameters/{name}/rename`.
- **Deleting Unwired Parameters:** Unwired parameters are safely deleted without prompt in the inspector or with confirmation in the palette rail.
- **Deleting Wired Parameters:** Wired parameters trigger an unwire prompt modal (`Parameter "<name>" is wired into N field(s). Delete it and unwire all referencing fields?`), executing `cleanParamRefs` and safely unwiring references.
- **Defect Discovered (CF-285):** When a parameter is referenced exclusively by a resource's `when` condition or `forEach` loop, `fanOut` evaluates to 0. Deletion skips the unwiring prompt and directly issues `DELETE /api/blueprint/parameters/{name}`, which the backend refuses with HTTP 409 Conflict.

### Area 3: Resource Operations
- **Catalogue:** Kinds catalogue rail (`#rtabs button[data-r="kinds"]`) displays installed provider kinds (e.g. `Queue`), search filtering functions cleanly.
- **Card Selection & Inspector:** Clicking a resource card opens the Inspector panel displaying leaf fields, depth indentation, and wire bindings.
- **Inspector Modes:**
  - Segmented filters (`Required`, `Set`, `All`) correctly toggle field visibility.
  - Per-field mode toggles (`Val`, `Wire`, `Raw`) switch between literal values, parameter/status wires, and Go template expressions.
  - Wire dropdown `<select data-w="...">` binds parameter wires and redraws canvas SVG splines immediately.
  - Unwire buttons (`[data-unwire]`) drop wire bindings and update the blueprint.
- **Loops & Conditions:**
  - `data-foreach`: Allows selecting integer parameters or status counts to repeat a resource.
  - `data-when-param`: Allows selecting boolean or enum string parameters to guard resource composition.

### Area 4: Environment & Pipeline Operations
- **Environment Keys:** Adding keys via the palette Shared rail persists to `spec.environment`.
- **EnvironmentConfig Card:** Selecting the EnvironmentConfig node opens the environment inspector. Renaming selection (`#envSelName`) synchronizes with `spec.environmentConfigs`.
- **Pipeline Operations:** Pinning the `auto-ready` step (`#addAutoReadyBtn`) adds the step to `spec.pipeline`.

### Area 5: Canvas Keyboard & State
- **Undo/Redo:** `Meta+Z` / `Control+Z` properly rolls back canvas mutations (such as resource deletion or field edits) through the server-backed document history without stealing focus from active text fields.
- **Card Deletion via Keyboard:** Selecting a resource card and pressing `Delete` or `Backspace` prompts confirmation and deletes the card.
- **Wire Deletion via Keyboard:** Clicking a wire highlights it (`.wire-selected`) and pressing `Delete` or `Backspace` removes the wire.
- **Defect Discovered (CF-284):** Pressing `Escape` while a node (resource, XRD, or EnvironmentConfig) is selected fails to clear the selection. The node retains `.sel` and the Inspector remains open.

### Area 6: Topbar & Modals
- **Engine Selector (`#engineSel`):** Reflects the current emitter engine (`go-templating`, `kcl`, `python`). Switching to engines that conflict with template conventions displays an error toast and prevents UI desynchronization.
- **Starter Blueprints Modal (`#examplesBtn`):** Successfully opens modal displaying all curated examples (IRSA, RDS, K8s App, S3, SQS, CronJob). Closing via Escape or `#examplesCloseBtn` works smoothly.
- **Generate Flow (`#generateBtn`):** Displays tooltip with target output directory and warns before overwriting existing files; prompts confirmation modal before writing.
- **Package Flow (`#packageBtn`):** Downloads `.xpkg` bundle containing the compiled Composition and XRD package.

---

## 3. Discovered Defects Detail

### 1. CF-284: Canvas Escape key shortcut ignores active card selection and fails to clear selection
- **Severity:** P2
- **Scale:** `scale:ux`
- **Issue:** https://github.com/koorikla/composition-factory/issues/172
- **Task Brief:** `docs/tasks/CF-284-canvas-escape-clears-selection.md`
- **Test Spec:** `tests/cf284-escape-clears-selection.spec.js`
- **Root Cause:** In `web-proto/js/regions/canvas.js:1235`, `onKeyDown(e)` only handles `Escape` when `selectedWire` is active. If a card is selected (`S.state.selectedResource`), `Escape` is ignored and `S.select(null)` is never called.

### 2. CF-285: fanOut ignores parameters referenced in when or forEach guards, failing deletion with HTTP 409
- **Severity:** P2
- **Scale:** `scale:ux`
- **Issue:** https://github.com/koorikla/composition-factory/issues/173
- **Task Brief:** `docs/tasks/CF-285-when-foreach-param-fanout.md`
- **Test Spec:** `tests/cf285-when-foreach-param-fanout.spec.js`
- **Root Cause:** In `web-proto/js/wires.js:130`, `fanOutMap` only reads from `listWires(doc)`, which does not scan `r.when` or `r.forEach`. Parameters referenced only by resource guards evaluate to `fanOut = 0`, causing parameter deletion to bypass `cleanParamRefs` and issue an uncoordinated `DELETE /api/blueprint/parameters/{name}` that aborts with HTTP 409 Conflict.
