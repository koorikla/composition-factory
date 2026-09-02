const { test, expect } = require('@playwright/test')
const fs = require('fs')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.describe('package.yaml in and out', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('the package.yaml tab shows the Configuration stream', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await page.click('#tabs button[data-t="pkg"]');
    await expect(page.locator('#code')).toContainText('kind: Configuration');
    await expect(page.locator('#code')).toContainText('factory.crossplane.io/blueprint');
    await expect(page.locator('#code')).toContainText('dependsOn');
  });

  test('an exported package.yaml imports back through the Import button', async ({ page, request }) => {
    // export the pristine doc's package.yaml
    const res = await request.get(ENGINE + '/api/package?format=yaml');
    expect(res.ok()).toBeTruthy();
    fs.writeFileSync('.testrun/export.package.yaml', await res.text());

    // move the doc to a different blueprint entirely (the YAML door is the
    // import endpoint; PUT takes the JSON document)
    const other = fs.readFileSync('testdata/xqueue.cf.yaml', 'utf8');
    const moved = await request.post(ENGINE + '/api/blueprint/import', {
      headers: { 'Content-Type': 'application/yaml' },
      data: other,
    });
    expect(moved.ok()).toBeTruthy();

    await page.goto('/');
    await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible();

    // importing the package recovers the embedded blueprint
    const [chooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.click('#importBtn'),
    ]);
    await chooser.setFiles('.testrun/export.package.yaml');
    await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible();
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(doc.metadata.name).toBe('xnotify');
  });
});
