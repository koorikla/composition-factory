# Canvas UX QA Testing Session Report

- **Date:** 2026-09-12
- **Tester:** Canvas UX QA Tester
- **Target Environment:** `http://127.0.0.1:8090`
- **Scratch Directory:** `.testrun-ux`
- **Engine Command:** `./bin/cf serve --addr 127.0.0.1:8090 --blueprint .testrun-ux/doc.cf.yaml --out .testrun-ux/out --lock .testrun-ux/.cf.lock`

---

## 1. Executive Summary

An extensive manual and automated exploratory UX testing session was conducted on the visual canvas web UI running in an isolated test environment on port 8090.
Testing exercised end-to-end user journeys including Mission 1 (Blank start to generated S3 bucket and round-trip re-import), Mission 4 (Adopting hand-written Composition), card and wire manipulation, environment config cards, and engine switching.

### Findings Summary
- **Zero Uncaught Page Errors:** No unhandled JavaScript exceptions or console errors occurred during typical canvas navigation and editing.
- **Round-Trip Integrity Verified:** Generation of Crossplane Composition + XRD manifests and subsequent import back through the UI preserved all resources, wire bindings, and parameter definitions without loss.
- **1 Verified High-Signal UX Defect Discovered & Filed:**
  - **CF-387 (P2 / scale:ux):** `Canvas fails to render wires from object parameter members to composed resource fields` (Issue [#278](https://github.com/koorikla/composition-factory/issues/278)). Pinned in `tests/slice94-object-member-wire.spec.js`.

---

## 2. Test Execution & Journey Details

### Mission 1: Blank Start to Generated S3 Bucket & Round-Trip
- Started from an empty blueprint.
- Searched `s3` in the Kinds palette, drag/placed `Bucket` onto the canvas.
- Switched to the Shared/XRD rail and declared parameter `region` (type `string`).
- Selected the Bucket card, switched `spec.forProvider.region` to Wire mode, and selected `params.region`.
- Observed validation chip: the validate chip correctly detected that `params.region` was optional while `spec.forProvider.region` was required.
- Checked `req` on the XRD card parameter row; the validate chip immediately transitioned to green (`valid · 1 resource`).
- Generated manifests into `.testrun-ux/out` via `#generateBtn`. Manifests were written deterministically.
- Tested round-trip import: imported the generated manifests back via `#importFile`. The blueprint reloaded cleanly with matching resource definitions, fields, and wires.

### Mission 4: Adopt Hand-Written Composition
- Tested importing `testdata/xqueue-pipeline.composition.golden.yaml` via `#importFile`.
- All resources, pipelines, and parameter declarations were imported into the visual canvas with correct card layout and wire connections.
- Validation chip confirmed valid state immediately upon import.

### Advanced Interactions & Component Operations
- **Card Manipulation:** Tested card dragging, moving, resizing, and duplication via context menu (`#ctx-menu` / Ctrl+D). Cards render cleanly with SVG connection ports.
- **Keyboard Navigation:** Verified keyboard navigation across Kinds palette list (ArrowUp/ArrowDown, Enter/Space) for accessible placement of resources onto the canvas.
- **Engine Switching:** Tested switching `#engineSel` between `go-templating`, `kcl`, and `python`. Switching to engines that conflict with template syntax raised informative error toasts and guarded document integrity.
- **Template Sources:** Toggled between Inline and FileSystem modes; editor synchronization worked without desync.
- **EnvironmentConfig:** Tested EnvironmentConfig node, key addition, and secret reference wiring.

---

## 3. Discovered Defect: CF-387

### Canvas fails to render wires from object parameter members to composed resource fields
- **Severity:** P2
- **Scale:** `scale:ux`
- **Issue:** [CF-387 (#278)](https://github.com/koorikla/composition-factory/issues/278)
- **Task Brief:** `docs/tasks/CF-387-canvas-object-member-wires.md`
- **Test Spec:** `tests/slice94-object-member-wire.spec.js`

#### Mechanism
In `web-proto/js/regions/canvas.js:748`:
```javascript
a = portPos(XR_ID, w.param, cwRect);
```
When a resource field is wired from a member of an object parameter (`from: "params.<object>.<member>"`), `w.param` is e.g. `"dbConfig.host"`.
In `canvas.js:676-683`, `portPos(owner, path, cwRect)` executes:
```javascript
const el = canvasEl.querySelector(
  '.port[data-owner="' + CSS.escape(owner) + '"][data-path="' + CSS.escape(path) + '"] .d');
```
looking for `[data-owner="xrd"][data-path="dbConfig.host"]`.
However, `xrCardHTML` in `canvas.js:153-165` only renders port rows for top-level parameters (`data-path="dbConfig"`). It does not render ports for nested members, nor does `portPos` have a fallback to resolve `w.param.split('.')[0]`.
Consequently, `portPos` returns `null`, and `if (!a || !b) return;` at `canvas.js:755` silently skips drawing the wire spline.
Additionally, `fans[w.param]` at `canvas.js:729` keys on `w.param` rather than the root parameter name, preventing shared port styling from activating when multiple wires connect to different members of the same object parameter.

#### Cost to User
On the canvas, wires originating from object parameter members are completely invisible. Users cannot see that the resource is connected to the XRD parameter, cannot hover or click the wire, and cannot delete or select it from the canvas. This creates visual desynchronization where the Inspector and validation chip show the field is wired, but the canvas shows zero wires connected to the resource.

#### Expected Fix Contract
1. Canvas wire rendering must resolve object parameter member wires (`dbConfig.host`) to the parent parameter port (`dbConfig`) on the XRD card.
2. SVG wire spline and hit target must be rendered with title `$dbConfig.host → <resource>.<field>`.
3. Fan-out calculation must key on root parameter prefix so multiple wires from the same parent object parameter render as shared wires.
4. Existing wire selection, hover, and deletion interactions must work for object parameter member wires.
