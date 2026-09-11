# Task Brief: CF-294 — Canvas exposes unaddressable array status paths as wireable ports, failing validation on drag

## Context & Problem

The visual canvas (`web-proto/js/regions/canvas.js`) and inspector (`web-proto/js/regions/inspector.js`) offer status fields containing array indexing (e.g. `conditions[0].lastTransitionTime`, `conditions[0].status`, `loadBalancer.ingress[0].hostname`) as wireable status outputs with tooltips stating `"status output — other objects can wire from this"`.

When an engineer drags a wire from one of these status ports to another resource's input port (or types it into a field binding), `drag-to-wire.js` constructs `from: resources.<source>.status.conditions[0].status` and calls `store.replaceDoc()`. The backend `ParseFrom` validator in `internal/blueprint/refs.go` strictly rejects array-indexed segments with HTTP 400 (`status path segment "conditions[0]" is not a template identifier ([a-zA-Z_][a-zA-Z0-9_]*); the emitted template dereferences each segment as .segment, so dashed, empty or array-indexed segments (conditions[0]) cannot be addressed`).

As a result, the wire snaps back, the edit is aborted, and a red error banner is displayed. For Kubernetes native resources like `Service` (and any provider CRD whose primary status outputs are arrays like `conditions[]`), status outputs displayed on the canvas card are broken and cannot be wired.

## Root Cause

1. `web-proto/js/regions/canvas.js:960-977`: `statusLeavesFor(meta)` maps `detail.status` to paths without filtering against the template identifier rule.
2. `web-proto/js/regions/inspector.js:975-985`: Lists `detail.status` under "Status Outputs: Other resources can wire from this object's status" without filtering unaddressable array paths.
3. `internal/blueprint/refs.go:80-98`: `ParseFrom` enforces `statusSegmentRE = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]*$")` on each segment, failing any path containing `[` or `]` or characters outside `[a-zA-Z0-9_]`.

## Fix

1. Filter out unaddressable status paths that do not conform to Crossplane status template identifiers:
   - Each segment in the dot-separated status path must match `^[a-zA-Z_][a-zA-Z0-9_]*$`.
   - In `web-proto/js/regions/canvas.js`: `statusLeavesFor` must only include paths whose segments are all valid identifiers (filtering out array paths like `conditions[0]...`, `ingress[0]...`, etc.).
   - In `web-proto/js/regions/inspector.js`: Under "Status Outputs", only display paths whose segments are valid identifiers.
2. Ensure canvas cards only render wireable status ports.

## Acceptance Test

Write Playwright test `tests/cf294-canvas-array-status-ports.spec.js`:
1. Load or add a resource whose status includes array fields (e.g. `Service` with `conditions[0].status`, or mock kind).
2. Verify that canvas card output ports and inspector "Status Outputs" do NOT render array-indexed paths like `conditions[0].status` or `ingress[0].hostname` as wireable ports.
3. Verify that valid addressable status paths (e.g. `atProvider.id`, `loadBalancer.ip`) continue to be rendered and wireable without errors.

## Gates

- `npm run lint:js`
- `make lint`
- `make lint-strict`
- `make test`
- `make test-e2e`
