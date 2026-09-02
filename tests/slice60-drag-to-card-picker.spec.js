const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.describe('Drag to object popup field picker', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('dragging a parameter onto a resource card opens popup showing spec fields, envelope, and annotations', async ({ page, request }) => {
    page.on('response', async res => {
      if (res.url().includes('/api/blueprint') && res.request().method() === 'PUT') {
        console.log('PUT /api/blueprint status:', res.status(), await res.text())
      }
    })
    page.on('dialog', async d => {
      console.log('DIALOG:', d.type(), d.message())
      await d.accept()
    })
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    // Grab the $region parameter port dot
    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await paramPort.boundingBox();
    expect(paramBox).toBeTruthy();

    // Target the dead-letter queue card header (an empty card area)
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await card.boundingBox();
    expect(cardBox).toBeTruthy();

    // Drag from parameter to card
    await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 });
    await page.mouse.up();

    // Popup opens
    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible();
    await expect(picker.locator('.wire-picker-h')).toContainText('Wire $region → dead-letter');

    // Shows category headers and spec fields
    await expect(picker.locator('.wire-picker-cat')).toContainText(['Spec Fields']);
    
    // Check multiple field options exist (e.g. maxMessageSize, delaySeconds, etc.)
    const items = picker.locator('.wire-picker-item');
    const count = await items.count();
    expect(count).toBeGreaterThan(5);

    // Test search filtering: type "max"
    const search = page.locator('#wire-picker-search');
    await search.fill('max');
    await expect(items.first()).toContainText('maxMessageSize');

    // Click maxMessageSize
    await items.first().click();
    await expect(picker).toBeHidden();

    // Verify wire was committed to doc
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const dlq = doc.spec.resources.find(r => r.name === 'dead-letter');
      return dlq?.fields?.maxMessageSize?.from;
    }).toBe('params.region');
  });

  test('typing in search offers custom annotation and custom field path', async ({ page }) => {
    await page.goto('/');

    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await paramPort.boundingBox();
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await card.boundingBox();

    await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible();

    const search = page.locator('#wire-picker-search');
    await search.fill('my-custom-annotation');

    const annItem = picker.locator('.wire-picker-item[data-idx="0"]');
    await expect(annItem).toContainText('annotations.my-custom-annotation');

    // Press Escape to dismiss
    await page.keyboard.press('Escape');
    await expect(picker).toBeHidden();
  });
});
