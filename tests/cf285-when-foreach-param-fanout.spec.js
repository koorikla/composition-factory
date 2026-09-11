const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-285 — fanOut ignores parameters referenced in when or forEach guards, failing deletion with HTTP 409', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('parameter wired into resource when guard reflects fan-out count >= 1 and prompts unwire on delete instead of 409 abort', async ({ page, request }) => {
    // 1. Seed doc with parameter wired into when condition
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.enableQueue = {
      type: 'boolean',
      required: true,
      description: 'Conditionally enable work-queue'
    };
    doc.spec.resources[0].when = 'params.enableQueue';

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Inspect XRD: fan-out badge must reflect 1 wire, not 0
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const paramRow = page.locator('#region-inspector .fld:has(input[data-pn="enableQueue"])');
    await expect(paramRow).toBeVisible();
    const fanBadge = paramRow.locator('.fan');
    await expect(fanBadge).toHaveText('×1');

    // 3. Deleting enableQueue must prompt unwire confirmation
    let dialogMessage = '';
    page.on('dialog', async (d) => {
      dialogMessage = d.message();
      await d.accept();
    });

    await paramRow.locator('button[data-pd="enableQueue"]').click();
    await page.waitForTimeout(500);

    expect(dialogMessage).toBe('Parameter "enableQueue" is wired into 1 field. Delete it and unwire all referencing fields?');

    // 4. Parameter must be deleted and resource when condition unwired
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const p = updated.spec.xrd.parameters || {};
      const r = updated.spec.resources.find(res => res.name === 'work-queue');
      return {
        hasParam: 'enableQueue' in p,
        whenCondition: r ? r.when : undefined
      };
    }).toEqual({
      hasParam: false,
      whenCondition: undefined
    });
  });

  test('parameter wired into resource forEach loop reflects fan-out count in palette and unwires on delete', async ({ page, request }) => {
    // 1. Seed doc with integer parameter wired into forEach
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.replicas = {
      type: 'integer',
      required: false,
      default: '3'
    };
    doc.spec.resources[0].forEach = 'params.replicas';

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Check Shared rail in palette: bound badge must reflect 1 bound
    await page.click('#rtabs button[data-r="shared"]');
    const paramCard = page.locator('.card:has([data-param-del="replicas"])');
    await expect(paramCard).toBeVisible();
    await expect(paramCard.locator('.bind')).toHaveText('1 bound');

    // 3. Delete from palette rail must prompt unwire confirmation and succeed without warnbar
    let dialogMessage = '';
    page.on('dialog', async (d) => {
      dialogMessage = d.message();
      await d.accept();
    });

    await paramCard.locator('[data-param-del="replicas"]').click();
    await page.waitForTimeout(500);

    expect(dialogMessage).toBe('Parameter "replicas" is wired into 1 field. Delete it and unwire all referencing fields?');
    await expect(page.locator('#region-palette .warnbar')).toHaveCount(0);

    // 4. Parameter deleted and forEach loop unwired
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const p = updated.spec.xrd.parameters || {};
      const r = updated.spec.resources.find(res => res.name === 'work-queue');
      return {
        hasParam: 'replicas' in p,
        forEachLoop: r ? r.forEach : undefined
      };
    }).toEqual({
      hasParam: false,
      forEachLoop: undefined
    });
  });
});
