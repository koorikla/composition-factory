const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, settledBox } = require('./helpers')
guardPageErrors()

test.describe('Drag to object popup field picker', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('dragging a parameter onto a resource card opens popup showing spec fields, envelope, and annotations', async ({ page, request }) => {
    page.on('dialog', async d => await d.accept());
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await canvasSettled(page);

    // Grab the $region parameter port dot
    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await settledBox(paramPort);

    // Target the dead-letter queue card header (an empty card area)
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await settledBox(card);

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
    await expect(page.locator('.node')).toHaveCount(3);
    await canvasSettled(page);

    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await settledBox(paramPort);
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await settledBox(card);

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

  test('wiring an optional parameter to a required field displays warning badge in picker and prompts to mark required', async ({ page, request }) => {
    // Set region parameter to optional (required: false)
    const curDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    curDoc.spec.xrd.parameters.region.required = false;
    await request.put(ENGINE + '/api/blueprint', { data: curDoc });

    let dialogMessage = null;
    page.on('dialog', async dialog => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await canvasSettled(page);

    // Grab the $region parameter port dot
    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await settledBox(paramPort);

    // Target the dead-letter queue card header
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await settledBox(card);

    // Drag from parameter to card
    await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible();

    // Filter to "region"
    const search = page.locator('#wire-picker-search');
    await search.fill('region');

    const regionItem = picker.locator('.wire-picker-item', { hasText: 'region' }).first();
    await expect(regionItem).toBeVisible();

    // Verify warning badge in picker
    const warnBadge = regionItem.locator('.wire-picker-opt-warning');
    await expect(warnBadge).toBeVisible();
    await expect(warnBadge).toContainText('optional → req');

    // Click region field item to wire
    await regionItem.click();
    await expect(picker).toBeHidden();

    // Verify confirmation prompt was triggered
    expect(dialogMessage).toContain("Parameter '$region' is optional, but 'region' is required");
    expect(dialogMessage).toContain("Mark parameter as required to guarantee presence in render?");

    // Verify parameter was upgraded to required: true
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.xrd.parameters.region.required;
    }).toBe(true);
  });

  test('inspector and canvas port show warning indicator when optional parameter is wired to required field', async ({ page, request }) => {
    // Set region parameter to optional (required: false)
    const curDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    curDoc.spec.xrd.parameters.region.required = false;
    // dead-letter already has region wired to params.region in pristine doc
    await request.put(ENGINE + '/api/blueprint', { data: curDoc });

    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await canvasSettled(page);

    // Check canvas card port row for dead-letter region port
    const dlRegionPort = page.locator('.node[data-id="dead-letter"] .port[data-path="region"]');
    await expect(dlRegionPort).toHaveClass(/port-opt-warn/);
    await expect(dlRegionPort.locator('.port-warn')).toBeVisible();

    // Open inspector for dead-letter
    await page.locator('.node[data-id="dead-letter"]').click();
    const inspector = page.locator('#insp');
    await expect(inspector).toBeVisible();

    // Inspector shows warning under the bound wire
    const wireWarn = inspector.locator('.wire-warn');
    await expect(wireWarn).toBeVisible();
    await expect(wireWarn).toContainText('optional param into required field');
  });
});
