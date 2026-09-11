const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-295 — Canvas inspector preserves and edits env when conditions', () => {
  guardPageErrors();

  test('canvas inspector preserves and edits env when conditions', async ({ page }) => {
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
          parameters: {
            providerName: { type: 'string', required: true },
            replicas: { type: 'integer', default: '2' }
          }
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
    expect(options).toContain('params.providerName');

    // Modify another inspector field; verify r.when remains intact
    await page.selectOption('[data-foreach="my-config"]', 'params.replicas');
    await expect.poll(async () => {
      return page.evaluate(() => {
        const r = window.store.state.doc.spec.resources.find(x => x.name === 'my-config');
        return r && r.forEach;
      });
    }).toBe('params.replicas');

    const currentWhen = await page.evaluate(() => window.store.state.doc.spec.resources[0].when);
    expect(currentWhen).toBe('env.enabled');

    // Test changing the when condition to env.stage == "prod" and verify doc is updated correctly
    await page.selectOption('[data-when-param="my-config"]', 'env.stage');
    await page.waitForSelector('[data-when-op="my-config"]');
    await page.waitForSelector('[data-when-val="my-config"]');

    await page.selectOption('[data-when-op="my-config"]', '==');
    await page.fill('[data-when-val="my-config"]', 'prod');
    await page.dispatchEvent('[data-when-val="my-config"]', 'change');

    await expect.poll(async () => {
      return page.evaluate(() => {
        const r = window.store.state.doc.spec.resources.find(x => x.name === 'my-config');
        return r && r.when;
      });
    }).toBe('env.stage == "prod"');
  });
});
