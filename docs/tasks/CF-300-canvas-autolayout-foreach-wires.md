# CF-300 — Canvas auto-layout ignores forEach status wires, placing dependent in same layer as source

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: auto-layout places dependent resource in same layer as upstream source) |
| **Closes** | `#188` — `CF-300 — Canvas auto-layout ignores forEach status wires, placing dependent in same layer as source` |
| **Worktree** | `.worktrees/CF-300` on branch `CF-300-canvas-autolayout-foreach-wires` |
| **May write** | `web-proto/js/wires.js`, `web-proto/js/regions/canvas/layout.js`, `tests/cf300-canvas-autolayout-foreach.spec.js` |
| **Merges after** | `nothing` |

## Symptom

When a resource iterates using a status wire in `forEach` (e.g., `forEach: "resources.cluster.status.subnets"`), running canvas auto-layout (or opening the canvas with auto-placement) places the dependent resource in layer 1 (the same horizontal column as `cluster`), instead of placing it in a subsequent column (layer >= 2).
Because `dependencyLayers(doc)` computes layout layers strictly from `listWires(doc)`, and `listWires` only inspects `r.fields`, `r.envelope`, and `r.annotations`, status dependencies expressed in `forEach` are omitted from the dependency graph.

## Evidence

In `web-proto/js/regions/canvas/layout.js:31-41`:
```javascript
export function dependencyLayers(d) {
  const layers = {};
  const rs = (d && d.spec && d.spec.resources) || [];
  const deps = {};
  rs.forEach(function (r) { deps[r.name] = new Set(); });
  listWires(d).forEach(function (w) {
    if (w.kind === "status" && deps[w.resource] && w.srcResource !== w.resource) {
      deps[w.resource].add(w.srcResource);
    }
  });
...
```

When evaluated with:
```javascript
const doc = {
  spec: {
    resources: [
      { name: "vpc", type: "VPC" },
      { name: "subnet", type: "Subnet", forEach: "resources.vpc.status.subnets" }
    ]
  }
};
console.log(dependencyLayers(doc));
// Logs: { vpc: 1, subnet: 1 } instead of { vpc: 1, subnet: 2 }
```

## Location

1. `web-proto/js/wires.js:52-110`: `listWires(doc)` collects wires from `r.fields`, `r.envelope`, and `r.annotations`, but never inspects `r.forEach`.
2. `web-proto/js/regions/canvas/layout.js:31-52`: `dependencyLayers(d)` iterates over `listWires(d)` to build `deps`.

## Acceptance test

```javascript
// tests/cf300-canvas-autolayout-foreach.spec.js
import { test, expect } from '@playwright/test';
import { dependencyLayers } from '../web-proto/js/regions/canvas/layout.js';
import { listWires } from '../web-proto/js/wires.js';

test('CF-300: listWires and dependencyLayers respect forEach status wires', async () => {
  const doc = {
    apiVersion: "factory.crossplane.io/v1alpha1",
    kind: "Blueprint",
    metadata: { name: "test-foreach-layout" },
    spec: {
      resources: [
        { name: "cluster", type: "Cluster" },
        { name: "nodegroup", type: "NodeGroup", forEach: "resources.cluster.status.subnets" }
      ]
    }
  };

  const wires = listWires(doc);
  const forEachWire = wires.find(w => w.kind === "status" && w.resource === "nodegroup" && w.srcResource === "cluster");
  expect(forEachWire).toBeDefined();

  const layers = dependencyLayers(doc);
  expect(layers["cluster"]).toBe(1);
  expect(layers["nodegroup"]).toBe(2);
});
```

**Fails today with:**
```
expect(forEachWire).toBeDefined() -> received undefined
expect(layers["nodegroup"]).toBe(2) -> received 1
```

## Contract

1. In `web-proto/js/wires.js`:
   - `listWires(doc)` must parse `r.forEach` when present. If it matches a status wire (`resources.<src>.status.<path>`), emit a wire with `kind: "status"`, `srcResource: <src>`, `srcPath: <path>`, `resource: r.name`, `path: "forEach"`.
2. In `web-proto/js/regions/canvas/layout.js`:
   - `dependencyLayers(d)` must correctly place resources depending on upstream resources via `forEach` in a layer strictly greater than the source resource's layer.

## Verification

```sh
npm run lint:js
npx playwright test tests/cf300-canvas-autolayout-foreach.spec.js
make test
```

## Out of scope

- Wires between resources through custom functions outside blueprint resources.

## Handover

Branch `CF-300-canvas-autolayout-foreach-wires`, committed, not pushed, not merged. Include test outputs in your handover note.
