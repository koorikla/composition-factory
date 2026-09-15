// tests/cf498-kinds-rail-name-clip.spec.js
// CF-498 (issue #427) — Kinds rail paints kind names into 60px box with ellipsis causing runs of visually identical rows
//
// Contract:
// Two rows that stand for different kinds must be distinguishable without hovering.
// What must be true: in the kinds rail, a kind name is either shown in full or elided
// such that the rendered text of any two visible rows in the same group differs.

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
  await request.post('/api/providers', {
    data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' }
  });
});

// Helper to compute visible rendered text of a kind row's name element without hovering
async function getVisibleKindName(rowLocator) {
  return await rowLocator.evaluate(row => {
    const nmEl = row.querySelector('.nm');
    if (!nmEl) return '';
    const fullText = (nmEl.textContent || '').trim();
    const style = window.getComputedStyle(nmEl);

    // If it wraps and fits within clientWidth without horizontal overflow:
    if (style.whiteSpace !== 'nowrap' && nmEl.scrollWidth <= nmEl.clientWidth + 1) {
      return fullText;
    }
    // If it's single line and fits completely without overflow:
    if (nmEl.scrollWidth <= nmEl.clientWidth + 1) {
      return fullText;
    }
    // If it overflows horizontally with text-overflow: ellipsis:
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');
    ctx.font = `${style.fontSize} ${style.fontFamily}`;
    const ellipsisWidth = ctx.measureText('…').width;
    const availableWidth = nmEl.clientWidth - ellipsisWidth;
    if (availableWidth <= 0) return '…';
    let visibleLen = 0;
    for (let i = 1; i <= fullText.length; i++) {
      if (ctx.measureText(fullText.slice(0, i)).width <= availableWidth) {
        visibleLen = i;
      } else {
        break;
      }
    }
    return fullText.slice(0, visibleLen) + '…';
  });
}

test('searching "bucket" renders distinguishable kind names without collapsing into identical BucketA… rows', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  const searchInput = page.locator('#psearch');
  await expect(searchInput).toBeVisible();
  await searchInput.fill('bucket');

  // Wait for kinds to render
  const rows = page.locator('#lrail .kind');
  await expect(rows.first()).toBeVisible();

  // Check specific prefix-sharing kinds from literal repro:
  // BucketAccelerateConfiguration vs BucketAnalyticsConfiguration
  const accelRow = page.locator('#lrail .kind[data-kind="BucketAccelerateConfiguration"]').first();
  const analyticsRow = page.locator('#lrail .kind[data-kind="BucketAnalyticsConfiguration"]').first();
  await expect(accelRow).toBeVisible();
  await expect(analyticsRow).toBeVisible();

  const accelText = await getVisibleKindName(accelRow);
  const analyticsText = await getVisibleKindName(analyticsRow);

  expect(accelText).not.toEqual(analyticsText);
  expect(accelText).not.toEqual('BucketA…');
  expect(analyticsText).not.toEqual('BucketA…');

  // Name element should not be locked to a 60-80px box against a 200px+ row
  const firstNm = rows.first().locator('.nm');
  const nmWidth = await firstNm.evaluate(el => el.clientWidth);
  expect(nmWidth, `Kind name column width should be wider than 80px (was ${nmWidth}px)`).toBeGreaterThanOrEqual(140);

  // Across all visible kind rows under search, verify no two different kinds in the same group have identical visible rendered names
  const groupDups = await page.evaluate(() => {
    const lrail = document.querySelector('#lrail');
    if (!lrail) return [];
    const dups = [];
    let currentGroupName = '';
    let seenInGroup = new Map();

    for (const child of lrail.children) {
      if (child.classList.contains('grp')) {
        currentGroupName = child.querySelector('.lbl')?.innerText || '';
        seenInGroup = new Map();
      } else if (child.classList.contains('kind')) {
        const kind = child.getAttribute('data-kind') || '';
        const nmEl = child.querySelector('.nm');
        const vis = (nmEl?.textContent || '').trim();
        if (seenInGroup.has(vis)) {
          dups.push({
            group: currentGroupName,
            kind1: seenInGroup.get(vis),
            kind2: kind,
            visText: vis
          });
        } else {
          seenInGroup.set(vis, kind);
        }
      }
    }
    return dups;
  });

  expect(groupDups, `Found duplicate visible texts in bucket search groups: ${JSON.stringify(groupDups)}`).toEqual([]);
});

