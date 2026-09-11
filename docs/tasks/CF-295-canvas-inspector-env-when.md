# CF-295 — Canvas inspector drops and deletes env when conditions, defaulting to always on edit

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Canvas UX: drop/wipe of declared environment `when` condition on edit) |
| **Closes** | `#183` — `CF-295 — Canvas inspector drops and deletes env when conditions, defaulting to always on edit` |
| **Worktree** | `.worktrees/CF-295` on branch `CF-295-canvas-inspector-env-when` |
| **May write** | `web-proto/js/regions/inspector.js`, `web-proto/js/regions/inspector/xrd.js`, `web-proto/js/regions/inspector/events.js`, `web-proto/js/wires.js`, `tests/` |
| **Merges after** | none |

## Symptom

When a blueprint declares an environment variable in `spec.environment` (e.g. `stage: { type: "string" }` or `enabled: { type: "boolean" }`) and uses it in a resource condition (`when: env.enabled` or `when: env.stage == "prod"`), opening the canvas inspector on that resource displays `— always —` instead of the active `env.` condition. The dropdown choices only enumerate composite XRD parameters (`params.<name>`), omitting all environment keys. If the user touches or interacts with the inspector controls, `whenFromControls` evaluates to `null` and deletes `r.when`, permanently wiping the condition from the blueprint document.

## Evidence

Given a blueprint:

```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: test-env-when
spec:
  sources: []
  xrd:
    group: platform.example.org
    kind: XApp
    plural: xapps
    version: v1alpha1
    scope: Namespaced
    parameters:
      providerName: { type: string, required: true }
  environment:
    stage: { type: string, default: prod }
    enabled: { type: boolean, default: true }
  resources:
    - name: my-config
      kind: ConfigMap
      provider: k8s
      when: env.enabled
```

1. Launch canvas on port 8090 with this document.
2. Click card `my-config`.
3. Check `[data-when-param="my-config"]` selected value:
   - Evaluates to `""` (`— always —`).
   - Options only contain `— always —` and `params.providerName`. `env.enabled` and `env.stage` are not present.
4. Selecting any option or triggering change on inspector controls executes:
   ```javascript
   state.store.replaceDoc(function (d) {
     var r = d.spec.resources.find(function (x) { return x.name === wrn; });
     if (!r) return;
     if (expr) r.when = expr; else delete r.when;
   });
   ```
   `r.when` is deleted (`undefined`), wiping the condition.

## Location

1. `web-proto/js/regions/inspector/xrd.js:12-17`:
   `parseWhen(str)` regex only recognizes `^params\.([A-Za-z][A-Za-z0-9]*)...`, returning `{}` for any `env.` expression.
2. `web-proto/js/regions/inspector.js:839-851`:
   `condParams` is extracted solely from `Object.keys(allParams)` (`doc.spec.xrd.parameters`), ignoring `doc.spec.environment`.
3. `web-proto/js/regions/inspector/events.js:655-667`:
   `whenFromControls` assumes all conditions start with `params.`, querying `paramsOf(doc)` and returning `params.<name>`.
4. `web-proto/js/wires.js:130-139`:
   `parseWhen(str)` fallback regex also only matches `params|parameters|\$params`.

## Acceptance test

Add Playwright E2E test in `tests/`:

```javascript
test('canvas inspector preserves and edits env when conditions', async ({ page, isolatedPort }) => {
  const doc = {
    apiVersion: 'factory.crossplane.io/v1alpha1',
    kind: 'Blueprint',
    metadata: { name: 'test-env-when' },
    spec: {
      sources: [],
      xrd: {
        group: 'platform.example.org',
        kind: 'XApp',
        plural: 'xapps',
        version: 'v1alpha1',
        scope: 'Namespaced',
        parameters: { providerName: { type: 'string', required: true } }
      },
      environment: {
        enabled: { type: 'boolean', default: 'true' },
        stage: { type: 'string', default: 'prod' }
      },
      resources: [
        {
          name: 'my-config',
          kind: 'ConfigMap',
          provider: 'k8s',
          when: 'env.enabled'
        }
      ]
    }
  };

  await resetDoc(page, doc);
  await page.click('.node[data-id="my-config"]');
  await page.waitForSelector('[data-when-param="my-config"]');

  const selected = await page.locator('[data-when-param="my-config"]').inputValue();
  expect(selected).toBe('env.enabled');

  const options = await page.locator('[data-when-param="my-config"] option').allInnerTexts();
  expect(options).toContain('env.enabled');
  expect(options).toContain('env.stage');

  // Modify another inspector field; verify r.when remains intact
  const currentWhen = await page.evaluate(() => window.store.state.doc.spec.resources[0].when);
  expect(currentWhen).toBe('env.enabled');
});
```

**Fails today with:**
```
Expected: "env.enabled"
Received: ""
```

## Contract

1. In `web-proto/js/regions/inspector/xrd.js` and `web-proto/js/wires.js`:
   - Extend `parseWhen` to recognize `(params|env)\.<name>` with optional equality/inequality operators (`==`, `!=`).
2. In `web-proto/js/regions/inspector.js`:
   - Populate `when` dropdown options with both boolean/string `params.<name>` from XRD parameters and boolean/string `env.<name>` from `doc.spec.environment`.
   - Preserve current selection if `res.when` references `env.<key>`.
3. In `web-proto/js/regions/inspector/events.js`:
   - `whenFromControls` must inspect the source of the selected condition (`params.` vs `env.`) and emit the corresponding expression.
   - Do not delete `r.when` when an `env.` condition is active unless the user explicitly switches the selector to `— always —`.

## Verification

```sh
make test
make test-e2e
```

## Handover

Branch `CF-295-canvas-inspector-env-when`, committed, not pushed, not merged.
