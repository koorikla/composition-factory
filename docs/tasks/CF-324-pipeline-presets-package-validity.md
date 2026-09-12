# CF-324 — pipeline presets hardcode non-existent function-custom and outdated function-cel-filter packages

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: Adding custom or CEL filter steps via inspector presets inserts non-existent image references that break validation) |
| **Closes** | `#213` — `CF-324 — pipeline presets hardcode non-existent function-custom and outdated function-cel-filter packages` |
| **Worktree** | `.worktrees/CF-324` on branch `CF-324-pipeline-presets-package-validity` |
| **May write** | `web-proto/js/regions/inspector/events.js`, `tests/cf324-pipeline-presets-packages.spec.js` |
| **Merges after** | `nothing` |

## Symptom

In `web-proto/js/regions/inspector/events.js:382-414`, `pipePresetMap` defines preset packages for adding pipeline steps from the XRD inspector dropdown:
- The `"custom"` preset hardcodes `package: "xpkg.crossplane.io/crossplane-contrib/function-custom:v0.1.0"`, which does not exist on the registry.
- The `"cel-filter"` preset hardcodes `package: "xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.3.0"`, which does not exist on the registry (the released tag is `v0.2.0`).

When a user adds either preset, `POST /api/render` fails permanently with Docker daemon pull / resolution errors.

## Acceptance test

```javascript
// tests/cf324-pipeline-presets-packages.spec.js
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');
guardPageErrors();

test.describe('Pipeline Presets Package Validity', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('custom preset should not insert non-existent package that breaks validation', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="xrd"] .node-h');
    await page.selectOption('#pipePresetSelect', 'custom');
    await page.click('#addPipeStepBtn');

    let doc;
    await expect.poll(async () => {
      doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.pipeline?.find(s => s.name === 'custom-step')?.name;
    }).toBe('custom-step');

    const customStep = doc.spec.pipeline.find(s => s.name === 'custom-step');
    expect(customStep.package || '').toBe('');
  });

  test('cel-filter preset should use valid published package v0.2.0 instead of v0.3.0', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="xrd"] .node-h');
    await page.selectOption('#pipePresetSelect', 'cel-filter');
    await page.click('#addPipeStepBtn');

    let doc;
    await expect.poll(async () => {
      doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.pipeline?.find(s => s.name === 'cel-filter')?.name;
    }).toBe('cel-filter');

    const celStep = doc.spec.pipeline.find(s => s.name === 'cel-filter');
    expect(celStep.package).toBe('xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0');
  });
});
```

## Contract

1. In `web-proto/js/regions/inspector/events.js`:
   - In `pipePresetMap`:
     - Change `"cel-filter"` package to `xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0`.
     - Change `"custom"` package to `""` (or omit it) so it does not inject a non-existent package reference.
2. Add Playwright test in `tests/cf324-pipeline-presets-packages.spec.js`.
3. Verify `npx playwright test tests/cf324-pipeline-presets-packages.spec.js` passes.

## Verification

```sh
make lint && npx playwright test tests/cf324-pipeline-presets-packages.spec.js
```

## Out of scope

- Changes outside `web-proto/` or `tests/`.

## Handover

Branch `CF-324-pipeline-presets-package-validity`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
