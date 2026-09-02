const { test, expect } = require('@playwright/test')
const fs = require('fs')
const { resetDoc, ENGINE } = require('./helpers')

test('the Package button downloads <name>.xpkg', async ({ page, request }) => {
  await resetDoc(request);
  await page.goto('/');
  await expect(page.locator('.node')).toHaveCount(3);

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('#packageBtn'),
  ]);
  expect(download.suggestedFilename()).toBe('xnotify.xpkg');
  const path = await download.path();
  expect(fs.statSync(path).size).toBeGreaterThan(0);
});
