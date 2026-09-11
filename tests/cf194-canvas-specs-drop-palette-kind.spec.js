const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, dropKind } = require('./helpers');

test.describe('CF-194 — Canvas specs drop palette kind helper', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('dropping a kind absent from palette fails with helper message naming the kind rather than TypeError', async ({ page }) => {
    await page.goto('/');
    let thrown = null;
    try {
      await dropKind(page, 'NoSuchKind', 'v1', 400, 300, { timeout: 1500 });
    } catch (e) {
      thrown = e;
    }
    expect(thrown).not.toBeNull();
    expect(thrown.name).not.toBe('TypeError');
    expect(thrown.message).not.toMatch(/TypeError/);
    expect(thrown.message).not.toMatch(/Cannot read properties of null/);
    expect(thrown.message).toContain('NoSuchKind');
  });

  test('dropping a valid kind waits for row and canvas and places card', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    const card = page.locator('.node[data-id="deployment"]');
    await expect(card).toBeVisible();
  });

  test('dropping a kind under 20x CPU throttling waits for row and canvas and drops successfully', async ({ page }) => {
    const client = await page.context().newCDPSession(page);
    await client.send('Emulation.setCPUThrottlingRate', { rate: 20 });
    try {
      await page.goto('/');
      await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
      const card = page.locator('.node[data-id="deployment"]');
      await expect(card).toBeVisible();
    } finally {
      await client.send('Emulation.setCPUThrottlingRate', { rate: 1 });
    }
  });
});
