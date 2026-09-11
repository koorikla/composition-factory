// CF-164 — Palette kind preview performs redundant sequential network request downloading full field trees
// Issue #49: showKindPreview must obtain total from kind data already loaded in the client
// (e.g. data-fields attribute or kinds record) rather than issuing a second unconstrained getKindFields request.
const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');
guardPageErrors();

test.describe('CF-164: Palette kind preview redundant request deduplication', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('hovering a kind row triggers only a single API request for requiredOnly: true', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const queueRow = page.locator('#lrail .kind[data-kind="Queue"][data-av*=".m."]');
    await expect(queueRow).toBeVisible({ timeout: 5000 });

    const fieldRequests = [];
    page.on('request', (req) => {
      const url = req.url();
      if (url.includes('/api/kinds/') && url.includes('/fields')) {
        fieldRequests.push(url);
      }
    });

    // Hover over the Queue kind row to trigger kind preview
    await queueRow.hover();

    const prev = page.locator('#kind-preview');
    await expect(prev).toBeVisible({ timeout: 5000 });

    // Verify preview card contents: kind, total field count, required count, required field details
    await expect(prev).toContainText('Queue');
    await expect(prev).toContainText('Namespaced');
    await expect(prev).toContainText(/18 .*fields|fields.*18/i);
    await expect(prev).toContainText('1 required');
    await expect(prev).toContainText('region');
    await expect(prev).toContainText('string');

    // Settle brief delay to ensure no trailing redundant requests fire
    await page.waitForTimeout(400);

    // Assert that exactly one fields API request was sent on hover, and it was required_only=true
    expect(fieldRequests.length).toBe(1);
    expect(fieldRequests[0]).toContain('required_only=true');

    // Assert that no unconstrained full-field-tree request was issued
    const unconstrainedRequests = fieldRequests.filter((u) => !u.includes('required_only=true'));
    expect(unconstrainedRequests).toHaveLength(0);
  });

  test('hovering different kinds triggers only single required_only requests and caches preview', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const queueRow = page.locator('#lrail .kind[data-kind="Queue"][data-av*=".m."]');
    await expect(queueRow).toBeVisible({ timeout: 5000 });

    const fieldRequests = [];
    page.on('request', (req) => {
      const url = req.url();
      if (url.includes('/api/kinds/') && url.includes('/fields')) {
        fieldRequests.push(url);
      }
    });

    // Hover Queue
    await queueRow.hover();
    const prev = page.locator('#kind-preview');
    await expect(prev).toBeVisible({ timeout: 5000 });
    await expect(prev).toContainText('Queue');
    await expect(prev).toContainText(/18 .*fields|fields.*18/i);

    await page.waitForTimeout(300);
    // On unpatched code, 2 requests are sent (one required_only=true, one unconstrained full tree)
    expect(fieldRequests.length).toBe(1);
    expect(fieldRequests[0]).toContain('required_only=true');

    // Leave kind rows
    await page.hover('#psearch');
    await expect(prev).toBeHidden();

    // Re-hover Queue - must use previewCache and fire no new network requests
    await queueRow.hover();
    await expect(prev).toBeVisible({ timeout: 5000 });
    await expect(prev).toContainText('Queue');
    await expect(prev).toContainText(/18 .*fields|fields.*18/i);

    await page.waitForTimeout(300);
    expect(fieldRequests.length).toBe(1);
  });
});
