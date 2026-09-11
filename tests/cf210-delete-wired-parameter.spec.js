const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-210 (#101) — Delete wired parameter unwires referencing fields', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting a wired parameter in XRD inspector prompts, unwires referencing fields, and succeeds without 409', async ({ page, request }) => {
    // 1. Seed blueprint with resource main-queue referencing parameter region
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.resources = [
      {
        name: 'main-queue',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.region' },
          messageRetentionSeconds: { from: 'params.retention' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Select XRD card to open XRD inspector
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    await expect(inspector.locator('input[data-pn="region"]')).toBeVisible();

    // 3. Setup dialog handler to capture confirmation message and accept
    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    // 4. Click delete button for parameter region
    const delBtn = inspector.locator('button[data-pd="region"]');
    await expect(delBtn).toBeVisible();
    await delBtn.click();

    // 5. Assert confirmation dialog message informs user of unwiring
    expect(dialogMessage).toBe('Parameter "region" is wired into 1 field. Delete it and unwire all referencing fields?');

    // 6. Assert no 409 Conflict error toast appears
    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();

    // 7. Assert parameter region is removed from inspector and XRD card
    await expect(inspector.locator('input[data-pn="region"]')).toHaveCount(0);
    await expect(xrdCard.locator('.port[data-path="region"]')).toHaveCount(0);

    // 8. Assert main-queue resource is still present on canvas
    const mainQueueCard = page.locator('.node[data-id="main-queue"]');
    await expect(mainQueueCard).toBeVisible();

    // 9. Inspect main-queue and assert region field is unwired
    await mainQueueCard.locator('.node-h').click();
    await expect(inspector).toBeVisible();

    // 10. Assert GET /api/blueprint reflects region is gone and main-queue no longer references region
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const params = (updatedDoc.spec.xrd && updatedDoc.spec.xrd.parameters) || {};
      const resources = updatedDoc.spec.resources || [];
      const mq = resources.find(r => r.name === 'main-queue');
      const mqFields = (mq && mq.fields) || {};
      return {
        hasRegionParam: 'region' in params,
        mqHasRegionWire: 'region' in mqFields && !!(mqFields.region.from || mqFields.region.raw)
      };
    }).toEqual({
      hasRegionParam: false,
      mqHasRegionWire: false
    });
  });

  test('deleting a parameter wired into multiple fields pluralizes prompt and unwires all', async ({ page, request }) => {
    // In pristine doc, region is wired into both work-queue.region and dead-letter.region (fo = 2)
    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('input[data-pn="region"]')).toBeVisible();

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-pd="region"]');
    await delBtn.click();

    expect(dialogMessage).toBe('Parameter "region" is wired into 2 fields. Delete it and unwire all referencing fields?');
    await expect(page.locator('#errtoast')).not.toBeVisible();
    await expect(inspector.locator('input[data-pn="region"]')).toHaveCount(0);

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const params = (updatedDoc.spec.xrd && updatedDoc.spec.xrd.parameters) || {};
      const resources = updatedDoc.spec.resources || [];
      const wq = resources.find(r => r.name === 'work-queue');
      const dl = resources.find(r => r.name === 'dead-letter');
      return {
        hasRegionParam: 'region' in params,
        wqRegion: (wq && wq.fields && wq.fields.region) || null,
        dlRegion: (dl && dl.fields && dl.fields.region) || null
      };
    }).toEqual({
      hasRegionParam: false,
      wqRegion: null,
      dlRegion: null
    });
  });

  test('cancelling confirmation dialog preserves the wired parameter and its wires', async ({ page, request }) => {
    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('input[data-pn="region"]')).toBeVisible();

    page.on('dialog', async (dialog) => {
      await dialog.dismiss();
    });

    const delBtn = inspector.locator('button[data-pd="region"]');
    await delBtn.click();

    // Parameter still exists in inspector
    await expect(inspector.locator('input[data-pn="region"]')).toBeVisible();

    // Document on server unchanged
    const res = await request.get(`${ENGINE}/api/blueprint`);
    const doc = await res.json();
    expect(doc.spec.xrd.parameters.region).toBeDefined();
    expect(doc.spec.resources[0].fields.region.from).toBe('params.region');
  });

  test('deleting an unwired parameter succeeds without confirmation prompt', async ({ page, request }) => {
    // Add an unwired parameter
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.unwiredParam = {
      type: 'string',
      required: false,
      default: 'test'
    };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('input[data-pn="unwiredParam"]')).toBeVisible();

    let dialogPrompted = false;
    page.on('dialog', async (dialog) => {
      dialogPrompted = true;
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-pd="unwiredParam"]');
    await delBtn.click();

    expect(dialogPrompted).toBe(false);
    await expect(page.locator('#errtoast')).not.toBeVisible();
    await expect(inspector.locator('input[data-pn="unwiredParam"]')).toHaveCount(0);

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      return 'unwiredParam' in (updatedDoc.spec.xrd.parameters || {});
    }).toBe(false);
  });
});
