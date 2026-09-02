const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

function scrollTop(page) {
  return page.evaluate(() => document.getElementById('code').scrollTop);
}

test.describe('Output scroll follows selection', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('clicking a card scrolls the composition output to that resource', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    // wait for the generated composition to land in the drawer
    await expect(page.locator('#code')).toContainText('setResourceNameAnnotation', { timeout: 30000 });

    expect(await scrollTop(page)).toBe(0);

    // selecting a card brings its setResourceNameAnnotation line into view
    const anchorVisible = (name) => page.evaluate((n) => {
      const code = document.getElementById('code');
      const idx = code.textContent.split('\n')
        .findIndex(l => l.includes('setResourceNameAnnotation "' + n + '"'));
      const lh = parseFloat(getComputedStyle(code).lineHeight) || 16;
      const y = idx * lh;
      return idx >= 0 && y >= code.scrollTop && y <= code.scrollTop + code.clientHeight;
    }, name);

    await page.click('.node[data-id="dead-letter"] .node-h');
    await expect.poll(() => scrollTop(page)).toBeGreaterThan(0);
    await expect.poll(() => anchorVisible('dead-letter')).toBe(true); // smooth scroll settles

    await page.click('.node[data-id="work-queue"] .node-h');
    await expect.poll(() => anchorVisible('work-queue')).toBe(true);
  });

  test('the blueprint tab follows selection too', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await page.click('#tabs button[data-t="bp"]');
    await expect(page.locator('#code')).toContainText('dead-letter');

    await page.click('.node[data-id="dead-letter"] .node-h');
    await expect.poll(() => scrollTop(page)).toBeGreaterThan(0);
  });
});
