import { test, expect } from '@playwright/test';
import * as fs from 'node:fs';

const ENGINE = 'http://127.0.0.1:8081';
const pristine = fs.readFileSync('tests/fixtures/pristine-doc.yaml', 'utf8');

async function resetDoc(request) {
  const res = await request.put(ENGINE + '/api/blueprint', {
    headers: { 'Content-Type': 'application/yaml' },
    data: pristine,
  });
  expect(res.ok()).toBeTruthy();
}

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
      await next.click();
      await page.waitForTimeout(120);
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