test('searching "s3" renders distinct kind rows without collapsing into runs of identical rows', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  const searchInput = page.locator('#psearch');
  await expect(searchInput).toBeVisible();
  await searchInput.fill('s3');

  const rows = page.locator('#lrail .kind');
  await expect(rows.first()).toBeVisible();

  // Groups mentioned in repro:
  // BucketA...: BucketAbac, BucketAccelerateConfiguration, BucketACL, BucketAnalyticsConfiguration
  // BucketI...: BucketIntelligentTieringConfiguration, BucketInventory
  // BucketL...: BucketLifecycleConfiguration, BucketLoggingConfiguration
  const testPairs = [
    ['BucketAccelerateConfiguration', 'BucketAnalyticsConfiguration'],
    ['BucketIntelligentTieringConfiguration', 'BucketInventory'],
    ['BucketLifecycleConfiguration', 'BucketLoggingConfiguration'],
  ];

  for (const [kindA, kindB] of testPairs) {
    const rowA = page.locator(`#lrail .kind[data-kind="${kindA}"]`).first();
    const rowB = page.locator(`#lrail .kind[data-kind="${kindB}"]`).first();
    if (await rowA.isVisible() && await rowB.isVisible()) {
      const textA = await getVisibleKindName(rowA);
      const textB = await getVisibleKindName(rowB);
      expect(textA, `Kinds "${kindA}" and "${kindB}" should not have identical visible text`).not.toEqual(textB);
    }
  }

  // Verify that within each group in the s3 search results, all kind rows have unique visible rendered text
  const groupDups = await page.evaluate(() => {
    const lrail = document.querySelector('#lrail');
    if (!lrail) return [];
    const dups = [];
    let currentGroupName = '';
    let seenInGroup = new Map();

    for (const child of lrail.children) {
      if (child.classList.contains('grp')) {
        currentGroupName = child.querySelector('.lbl')?.innerText || '';
        seenInGroup = new Map();
      } else if (child.classList.contains('kind')) {
        const kind = child.getAttribute('data-kind') || '';
        const nmEl = child.querySelector('.nm');
        const vis = (nmEl?.textContent || '').trim();
        if (seenInGroup.has(vis)) {
          dups.push({
            group: currentGroupName,
            kind1: seenInGroup.get(vis),
            kind2: kind,
            visText: vis
          });
        } else {
          seenInGroup.set(vis, kind);
        }
      }
    }
    return dups;
  });

  expect(groupDups, `Found duplicate visible texts in s3 groups: ${JSON.stringify(groupDups)}`).toEqual([]);
});

test('unfiltered kinds rail renders distinguishable names for native kinds sharing prefixes', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  // Verify native kinds are distinguishable without hovering (not collapsed to Horizontal…)
  const hpaRow = page.locator('#lrail .kind[data-kind="HorizontalPodAutoscaler"]').first();
  await expect(hpaRow).toBeVisible();
  const text = await getVisibleKindName(hpaRow);
  expect(text).not.toEqual('Horizontal…');
  expect(text.startsWith('HorizontalPodAuto')).toBe(true);

  // Across the kinds rail, no two visible rows in the same group have identical rendered text
  const groupDups = await page.evaluate(() => {
    const lrail = document.querySelector('#lrail');
    if (!lrail) return [];
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');

    function getVis(nmEl) {
      if (!nmEl) return '';
      const fullText = (nmEl.textContent || '').trim();
      const style = window.getComputedStyle(nmEl);
      if (style.whiteSpace !== 'nowrap' && nmEl.scrollWidth <= nmEl.clientWidth + 1) {
        return fullText;
      }
      if (nmEl.scrollWidth <= nmEl.clientWidth + 1) {
        return fullText;
      }
      ctx.font = `${style.fontSize} ${style.fontFamily}`;
      const ellipsisWidth = ctx.measureText('…').width;
      const availableWidth = nmEl.clientWidth - ellipsisWidth;
      if (availableWidth <= 0) return '…';
      let visibleLen = 0;
      for (let i = 1; i <= fullText.length; i++) {
        if (ctx.measureText(fullText.slice(0, i)).width <= availableWidth) {
          visibleLen = i;
        } else {
          break;
        }
      }
      return fullText.slice(0, visibleLen) + '…';
    }

    const dups = [];
    let currentGroupName = '';
    let seenInGroup = new Map();

    for (const child of lrail.children) {
      if (child.classList.contains('grp')) {
        currentGroupName = child.querySelector('.lbl')?.innerText || '';
        seenInGroup = new Map();
      } else if (child.classList.contains('kind')) {
        const kind = child.getAttribute('data-kind') || '';
        const vis = getVis(child.querySelector('.nm'));
        if (seenInGroup.has(vis)) {
          dups.push({
            group: currentGroupName,
            kind1: seenInGroup.get(vis),
            kind2: kind,
            visText: vis
          });
        } else {
          seenInGroup.set(vis, kind);
        }
      }
    }
    return dups;
  });

  expect(groupDups, `Found duplicate visible texts in groups: ${JSON.stringify(groupDups)}`).toEqual([]);
});
