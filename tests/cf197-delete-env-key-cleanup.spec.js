const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-197 — Deleting an environment key cleans up environmentConfigs and refuses if wired', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting the last environment key in the inspector removes spec.environmentConfigs without 400 error', async ({ page, request }) => {
    // 1. Seed blueprint with single environment key and an environmentConfigs entry
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      vpcId: { type: 'string' }
    };
    docWithEnv.spec.environmentConfigs = [
      {
        name: 'default',
        data: {
          vpcId: 'vpc-12345'
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Click EnvironmentConfig card on canvas to select it
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // 3. Click the delete button next to vpcId
    const delBtn = inspector.locator('button[data-env-del-key="vpcId"]');
    await expect(delBtn).toBeVisible();
    await delBtn.click();

    // 4. Verify no error toast appears
    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();

    // 5. Verify server persisted doc has no environment and no dangling environmentConfigs
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await res.json();
      return {
        hasEnv: !!(doc.spec.environment && Object.keys(doc.spec.environment).length > 0),
        hasConfigs: !!(doc.spec.environmentConfigs && doc.spec.environmentConfigs.length > 0)
      };
    }).toEqual({ hasEnv: false, hasConfigs: false });

    // 6. EnvironmentConfig card is removed from canvas
    await expect(page.locator('.node[data-id="environment"]')).toHaveCount(0);
  });

  test('deleting an environment key when multiple remain removes the key from environmentConfigs data', async ({ page, request }) => {
    // 1. Seed blueprint with two keys and environmentConfigs data
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      vpcId: { type: 'string' },
      region: { type: 'string' }
    };
    docWithEnv.spec.environmentConfigs = [
      {
        name: 'default',
        data: {
          vpcId: 'vpc-12345',
          region: 'us-east-1'
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // Select EnvironmentConfig card
    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('[data-env-key="vpcId"]')).toBeVisible();
    await expect(inspector.locator('[data-env-key="region"]')).toBeVisible();

    // Delete vpcId
    await inspector.locator('button[data-env-del-key="vpcId"]').click();

    // Verify no error toast
    await expect(page.locator('#errtoast')).not.toBeVisible();

    // Verify blueprint doc on server: vpcId is gone from environment and environmentConfigs, region remains
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await res.json();
      const envKeys = Object.keys(doc.spec.environment || {});
      const cfgData = (doc.spec.environmentConfigs && doc.spec.environmentConfigs[0] && doc.spec.environmentConfigs[0].data) || {};
      return {
        envKeys,
        cfgData
      };
    }).toEqual({
      envKeys: ['region'],
      cfgData: { region: 'us-east-1' }
    });
  });

  test('deleting an environment key via the SHARED palette cleans up environmentConfigs', async ({ page, request }) => {
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      vpcId: { type: 'string', default: 'vpc-12345' }
    };
    docWithEnv.spec.environmentConfigs = [
      {
        name: 'default',
        data: {
          vpcId: 'vpc-12345'
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await page.click('#rtabs button[data-r="shared"]');

    page.on('dialog', d => d.accept());
    await page.click('[data-env-del="vpcId"]');

    // Verify no error alert in palette
    const alert = page.locator('#region-palette [role="alert"]');
    await expect(alert).toHaveCount(0);

    // Verify doc on server has cleaned up environment and environmentConfigs
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await res.json();
      return {
        hasEnv: !!(doc.spec.environment && Object.keys(doc.spec.environment).length > 0),
        hasConfigs: !!(doc.spec.environmentConfigs && doc.spec.environmentConfigs.length > 0)
      };
    }).toEqual({ hasEnv: false, hasConfigs: false });
  });

  test('cancelling confirmation dialog in inspector preserves wired environment key and its wires', async ({ page, request }) => {
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      sharedRegion: { type: 'string', default: 'us-east-1' }
    };
    docWithEnv.spec.resources[0].fields = docWithEnv.spec.resources[0].fields || {};
    docWithEnv.spec.resources[0].fields.region = { from: 'env.sharedRegion' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('[data-env-key="sharedRegion"]')).toBeVisible();

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    // Attempt to delete wired key
    await inspector.locator('button[data-env-del-key="sharedRegion"]').click();

    expect(dialogMessage).toBe('Environment key "sharedRegion" is wired into 1 field. Delete it and unwire all referencing fields?');

    // Key still visible in inspector
    await expect(inspector.locator('[data-env-key="sharedRegion"]')).toBeVisible();

    // Key still exists on server with wire intact
    const res = await request.get(`${ENGINE}/api/blueprint`);
    const doc = await res.json();
    expect(doc.spec.environment.sharedRegion).toBeDefined();
    expect(doc.spec.resources[0].fields.region.from).toBe('env.sharedRegion');
  });
});
