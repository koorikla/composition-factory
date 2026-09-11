const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.describe('CF-057: Generate destination tooltip and overwrite confirmation', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });
  test('Generate button tooltip names destination directory and warns of overwrite', async ({ page, request }) => {
    // 1. Fetch /api/version to get the expected outDir
    const verRes = await (await request.get('/api/version')).json();
    expect(verRes.outDir).toBeTruthy();

    await page.goto('/');

    const genBtn = page.locator('#generateBtn');
    await expect(genBtn).toBeVisible();

    // 2. Button title must include the destination path and overwrite warning
    const expectedTitle = `Write generated manifests to ${verRes.outDir} (overwrites existing files)`;
    await expect(genBtn).toHaveAttribute('title', expectedTitle);
  });

  test('Clicking Generate prompts confirmation before writing; dismissing cancels write', async ({ page, request }) => {
    const verRes = await (await request.get('/api/version')).json();
    const outDir = verRes.outDir;

    await page.goto('/');

    const banner = page.locator('#next-steps-banner');
    await expect(banner).toBeVisible({ timeout: 10000 });
    await expect(banner).toContainText('Preview only');

    let dialogAppeared = false;
    let dialogMessage = '';

    page.on('dialog', async (dialog) => {
      dialogAppeared = true;
      dialogMessage = dialog.message();
      // Dismiss the dialog (Cancel)
      await dialog.dismiss();
    });

    const genBtn = page.locator('#generateBtn');
    await expect(genBtn).toBeVisible();
    await genBtn.click();

    // Verify confirmation prompt was shown with expected message
    expect(dialogAppeared).toBe(true);
    expect([
      `Generate will write manifests to disk in '${outDir}'.\n\nProceed?`,
      `Generate will write manifests to disk in '${outDir}', overwriting existing files.\n\nProceed?`
    ]).toContain(dialogMessage);

    // Because it was dismissed, banner should NOT transition to "Output written to"
    await expect(banner).toContainText('Preview only');
    await expect(banner).not.toContainText('Output written to');
  });

  test('Clicking Generate and accepting confirmation writes to disk', async ({ page, request }) => {
    const verRes = await (await request.get('/api/version')).json();
    const outDir = verRes.outDir;

    await page.goto('/');

    const banner = page.locator('#next-steps-banner');
    await expect(banner).toBeVisible({ timeout: 10000 });
    await expect(banner).toContainText('Preview only');

    let dialogAppeared = false;
    let dialogMessage = '';

    page.once('dialog', async (dialog) => {
      dialogAppeared = true;
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    const genBtn = page.locator('#generateBtn');
    await expect(genBtn).toBeVisible();
    await genBtn.click();

    expect(dialogAppeared).toBe(true);
    expect([
      `Generate will write manifests to disk in '${outDir}'.\n\nProceed?`,
      `Generate will write manifests to disk in '${outDir}', overwriting existing files.\n\nProceed?`
    ]).toContain(dialogMessage);

    // Because it was accepted, banner transitions to "Output written to"
    await expect(banner).toContainText('Output written to');

    // Subsequent generate on now-populated directory warns about overwriting existing files
    let overwriteWarnSeen = false;
    page.once('dialog', async (dialog) => {
      if (dialog.message().includes('overwriting existing files')) {
        overwriteWarnSeen = true;
      }
      await dialog.dismiss();
    });
    await genBtn.click();
    expect(overwriteWarnSeen).toBe(true);
  });
});
