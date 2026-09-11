// tests/cf191-palette-scope-badge-wrap.spec.js
// CF-191 (issue #77) — KINDS group-header scope badge wraps and overprints the first kind row of every group
//
// Contract:
// A group header must render its name, scope badge and count within the palette's
// width without overlapping any kind row, at the default window size (1600x950)
// and at the narrow end of the panel's range (1280x720 / min-width 220px).
// Truncation is acceptable; overprinting is not.

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test('KINDS group-header scope badge does not wrap or overlap first kind row at 1600x950', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 950 });
  await page.goto('/');

  const lrail = page.locator('#lrail');
  await expect(lrail).toBeVisible();

  // Wait for kinds to render
  const firstKind = page.locator('#lrail .kind').first();
  await expect(firstKind).toBeVisible();

  const palette = page.locator('#region-palette');
  const paletteBox = await palette.boundingBox();
  expect(paletteBox).not.toBeNull();

  // Inspect all groups with scope badges
  const targetGroups = ['apps', 'batch', 'networking.k8s.io', 'rbac.authorization.k8s.io', 'sqs.aws.m.upbound.io'];

  for (const groupName of targetGroups) {
    const grp = page.locator('#lrail .grp', { hasText: groupName });
    await expect(grp).toBeVisible();

    const pill = grp.locator('.pill');
    await expect(pill).toBeVisible();

    const grpBox = await grp.boundingBox();
    const pillBox = await pill.boundingBox();
    const countEl = grp.locator('.n');
    const countBox = await countEl.boundingBox();

    const lblEl = grp.locator('.lbl');
    const textInfo = await pill.evaluate((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      const rects = Array.from(range.getClientRects());
      return {
        lineCount: rects.length,
        rects: rects.map(r => ({ top: r.top, bottom: r.bottom, height: r.height, left: r.left, right: r.right })),
        scrollHeight: el.scrollHeight,
        clientHeight: el.clientHeight,
        whiteSpace: window.getComputedStyle(el).whiteSpace,
      };
    });

    // 1. Group children (name, pill, count) must not lay out beyond the palette width
    expect(pillBox.x + pillBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);
    expect(countBox.x + countBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);

    // 2. The pill must render on a single line and not wrap or overflow vertically
    expect(textInfo.lineCount).toBe(1);
    expect(textInfo.scrollHeight).toBeLessThanOrEqual(textInfo.clientHeight);
    expect(textInfo.whiteSpace).toBe('nowrap');

    // 3. Must not overlap the kind row immediately beneath it
    const nextKind = grp.locator('+ .kind');
    if (await nextKind.isVisible()) {
      const kindBox = await nextKind.boundingBox();
      const textBottom = textInfo.rects.length > 0 ? Math.max(...textInfo.rects.map(r => r.bottom)) : pillBox.y + pillBox.height;
      expect(textBottom).toBeLessThanOrEqual(kindBox.y);
      expect(pillBox.y + pillBox.height).toBeLessThanOrEqual(kindBox.y);
      expect(grpBox.y + grpBox.height).toBeLessThanOrEqual(kindBox.y);
    }
  }
});

test('KINDS group headers and scope badges stay within palette width and do not overlap kind rows at 1280x720', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto('/');

  const lrail = page.locator('#lrail');
  await expect(lrail).toBeVisible();
  await expect(page.locator('#lrail .kind').first()).toBeVisible();

  const palette = page.locator('#region-palette');
  const paletteBox = await palette.boundingBox();
  expect(paletteBox).not.toBeNull();

  // Check every group that has a scope badge
  const grpsWithPill = page.locator('#lrail .grp:has(.pill)');
  const count = await grpsWithPill.count();
  expect(count).toBeGreaterThan(0);

  for (let i = 0; i < count; i++) {
    const grp = grpsWithPill.nth(i);
    const pill = grp.locator('.pill');
    const countEl = grp.locator('.n');

    const grpBox = await grp.boundingBox();
    const pillBox = await pill.boundingBox();
    const countBox = await countEl.boundingBox();

    // Must stay within palette width
    expect(pillBox.x + pillBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);
    expect(countBox.x + countBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);

    // Must not overlap next kind row
    const nextKind = grp.locator('+ .kind');
    if (await nextKind.isVisible()) {
      const kindBox = await nextKind.boundingBox();
      expect(pillBox.y + pillBox.height).toBeLessThanOrEqual(kindBox.y);
      expect(grpBox.y + grpBox.height).toBeLessThanOrEqual(kindBox.y);
    }
  }
});

test('at narrow end of palette range after resizing, scope badge and group header stay within width without overlap', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto('/');

  const handle = page.locator('#col-resize-l');
  await expect(handle).toBeVisible();
  const hb = await handle.boundingBox();
  // Drag column left towards min-width
  await page.mouse.move(hb.x + hb.width / 2, hb.y + 200);
  await page.mouse.down();
  await page.mouse.move(hb.x + hb.width / 2 - 100, hb.y + 200, { steps: 5 });
  await page.mouse.up();

  const palette = page.locator('#region-palette');
  const paletteBox = await palette.boundingBox();

  const grpsWithPill = page.locator('#lrail .grp:has(.pill)');
  const count = await grpsWithPill.count();
  for (let i = 0; i < count; i++) {
    const grp = grpsWithPill.nth(i);
    const pill = grp.locator('.pill');
    const countEl = grp.locator('.n');

    const grpBox = await grp.boundingBox();
    const pillBox = await pill.boundingBox();
    const countBox = await countEl.boundingBox();

    expect(pillBox.x + pillBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);
    expect(countBox.x + countBox.width).toBeLessThanOrEqual(paletteBox.x + paletteBox.width + 1);

    const nextKind = grp.locator('+ .kind');
    if (await nextKind.isVisible()) {
      const kindBox = await nextKind.boundingBox();
      expect(pillBox.y + pillBox.height).toBeLessThanOrEqual(kindBox.y);
      expect(grpBox.y + grpBox.height).toBeLessThanOrEqual(kindBox.y);
    }
  }
});

