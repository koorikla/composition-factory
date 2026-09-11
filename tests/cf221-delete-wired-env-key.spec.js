const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-221 (#107) — Delete wired environment key unwires referencing fields', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting a wired environment key in inspector prompts, unwires referencing fields, and succeeds without 400', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      envRegion: { type: 'string', default: 'us-east-1' }
    };
    doc.spec.resources[0].fields = doc.spec.resources[0].fields || {};
    doc.spec.resources[0].fields.region = { from: 'env.envRegion' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // Open environment inspector
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    await expect(inspector.locator('[data-env-key="envRegion"]')).toBeVisible();

    // Setup dialog handler to capture confirmation message and accept
    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    // Click delete button for envRegion
    const delBtn = inspector.locator('button[data-env-del-key="envRegion"]');
    await expect(delBtn).toBeVisible();
    await delBtn.click();

    expect(dialogMessage).toBe('Environment key "envRegion" is wired into 1 field. Delete it and unwire all referencing fields?');

    // Assert no 400 error toast appears
    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();

    // Assert envRegion is removed from inspector
    await expect(inspector.locator('[data-env-key="envRegion"]')).toHaveCount(0);

    // Assert GET /api/blueprint reflects envRegion is gone and resource no longer references envRegion
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const env = (updatedDoc.spec && updatedDoc.spec.environment) || {};
      const res0 = (updatedDoc.spec && updatedDoc.spec.resources[0]) || {};
      const res0Fields = (res0 && res0.fields) || {};
      return {
        hasEnvRegion: 'envRegion' in env,
        res0HasRegionWire: 'region' in res0Fields && !!(res0Fields.region.from || res0Fields.region.raw)
      };
    }).toEqual({
      hasEnvRegion: false,
      res0HasRegionWire: false
    });
  });

  test('deleting an environment key wired into multiple resources pluralizes prompt and unwires all', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      sharedZone: { type: 'string', default: 'eu-west-1' }
    };
    doc.spec.resources[0].fields = doc.spec.resources[0].fields || {};
    doc.spec.resources[0].fields.region = { from: 'env.sharedZone' };
    doc.spec.resources[1].fields = doc.spec.resources[1].fields || {};
    doc.spec.resources[1].fields.region = { from: 'env.sharedZone' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('[data-env-key="sharedZone"]')).toBeVisible();

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-env-del-key="sharedZone"]');
    await delBtn.click();

    expect(dialogMessage).toBe('Environment key "sharedZone" is wired into 2 fields. Delete it and unwire all referencing fields?');

    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const env = (updatedDoc.spec && updatedDoc.spec.environment) || {};
      const res0 = (updatedDoc.spec && updatedDoc.spec.resources[0]) || {};
      const res1 = (updatedDoc.spec && updatedDoc.spec.resources[1]) || {};
      return {
        hasSharedZone: 'sharedZone' in env,
        res0HasWire: !!(res0.fields && res0.fields.region),
        res1HasWire: !!(res1.fields && res1.fields.region)
      };
    }).toEqual({
      hasSharedZone: false,
      res0HasWire: false,
      res1HasWire: false
    });
  });

  test('cancelling confirmation dialog in inspector preserves wired environment key and its wires', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      clusterEnv: { type: 'string', default: 'prod' }
    };
    doc.spec.resources[0].fields = doc.spec.resources[0].fields || {};
    doc.spec.resources[0].fields.region = { from: 'env.clusterEnv' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('[data-env-key="clusterEnv"]')).toBeVisible();

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    const delBtn = inspector.locator('button[data-env-del-key="clusterEnv"]');
    await delBtn.click();

    expect(dialogMessage).toBe('Environment key "clusterEnv" is wired into 1 field. Delete it and unwire all referencing fields?');

    // Key still visible in inspector
    await expect(inspector.locator('[data-env-key="clusterEnv"]')).toBeVisible();

    // Key still exists on server with wire intact
    const res = await request.get(`${ENGINE}/api/blueprint`);
    const updatedDoc = await res.json();
    expect(updatedDoc.spec.environment.clusterEnv).toBeDefined();
    expect(updatedDoc.spec.resources[0].fields.region.from).toBe('env.clusterEnv');
  });

  test('deleting an unwired environment key in inspector succeeds without confirmation prompt', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      unwiredKey: { type: 'string', default: 'test' }
    };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('[data-env-key="unwiredKey"]')).toBeVisible();

    let dialogFired = false;
    page.on('dialog', async (dialog) => {
      dialogFired = true;
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-env-del-key="unwiredKey"]');
    await delBtn.click();

    expect(dialogFired).toBe(false);

    await expect(inspector.locator('[data-env-key="unwiredKey"]')).toHaveCount(0);
    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();
  });
});
