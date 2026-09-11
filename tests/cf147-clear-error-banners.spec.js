const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-147 — Error banners and toasts clear on next action or doc change', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('SOURCES refusal banner clears on search input, tab switch, and document change', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Switch to SOURCES rail
    await page.click('#rtabs button[data-r="src"]');
    const lrail = page.locator('#lrail');
    await expect(lrail).toBeVisible();

    // SQS provider is referenced by "work-queue" and "dead-letter" resources in pristine doc.
    const sqsRow = lrail.locator('.src-row', { hasText: 'provider-aws-sqs' }).first();
    await expect(sqsRow).toBeVisible({ timeout: 10000 });

    const rowRemove = sqsRow.locator('.src-row-remove, button[data-remove-ref]');
    await expect(rowRemove).toBeVisible();

    page.on('dialog', d => d.accept());
    await rowRemove.click();

    // Refusal banner should appear in SOURCES
    const warnbar = lrail.locator('.warnbar[role="alert"]');
    await expect(warnbar).toBeVisible({ timeout: 5000 });
    await expect(warnbar).toContainText(/still referenced/i);

    // Typing into the catalogue search input (#cat-search) must clear the refusal banner
    const catSearch = lrail.locator('#cat-search');
    await expect(catSearch).toBeVisible();
    await catSearch.fill('rds');
    await expect(warnbar).toHaveCount(0);

    // Re-trigger the refusal banner
    await rowRemove.click();
    await expect(warnbar).toBeVisible({ timeout: 5000 });
    await expect(warnbar).toContainText(/still referenced/i);

    // Switching to another rail tab must clear the refusal banner
    await page.click('#rtabs button[data-r="kinds"]');
    await page.click('#rtabs button[data-r="src"]');
    await expect(warnbar).toHaveCount(0);

    // Re-trigger the refusal banner again
    await rowRemove.click();
    await expect(warnbar).toBeVisible({ timeout: 5000 });
    await expect(warnbar).toContainText(/still referenced/i);

    // An external or internal document change (e.g. starter load from Examples) must clear the banner
    await page.click('#examplesBtn');
    await expect(page.locator('#examplesOverlay')).toBeVisible();
    const irsaCard = page.locator('.example-card[data-id="irsa"]');
    await irsaCard.locator('button[data-load-id="irsa"]').click();
    await expect(page.locator('#examplesOverlay')).toBeHidden();
    await canvasSettled(page);

    // Switch back to SOURCES if needed and verify warnbar is gone
    await page.click('#rtabs button[data-r="src"]');
    await expect(page.locator('#lrail .warnbar[role="alert"]')).toHaveCount(0);
  });

  test('Inspector error banner clears on user input, generate, and doc change', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Select work-queue resource
    const queueCard = page.locator('.node[data-id="work-queue"]');
    await expect(queueCard).toBeVisible();
    await queueCard.locator('.node-h').click();
    await expect(queueCard).toHaveClass(/sel/);

    // Make an undoable document change by duplicating the resource
    const dupBtn = queueCard.locator('[data-act="duplicate"]');
    await expect(dupBtn).toBeVisible();
    await dupBtn.click();
    const undoBtn = page.locator('#undoBtn');
    await expect(undoBtn).toBeEnabled();

    // Re-select work-queue
    await queueCard.locator('.node-h').click();
    await expect(queueCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const sec = inspector.locator('.insp-sec', { hasText: /annotations/i });
    await expect(sec).toBeVisible();

    const keyInput = sec.locator('input[data-ann-key]');
    const valInput = sec.locator('input[data-ann-value]');
    const addBtn = sec.locator('button[data-ann-add]');

    // Try to add an annotation with empty value
    await keyInput.fill('example.com/test-key');
    await addBtn.click();

    // 1. Error banner should appear in inspector
    const warnbar = inspector.locator('.warnbar');
    await expect(warnbar).toBeVisible();
    await expect(warnbar).toContainText(/requires a value/i);

    // 2. Typing into the value input must immediately clear the error banner
    await valInput.fill('my-val');
    await expect(warnbar).toHaveCount(0);

    // 3. Re-trigger error by submitting with empty key
    await keyInput.fill('');
    await addBtn.click();
    await expect(warnbar).toBeVisible();
    await expect(warnbar).toContainText(/key is required/i);

    // 4. Clicking Generate must clear inspector error banner
    page.on('dialog', d => d.accept());
    const genBtn = page.locator('#generateBtn');
    await expect(genBtn).toBeVisible();
    await genBtn.click();
    await expect(warnbar).toHaveCount(0);

    // 5. Re-trigger error again
    await addBtn.click();
    await expect(warnbar).toBeVisible();

    // 6. Document change (via undoBtn) must clear inspector error banner
    await undoBtn.click();
    await expect(inspector.locator('.warnbar')).toHaveCount(0);
  });
});
