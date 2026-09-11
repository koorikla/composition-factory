const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled, settledBox } = require('./helpers');

test.describe('CF-159: Canvas M1 polish suite', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  // Moment 1: Zoom reset centers cards that were placed out of viewport
  test('zoom reset centers cards that were placed out of viewport', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const cw = page.locator('#cw');
    await expect(cw).toBeVisible();
    const cwBox = await settledBox(cw);

    const node = page.locator('.node[data-id="work-queue"]');
    await expect(node).toBeVisible();

    // Place card far to the right and bottom, way outside the visible canvas viewport
    await node.evaluate((el) => {
      el.style.left = '1800px';
      el.style.top = '1400px';
    });
    await canvasSettled(page);

    // Verify card is currently placed far outside the canvas viewport before reset
    const boxBefore = await settledBox(node);
    expect(boxBefore.x).toBeGreaterThan(cwBox.x + cwBox.width);

    // Click #zoom-reset (⌂)
    await page.click('#zoom-reset');
    await canvasSettled(page);

    // After reset, all cards must be centered and fully visible within canvas viewport
    const allNodes = page.locator('.node');
    const count = await allNodes.count();
    expect(count).toBeGreaterThan(0);

    const updatedCwBox = await settledBox(cw);
    for (let i = 0; i < count; i++) {
      const card = allNodes.nth(i);
      const box = await settledBox(card);
      expect(box.x).toBeGreaterThanOrEqual(updatedCwBox.x - 2);
      expect(box.y).toBeGreaterThanOrEqual(updatedCwBox.y - 2);
      expect(box.x + box.width).toBeLessThanOrEqual(updatedCwBox.x + updatedCwBox.width + 2);
      expect(box.y + box.height).toBeLessThanOrEqual(updatedCwBox.y + updatedCwBox.height + 2);
    }
  });

  // Moment 2: Generate with empty output directory omits overwrite warning
  test('generate with empty output directory omits overwrite warning', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    let dialogAppeared = false;
    let dialogMessage = '';

    page.on('dialog', async (dialog) => {
      dialogAppeared = true;
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    const genBtn = page.locator('#generateBtn');
    await expect(genBtn).toBeVisible();
    await genBtn.click();

    expect(dialogAppeared).toBe(true);
    // Dialog must prompt for generation but must omit "overwriting existing files" on an empty out dir
    expect(dialogMessage).toContain('Generate will write manifests to disk');
    expect(dialogMessage).not.toContain('overwriting existing files');
  });

  // Moment 3: Kind drag-and-drop clears tooltip immediately
  test('kind drag-and-drop clears tooltip immediately', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Hover over a kind in the palette to trigger the tooltip
    const kindRow = page.locator('#lrail .kind[data-kind="QueuePolicy"]').first();
    await expect(kindRow).toBeVisible();
    await kindRow.hover();

    const tooltip = page.locator('#kind-preview');
    await expect(tooltip).toBeVisible({ timeout: 5000 });

    // Drop onto canvas while tooltip is displayed
    const cw = page.locator('#cw');
    await cw.dispatchEvent('drop', {
      dataTransfer: await page.evaluateHandle(() => new DataTransfer())
    });

    // Verify tooltip popover is dismissed immediately after drop
    await expect(tooltip).not.toBeVisible();
  });

  // Moment 4: Inspector handles long field path without horizontal scroll container expansion
  test('inspector handles long field path without horizontal scroll container expansion', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Select work-queue card and switch to "All" fields to view deep/long paths
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const inspPane = page.locator('#insp');
    await expect(inspPane).toBeVisible();

    // Check that long field paths use ellipsis truncation and native title tooltip
    const fieldNames = page.locator('#insp .fld-h .n');
    await expect(fieldNames.first()).toBeVisible({ timeout: 10000 });
    const count = await fieldNames.count();
    expect(count).toBeGreaterThan(0);

    const firstField = fieldNames.first();
    const textOverflow = await firstField.evaluate((el) => getComputedStyle(el).textOverflow);
    const whiteSpace = await firstField.evaluate((el) => getComputedStyle(el).whiteSpace);
    const overflow = await firstField.evaluate((el) => getComputedStyle(el).overflow);

    expect(textOverflow).toBe('ellipsis');
    expect(whiteSpace).toBe('nowrap');
    expect(overflow).toBe('hidden');

    // Title attribute must be present and non-empty for native tooltip
    const titleAttr = await firstField.getAttribute('title');
    expect(titleAttr).toBeTruthy();

    // Inspector container must not expand horizontally or scroll horizontally
    const scrollInfo = await inspPane.evaluate((el) => ({
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth,
    }));
    expect(scrollInfo.scrollWidth).toBeLessThanOrEqual(scrollInfo.clientWidth + 1);
  });
});
