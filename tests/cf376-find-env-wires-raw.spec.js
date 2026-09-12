const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-376 — findEnvWires ignores raw template references', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('environment key referenced in f.raw shows 1 bound in palette and triggers unwire prompt on delete', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      clusterRegion: {
        type: 'string',
        default: 'us-east-1'
      }
    };
    doc.spec.resources[0].fields.region = {
      raw: 'prefix-${env.clusterRegion}-suffix'
    };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    await page.click('#rtabs button[data-r="shared"]');
    const card = page.locator('.card:has([data-env-del="clusterRegion"])');
    await expect(card.locator('.bind')).toHaveText('1 bound');

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await page.click('[data-env-del="clusterRegion"]');
    expect(dialogMessage).toBe('Environment key "clusterRegion" is wired into 1 field. Delete it and unwire all referencing fields?');

    await expect(page.locator('#region-palette .warnbar')).toHaveCount(0);
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const env = (updated.spec && updated.spec.environment) || {};
      const resList = (updated.spec && updated.spec.resources) || [];
      const r = resList.find(x => x.name === 'work-queue');
      return {
        hasEnv: 'clusterRegion' in env,
        hasField: !!(r && r.fields && 'region' in r.fields)
      };
    }).toEqual({
      hasEnv: false,
      hasField: false
    });
  });
});
