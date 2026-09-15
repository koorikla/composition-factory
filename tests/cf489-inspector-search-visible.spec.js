// tests/cf489-inspector-search-visible.spec.js
const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, dropKind } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test.describe('CF-489: Inspector field-search matches visible on large kinds', () => {
  test('Manifest view: typing a term into field search on a long-form kind brings matches into view or shows count next to input', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);
    await page.click('.node[data-id="stateful-set"] .node-h');
    await page.click('#vseg button[data-view="manifest"]');

    await expect(page.locator('#region-inspector')).toBeVisible();

    // Type 'replicas' into search fields
    await page.fill('#insp-search', 'replicas');
    await expect(page.locator('#insp-search')).toHaveValue('replicas');
    await page.waitForTimeout(600); // let debounce and render finish

    const countBadge = page.locator('#insp-tools #insp-search-count');
    const searchHits = page.locator('#insp .search-hits');

    await expect(searchHits).toBeAttached();

    const hasVisibleCount = await countBadge.isVisible().catch(() => false);
    if (hasVisibleCount) {
      const text = (await countBadge.textContent()) || '';
      expect(text).toMatch(/4/);
    }

    const inspBox = await page.locator('#insp').boundingBox();
    const hitsBox = await searchHits.boundingBox();

    // The matches must be visible within the inspector box viewport, or a count badge must be visible next to the input
    const isHitsInView = !!(inspBox && hitsBox &&
      hitsBox.y >= inspBox.y &&
      hitsBox.y < inspBox.y + inspBox.height);

    expect(
      hasVisibleCount || isHitsInView,
      `Neither visible count next to input nor search matches brought into view for 'replicas' in manifest view (inspBox=${JSON.stringify(inspBox)}, hitsBox=${JSON.stringify(hitsBox)}, hasVisibleCount=${hasVisibleCount})`
    ).toBe(true);

    // Both should ideally be true
    expect(hasVisibleCount).toBe(true);
    expect(isHitsInView).toBe(true);
  });

  test('Fields view: typing a term into field search on a long-form kind brings matches into view or shows count next to input', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);
    await page.click('.node[data-id="stateful-set"] .node-h');
    await page.click('#vseg button[data-view="fields"]');

    await expect(page.locator('#region-inspector')).toBeVisible();

    // Type 'replicas' into search fields
    await page.fill('#insp-search', 'replicas');
    await expect(page.locator('#insp-search')).toHaveValue('replicas');
    await page.waitForTimeout(600);

    const countBadge = page.locator('#insp-tools #insp-search-count');
    const firstMatch = page.locator('#insp > .fld').first();

    await expect(firstMatch).toBeAttached();

    const hasVisibleCount = await countBadge.isVisible().catch(() => false);
    if (hasVisibleCount) {
      const text = (await countBadge.textContent()) || '';
      expect(text).toMatch(/4/);
    }

    const inspBox = await page.locator('#insp').boundingBox();
    const firstMatchBox = await firstMatch.boundingBox();

    const isHitsInView = !!(inspBox && firstMatchBox &&
      firstMatchBox.y >= inspBox.y - 2 &&
      firstMatchBox.y < inspBox.y + inspBox.height);

    expect(
      hasVisibleCount || isHitsInView,
      `Neither visible count next to input nor search matches brought into view for 'replicas' in fields view (inspBox=${JSON.stringify(inspBox)}, firstMatchBox=${JSON.stringify(firstMatchBox)}, hasVisibleCount=${hasVisibleCount})`
    ).toBe(true);

    expect(hasVisibleCount).toBe(true);
    expect(isHitsInView).toBe(true);
  });

  test('SQS Queue (short essentials form): search shows count and matches remain visible', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Queue', 'v1beta1', 400, 300);
    await page.click('.node[data-id="queue"] .node-h');
    await page.click('#vseg button[data-view="manifest"]');

    await page.fill('#insp-search', 'retention');
    await page.waitForTimeout(600);

    const countBadge = page.locator('#insp-tools #insp-search-count');
    await expect(countBadge).toBeVisible();
    await expect(countBadge).toContainText('1 match');

    const searchHits = page.locator('#insp .search-hits');
    await expect(searchHits).toBeVisible();
    await expect(searchHits).toContainText('messageRetentionSeconds');

    const inspBox = await page.locator('#insp').boundingBox();
    const hitsBox = await searchHits.boundingBox();
    expect(hitsBox.y >= inspBox.y && hitsBox.y < inspBox.y + inspBox.height).toBe(true);
  });

  test('no-match search displays 0 matches and empty message', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);
    await page.click('.node[data-id="stateful-set"] .node-h');
    await page.click('#vseg button[data-view="manifest"]');

    await page.fill('#insp-search', 'zzqqxx-no-such-field');
    await page.waitForTimeout(600);

    const countBadge = page.locator('#insp-tools #insp-search-count');
    await expect(countBadge).toBeVisible();
    await expect(countBadge).toContainText('0 matches');

    const emptyMsg = page.locator('#insp .empty');
    await expect(emptyMsg).toBeVisible();
    await expect(emptyMsg).toContainText('No schema field matches');
  });

  test('clearing the search resets the count indicator', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);
    await page.click('.node[data-id="stateful-set"] .node-h');

    await page.fill('#insp-search', 'replicas');
    await page.waitForTimeout(600);

    const countBadge = page.locator('#insp-tools #insp-search-count');
    await expect(countBadge).toBeVisible();

    // Clear search
    await page.fill('#insp-search', '');
    await page.waitForTimeout(600);

    await expect(countBadge).toBeHidden();
  });
});
