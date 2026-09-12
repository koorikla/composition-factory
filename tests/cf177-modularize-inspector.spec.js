const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-177 — Modularize inspector.js into xrd.js, preview.js, events.js', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('inspector modular submodules are importable with expected exports and canvas runs error-free', async ({ page }) => {
    page.on('dialog', d => d.accept());
    await page.goto('/');
    await canvasSettled(page);

    // 1. Verify inspector modular submodules exist and can be loaded as native ES modules
    const moduleExports = await page.evaluate(async () => {
      const xrd = await import('./js/regions/inspector/xrd.js');
      const preview = await import('./js/regions/inspector/preview.js');
      const events = await import('./js/regions/inspector/events.js');

      return {
        xrd: {
          hasRenderXRD: typeof xrd.renderXRD === 'function',
          hasParamsOf: typeof xrd.paramsOf === 'function',
          hasIsParamLocked: typeof xrd.isParamLocked === 'function',
          hasCleanParamRefs: typeof xrd.cleanParamRefs === 'function',
        },
        preview: {
          hasRawEditorHtml: typeof preview.rawEditorHtml === 'function',
          hasBuildSnippets: typeof preview.buildSnippets === 'function',
          hasTriggerPreview: typeof preview.triggerExpressionPreview === 'function',
          hasUpdateAllPreviews: typeof preview.updateAllPreviews === 'function',
          hasInsertSnippet: typeof preview.insertSnippetIntoTextarea === 'function',
        },
        events: {
          hasCommitValue: typeof events.commitValue === 'function',
          hasCommitEnvelopeValue: typeof events.commitEnvelopeValue === 'function',
          hasOnBoxClick: typeof events.onBoxClick === 'function',
          hasOnBoxChange: typeof events.onBoxChange === 'function',
          hasBindInspectorEvents: typeof events.bindInspectorEvents === 'function',
        },
      };
    });

    expect(moduleExports.xrd.hasRenderXRD).toBe(true);
    expect(moduleExports.xrd.hasParamsOf).toBe(true);
    expect(moduleExports.xrd.hasIsParamLocked).toBe(true);
    expect(moduleExports.xrd.hasCleanParamRefs).toBe(true);

    const paramLockedResults = await page.evaluate(async () => {
      const xrd = await import('./js/regions/inspector/xrd.js');
      return {
        crdManifest: xrd.isParamLocked({
          spec: {
            xrd: { scope: 'Namespaced' },
            sources: [{ crds: 'crds/custom.yaml' }],
            resources: [{ provider: 'crds/custom.yaml' }],
          },
        }, 'providerName'),
        yamlSuffix: xrd.isParamLocked({
          spec: {
            xrd: { scope: 'Namespaced' },
            resources: [{ provider: 'custom.yaml' }],
          },
        }, 'providerName'),
        managed: xrd.isParamLocked({
          spec: {
            xrd: { scope: 'Namespaced' },
            resources: [{ provider: 'xpkg.upbound.io/upbound/provider-aws-rds:v1.14.0' }],
          },
        }, 'providerName'),
      };
    });
    expect(paramLockedResults.crdManifest).toBe(false);
    expect(paramLockedResults.yamlSuffix).toBe(false);
    expect(paramLockedResults.managed).toBe(true);

    expect(moduleExports.preview.hasRawEditorHtml).toBe(true);
    expect(moduleExports.preview.hasBuildSnippets).toBe(true);
    expect(moduleExports.preview.hasTriggerPreview).toBe(true);
    expect(moduleExports.preview.hasUpdateAllPreviews).toBe(true);
    expect(moduleExports.preview.hasInsertSnippet).toBe(true);

    expect(moduleExports.events.hasCommitValue).toBe(true);
    expect(moduleExports.events.hasCommitEnvelopeValue).toBe(true);
    expect(moduleExports.events.hasOnBoxClick).toBe(true);
    expect(moduleExports.events.hasOnBoxChange).toBe(true);
    expect(moduleExports.events.hasBindInspectorEvents).toBe(true);

    // 2. XRD Inspector functions properly via modular xrd.js
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();
    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const addParamBtn = inspector.locator('#addParamBtn');
    await expect(addParamBtn).toBeVisible();
    await addParamBtn.click();
    const newParamInput = inspector.locator('input[data-pn="newParam"]');
    await expect(newParamInput).toBeVisible();

    // 3. Resource Inspector & Preview functions properly via preview.js and events.js
    const resCard = page.locator('.node[data-id="work-queue"]');
    await resCard.locator('.node-h').click();
    await expect(inspector.locator('.insp-t .k')).toContainText('Queue');

    // Switch first field mode to Raw ('r')
    const rawModeBtn = inspector.locator('.fld .modes button[data-m="r"]').first();
    await rawModeBtn.click();
    const rawTextarea = inspector.locator('textarea.raw').first();
    await expect(rawTextarea).toBeVisible();

    // Verify snippet bar rendered by preview.js
    const snippetSelect = inspector.locator('select.snippet-select').first();
    await expect(snippetSelect).toBeVisible();

    // Type a template expression and check that live preview triggers
    await rawTextarea.fill('{{ $xr }}-test');
    await rawTextarea.dispatchEvent('input');
    const previewEl = inspector.locator('.expr-preview').first();
    await expect(previewEl).toBeVisible();
  });

  test('inspector.js is modularized into submodules and its line count is reduced under 1600 lines', async () => {
    const fs = require('fs');
    const path = require('path');
    const inspectorPath = path.join(__dirname, '..', 'web-proto', 'js', 'regions', 'inspector.js');
    const xrdPath = path.join(__dirname, '..', 'web-proto', 'js', 'regions', 'inspector', 'xrd.js');
    const previewPath = path.join(__dirname, '..', 'web-proto', 'js', 'regions', 'inspector', 'preview.js');
    const eventsPath = path.join(__dirname, '..', 'web-proto', 'js', 'regions', 'inspector', 'events.js');

    expect(fs.existsSync(xrdPath)).toBe(true);
    expect(fs.existsSync(previewPath)).toBe(true);
    expect(fs.existsSync(eventsPath)).toBe(true);

    const inspectorLines = fs.readFileSync(inspectorPath, 'utf8').split('\n').length;
    expect(inspectorLines).toBeLessThan(1600);
  });
});
