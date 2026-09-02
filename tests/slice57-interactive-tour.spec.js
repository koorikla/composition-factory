const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.describe('Interactive tour', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('the tour walks every station and the prep actions drive the real UI', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    await page.click('#tourBtn');
    const overlay = page.locator('#tour-overlay');
    await expect(overlay).toBeVisible();
    await expect(page.locator('.tour-card')).toContainText(/1 \//);

    // step through the whole tour; the providers step must really activate
    // the SOURCES rail (prep actions drive the actual UI, not a mock)
    let sawSources = false;
    for (let i = 0; i < 24; i++) {
      if (await page.locator('#rtabs button[data-r="src"][aria-pressed="true"]').count()) {
        sawSources = true;
      }
      const next = page.locator('#tour-next');
      if (!(await overlay.isVisible())) break;
      const prevStep = await page.locator('.tour-card').textContent();
      await next.click();
      await expect.poll(async () => {
        if (!(await overlay.isVisible())) return 'done';
        return await page.locator('.tour-card').textContent();
      }).not.toBe(prevStep);
    }
    expect(sawSources).toBe(true);
    await expect(overlay).toBeHidden(); // Done closes it
  });

  test('escape and skip both leave the tour; reopening starts at step 1', async ({ page }) => {
    await page.goto('/');
    await page.click('#tourBtn');
    await expect(page.locator('#tour-overlay')).toBeVisible();
    await page.click('#tour-next');
    await page.keyboard.press('Escape');
    await expect(page.locator('#tour-overlay')).toBeHidden();

    await page.click('#tourBtn');
    await expect(page.locator('.tour-card')).toContainText(/1 \//);
    await page.click('#tour-skip');
    await expect(page.locator('#tour-overlay')).toBeHidden();
  });
});
