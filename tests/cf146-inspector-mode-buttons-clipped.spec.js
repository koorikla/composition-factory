// tests/cf146-inspector-mode-buttons-clipped.spec.js
// CF-146 (issue #31) — Val/Wire/Raw buttons on indented inspector rows are clipped at the panel's right edge
//
// Contract:
// The mode buttons stay inside the panel at every nesting depth (wrap or shrink the label, not the controls).

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-146 — Val/Wire/Raw buttons inside inspector panel', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('mode buttons stay inside inspector panel on a 1440-wide page for envelope and nested ref rows', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/');

    // Select work-queue resource card
    await page.click('.node[data-id="work-queue"] .node-h');

    // Switch to All fields view so envelope and nested fields are visible
    await page.click('#fseg button[data-f="all"]');

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    const inspectorBox = await inspector.boundingBox();
    expect(inspectorBox).not.toBeNull();
    const panelRightEdge = inspectorBox.x + inspectorBox.width;

    // Check CROSSPLANE ENVELOPE row: writeConnectionSecretToRef.name
    const envRow = page.locator('#insp .fld', { hasText: 'writeConnectionSecretToRef.name' }).first();
    await expect(envRow).toBeVisible();
    await envRow.scrollIntoViewIfNeeded();

    const envModes = envRow.locator('.modes');
    await expect(envModes).toBeVisible();
    const envModesBox = await envModes.boundingBox();
    expect(envModesBox).not.toBeNull();

    // Mode buttons container and all its buttons must stay inside the panel
    expect(envModesBox.x + envModesBox.width).toBeLessThanOrEqual(panelRightEdge);

    const envRawBtn = envModes.locator('button[data-m="r"]');
    await expect(envRawBtn).toBeVisible();
    const envRawBox = await envRawBtn.boundingBox();
    expect(envRawBox).not.toBeNull();
    expect(envRawBox.x + envRawBox.width).toBeLessThanOrEqual(panelRightEdge);
    // Controls must not shrink below their minimum width (24px)
    expect(envRawBox.width).toBeGreaterThanOrEqual(24);

    // Check every visible mode button group in the inspector
    const allModes = page.locator('#insp .fld .modes');
    const count = await allModes.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const modeGroup = allModes.nth(i);
      if (!(await modeGroup.isVisible())) continue;

      const mBox = await modeGroup.boundingBox();
      if (!mBox) continue;

      const rawBtn = modeGroup.locator('button[data-m="r"]');
      const rawBox = await rawBtn.boundingBox();
      expect(rawBox).not.toBeNull();

      // Contract: mode buttons stay inside the panel at every nesting depth
      expect(mBox.x + mBox.width).toBeLessThanOrEqual(panelRightEdge);
      expect(rawBox.x + rawBox.width).toBeLessThanOrEqual(panelRightEdge);
      // Contract: do not shrink the controls
      expect(rawBox.width).toBeGreaterThanOrEqual(24);
    }
  });

  test('mode buttons stay inside inspector panel at narrow viewport 1280x720 and when panel is resized towards min-width', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.goto('/');

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    const handle = page.locator('#col-resize-r');
    await expect(handle).toBeVisible();
    const hb = await handle.boundingBox();

    // Drag inspector column left to narrow it towards min-width (260px)
    await page.mouse.move(hb.x + hb.width / 2, hb.y + 200);
    await page.mouse.down();
    await page.mouse.move(hb.x + hb.width / 2 + 50, hb.y + 200, { steps: 5 });
    await page.mouse.up();

    const inspector = page.locator('#region-inspector');
    const inspectorBox = await inspector.boundingBox();
    const panelRightEdge = inspectorBox.x + inspectorBox.width;

    const allModes = page.locator('#insp .fld .modes');
    const count = await allModes.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const modeGroup = allModes.nth(i);
      if (!(await modeGroup.isVisible())) continue;

      const mBox = await modeGroup.boundingBox();
      if (!mBox) continue;

      const rawBtn = modeGroup.locator('button[data-m="r"]');
      const rawBox = await rawBtn.boundingBox();
      expect(rawBox).not.toBeNull();

      expect(mBox.x + mBox.width).toBeLessThanOrEqual(panelRightEdge);
      expect(rawBox.x + rawBox.width).toBeLessThanOrEqual(panelRightEdge);
      expect(rawBox.width).toBeGreaterThanOrEqual(24);
    }
  });

  test('map entry mode buttons stay inside panel even with long key name', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto('/');

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Find tags map row
    const tagsRow = page.locator('#insp .fld', { hasText: 'tags' }).first();
    await expect(tagsRow).toBeVisible();
    await tagsRow.scrollIntoViewIfNeeded();

    // Click + Add key
    const addKeyBtn = tagsRow.locator('button[data-add-map-entry="tags"]');
    await expect(addKeyBtn).toBeVisible();
    await addKeyBtn.click();

    // Fill long key name
    const keyInput = tagsRow.locator('input[data-new-map-key="tags"]');
    await expect(keyInput).toBeVisible();
    await keyInput.fill('veryLongDepartmentAndTeamOwnershipTagKey');

    const valInput = tagsRow.locator('input[data-new-map-val="tags"]');
    await valInput.fill('platform-infra');

    const okBtn = tagsRow.locator('button[data-new-map-ok="tags"]');
    await okBtn.click();

    const inspector = page.locator('#region-inspector');
    const inspectorBox = await inspector.boundingBox();
    const panelRightEdge = inspectorBox.x + inspectorBox.width;

    // Verify map entry card was created and mode buttons are within panel
    const entryCard = tagsRow.locator('.map-entry-card').first();
    await expect(entryCard).toBeVisible();
    const modeGroup = entryCard.locator('.modes');
    await expect(modeGroup).toBeVisible();

    const mBox = await modeGroup.boundingBox();
    expect(mBox).not.toBeNull();
    expect(mBox.x + mBox.width).toBeLessThanOrEqual(panelRightEdge);

    const rawBtn = modeGroup.locator('button[data-m="r"]');
    const rawBox = await rawBtn.boundingBox();
    expect(rawBox).not.toBeNull();
    expect(rawBox.x + rawBox.width).toBeLessThanOrEqual(panelRightEdge);
    expect(rawBox.width).toBeGreaterThanOrEqual(24);
  });
});
