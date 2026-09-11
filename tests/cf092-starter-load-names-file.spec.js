const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-092 — Starter example load names served blueprint file', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('example cards explicitly name the served blueprint file in the replacement note', async ({ page }) => {
    await page.goto('/');

    const exBtn = page.locator('#examplesBtn');
    await expect(exBtn).toBeVisible();
    await exBtn.click();

    const overlay = page.locator('#examplesOverlay');
    await expect(overlay).toBeVisible();

    // Wait for example cards to render
    const card = page.locator('.example-card[data-id="irsa"]');
    await expect(card).toBeVisible({ timeout: 10000 });

    const note = card.locator('.example-note');
    await expect(note).toBeVisible();

    // In the test runner environment, the served blueprint filename is doc.cf.yaml
    // The note must not be generic "(replaces current blueprint · undoable)".
    // It must name the file being replaced: "(replaces doc.cf.yaml · undoable)"
    await expect(note).toContainText('doc.cf.yaml');
    await expect(note).toContainText('(replaces doc.cf.yaml · undoable)');
  });
});
