const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.describe('Pipeline Steps UI Authoring', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('default pipeline shows inferred auto-ready and allows pinning it explicitly', async ({ page, request }) => {
    await page.goto('/');
    
    // Select the XRD node
    await page.click('.node[data-id="xrd"] .node-h');
    
    // Verify Pipeline section exists with default explanation
    await expect(page.locator('#insp')).toContainText('Pipeline (default)');
    await expect(page.locator('#insp')).toContainText('1. render-resources');
    await expect(page.locator('#insp')).toContainText('2. auto-ready');

    // Click "+ Pin step" to make auto-ready explicit
    await page.click('#addAutoReadyBtn');

    // Verify persisted doc has spec.pipeline
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.pipeline?.[0]?.name;
    }).toBe('auto-ready');

    // Verify inspector updates to show editable step
    await expect(page.locator('#insp input[data-pipe-name="0"]')).toHaveValue('auto-ready');
    await expect(page.locator('#insp select[data-pipe-pos="0"]')).toHaveValue('after');
  });

  test('adding a before-render function step updates composition.yaml and functions.yaml output', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="xrd"] .node-h');

    // Select environment-configs preset and click Add Step
    await page.selectOption('#pipePresetSelect', 'environment-configs');
    await page.click('#addPipeStepBtn');

    // Verify doc has environment-configs
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.pipeline?.find(s => s.name === 'environment-configs')?.position;
    }).toBe('before');

    // Switch to functions.yaml tab in output drawer
    await page.click('#tabs button[data-t="fns"]');
    await expect(page.locator('#code')).toContainText('function-environment-configs');
    await expect(page.locator('#code')).toContainText('function-go-templating');

    // Switch to composition.yaml tab in output drawer
    await page.click('#tabs button[data-t="comp"]');
    const compText = await page.locator('#code').textContent();
    expect(compText).toContain('- step: environment-configs');
    expect(compText).toContain('- step: render-resources');
    
    // environment-configs (before) must appear before render-resources
    const envIdx = compText.indexOf('step: environment-configs');
    const rendIdx = compText.indexOf('step: render-resources');
    expect(envIdx).toBeLessThan(rendIdx);

    // Test deleting the step
    await page.click('button[data-pipe-del="0"]');
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.pipeline;
    }).toBeUndefined();
  });
});
