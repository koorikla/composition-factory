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
