const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, ENGINE } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-475 — Inline new-parameter form in inspector wire dropdown dismisses on submission', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
    // Isolate to single resource so inspector render completes without awaiting sibling schema fetches
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.resources = [doc.spec.resources[0]];
    const res = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(res.ok()).toBe(true);
  });

  test('adding a parameter from a field wire dropdown dismisses the inline form and shows bound chip', async ({ page, request }) => {
    await page.goto('/');

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    const row = page.locator('#insp .fld', { hasText: 'delaySeconds' }).first();
    await expect(row).toBeVisible();
    await row.locator('button[data-m="w"]').click();

    const wireSelect = row.locator('select[data-wire="delaySeconds"]');
    await expect(wireSelect).toBeVisible();

    await wireSelect.selectOption('__new__');

    const nameInput = row.locator('input[data-npname="delaySeconds"]');
    const okBtn = row.locator('button[data-npok="delaySeconds"]');
    await expect(nameInput).toBeVisible();
    await expect(okBtn).toBeVisible();

    await nameInput.fill('queueDelay');
    await okBtn.click();

    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = doc.spec.resources.find(x => x.name === 'work-queue');
      return {
        param: !!(doc.spec.xrd.parameters || {}).queueDelay,
        from: r && r.fields && r.fields.delaySeconds && r.fields.delaySeconds.from,
      };
    }).toEqual({ param: true, from: 'params.queueDelay' });

    // Defect check: inline new-parameter form must dismiss immediately
    await expect(nameInput).not.toBeVisible();
    await expect(okBtn).not.toBeVisible();

    // The field row must show the bound chip
    const boundChip = row.locator('.bound');
    await expect(boundChip).toBeVisible();
    await expect(boundChip.locator('.src')).toHaveText('params.queueDelay');
  });

  test('adding a parameter from an envelope wire dropdown dismisses the inline form and shows bound chip', async ({ page, request }) => {
    await page.goto('/');

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    const row = page.locator('#insp .fld', { hasText: 'writeConnectionSecretToRef.name' }).first();
    await expect(row).toBeVisible();
    await row.locator('button[data-m="w"]').click();

    const wireSelect = row.locator('select[data-env-wire="writeConnectionSecretToRef.name"]');
    await expect(wireSelect).toBeVisible();

    await wireSelect.selectOption('__new__');

    const nameInput = row.locator('input[data-npname="env:writeConnectionSecretToRef.name"]');
    const okBtn = row.locator('button[data-npok="env:writeConnectionSecretToRef.name"]');
    await expect(nameInput).toBeVisible();
    await expect(okBtn).toBeVisible();

    await nameInput.fill('connectionSecretName');
    await okBtn.click();

    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = doc.spec.resources.find(x => x.name === 'work-queue');
      return {
        param: !!(doc.spec.xrd.parameters || {}).connectionSecretName,
        from: r && r.envelope && r.envelope['writeConnectionSecretToRef.name'] && r.envelope['writeConnectionSecretToRef.name'].from,
      };
    }).toEqual({ param: true, from: 'params.connectionSecretName' });

    // Defect check: inline form must dismiss immediately
    await expect(nameInput).not.toBeVisible();
    await expect(okBtn).not.toBeVisible();

    const boundChip = row.locator('.bound');
    await expect(boundChip).toBeVisible();
    await expect(boundChip.locator('.src')).toHaveText('params.connectionSecretName');
  });

  test('pressing Enter in the new parameter name input submits and dismisses the form', async ({ page, request }) => {
    await page.goto('/');

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    const row = page.locator('#insp .fld', { hasText: 'delaySeconds' }).first();
    await expect(row).toBeVisible();
    await row.locator('button[data-m="w"]').click();

    const wireSelect = row.locator('select[data-wire="delaySeconds"]');
    await expect(wireSelect).toBeVisible();
    await wireSelect.selectOption('__new__');

    const nameInput = row.locator('input[data-npname="delaySeconds"]');
    const okBtn = row.locator('button[data-npok="delaySeconds"]');
    await expect(nameInput).toBeVisible();
    await nameInput.fill('enterKeyDelay');
    await nameInput.press('Enter');

    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = doc.spec.resources.find(x => x.name === 'work-queue');
      return {
        param: !!(doc.spec.xrd.parameters || {}).enterKeyDelay,
        from: r && r.fields && r.fields.delaySeconds && r.fields.delaySeconds.from,
      };
    }).toEqual({ param: true, from: 'params.enterKeyDelay' });

    await expect(nameInput).not.toBeVisible();
    await expect(okBtn).not.toBeVisible();
    const boundChip = row.locator('.bound');
    await expect(boundChip).toBeVisible();
    await expect(boundChip.locator('.src')).toHaveText('params.enterKeyDelay');
  });
});
