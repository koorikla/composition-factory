// tests/cf324-pipeline-presets-packages.spec.js
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');
guardPageErrors();

test.describe('Pipeline Presets Package Validity', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('custom preset prompts for package reference and does not insert non-existent package', async ({ page, request }) => {
    page.on('dialog', async dialog => {
      await dialog.accept('xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0');
    });
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
    expect(customStep.package).toBe('xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0');
  });

  test('custom preset dismissed does not add step', async ({ page, request }) => {
    page.on('dialog', async dialog => {
      await dialog.dismiss();
    });
    await page.goto('/');
    await page.click('.node[data-id="xrd"] .node-h');
    await page.selectOption('#pipePresetSelect', 'custom');
    await page.click('#addPipeStepBtn');

    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(doc.spec.pipeline?.find(s => s.name === 'custom-step')).toBeUndefined();
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
