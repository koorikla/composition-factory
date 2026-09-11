const { test, expect } = require('@playwright/test');
const fs = require('fs');
const path = require('path');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-235: Stacking and offset for concurrent toasts and import notices', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  function doBoxesOverlap(boxA, boxB) {
    return (
      boxA.x < boxB.x + boxB.width &&
      boxA.x + boxA.width > boxB.x &&
      boxA.y < boxB.y + boxB.height &&
      boxA.y + boxA.height > boxB.y
    );
  }

  test('importing a composition with dropped items lays out loss bar and Adopted toast without occlusion', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../testdata/xqueue-pipeline.composition.golden.yaml');
    const compYaml = fs.readFileSync(yamlPath, 'utf8');

    await page.goto('/');

    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    await page.setInputFiles('#importFile', {
      name: 'xqueue-pipeline.composition.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(compYaml),
    });

    const warnBar = page.locator('#import-warn');
    const toast = page.locator('#import-toast');

    await expect(warnBar).toBeVisible({ timeout: 10000 });
    await expect(toast).toBeVisible({ timeout: 10000 });

    const warnBox = await warnBar.boundingBox();
    const toastBox = await toast.boundingBox();

    expect(warnBox).not.toBeNull();
    expect(toastBox).not.toBeNull();

    // Verify the toast does NOT overlap or occlude the loss bar
    const overlaps = doBoxesOverlap(warnBox, toastBox);
    expect(overlaps).toBe(false);

    // Toast must be vertically positioned below the loss bar
    expect(toastBox.y).toBeGreaterThanOrEqual(warnBox.y + warnBox.height);
  });

  test('importing a composition with both drops and schema error shows all 3 notices stacked without occlusion', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../testdata/xqueue-pipeline.composition.golden.yaml');
    let compYaml = fs.readFileSync(yamlPath, 'utf8');

    // Add an unknown field `retentionDays` to Queue spec.forProvider to trigger schema error on generate
    compYaml = compYaml.replace(
      "region: 'eu-north-1'",
      "region: 'eu-north-1'\n              retentionDays: {{ $spec.retentionDays }}"
    );

    await page.goto('/');

    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    await page.setInputFiles('#importFile', {
      name: 'xqueue-pipeline-error.composition.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(compYaml),
    });

    const warnBar = page.locator('#import-warn');
    const importToast = page.locator('#import-toast');
    const errorToast = page.locator('#canvas-error-toast');

    await expect(warnBar).toBeVisible({ timeout: 10000 });
    await expect(importToast).toBeVisible({ timeout: 10000 });

    // In CF-249 unknown wired fields are now pruned during adopt so generate succeeds.
    // Trigger an error to verify concurrent 3-way notice stacking (CF-235).
    await page.evaluate(() => {
      window.store.emit('error', { message: 'schema error: retentionDays' });
    });

    await expect(errorToast).toBeVisible({ timeout: 10000 });

    // Verify content of each notice
    await expect(warnBar).toContainText(/dropped item/i);
    await expect(importToast).toContainText(/Adopted/i);
    await expect(errorToast).toContainText(/retentionDays/i);

    const warnBox = await warnBar.boundingBox();
    const importToastBox = await importToast.boundingBox();
    const errorToastBox = await errorToast.boundingBox();

    expect(warnBox).not.toBeNull();
    expect(importToastBox).not.toBeNull();
    expect(errorToastBox).not.toBeNull();

    // Verify none of the notices overlap each other
    expect(doBoxesOverlap(warnBox, importToastBox)).toBe(false);
    expect(doBoxesOverlap(warnBox, errorToastBox)).toBe(false);
    expect(doBoxesOverlap(errorToastBox, importToastBox)).toBe(false);

    // Error toast must never be hidden behind the loss bar or informational toast.
    // Both toasts must be below the loss bar, and the error toast must be placed above the informational toast.
    expect(errorToastBox.y).toBeGreaterThanOrEqual(warnBox.y + warnBox.height);
    expect(importToastBox.y).toBeGreaterThanOrEqual(errorToastBox.y + errorToastBox.height);

    // Dismissing the loss bar updates the toast stack to sit directly below the topbar
    const dismissWarnBtn = warnBar.locator('.warnbar-dismiss, button[aria-label="Dismiss"]');
    await dismissWarnBtn.click();
    await expect(warnBar).toBeHidden();

    const topbar = page.locator('#region-topbar');
    const topbarBox = await topbar.boundingBox();
    const errorToastBoxAfter = await errorToast.boundingBox();
    const importToastBoxAfter = await importToast.boundingBox();

    // Toasts are still non-overlapping and placed below topbar
    expect(doBoxesOverlap(errorToastBoxAfter, importToastBoxAfter)).toBe(false);
    expect(errorToastBoxAfter.y).toBeGreaterThanOrEqual(topbarBox.y + topbarBox.height);
    expect(importToastBoxAfter.y).toBeGreaterThanOrEqual(errorToastBoxAfter.y + errorToastBoxAfter.height);

    // Dismissing the error toast shifts the adopted toast up
    const dismissErrorBtn = errorToast.locator('.toast-close');
    await dismissErrorBtn.click();
    await expect(errorToast).toHaveCount(0);

    await expect(importToast).toBeVisible();
    const finalImportToastBox = await importToast.boundingBox();
    expect(finalImportToastBox.y).toBeGreaterThanOrEqual(topbarBox.y + topbarBox.height);
  });
});
