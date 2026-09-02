// Slice 66 — Canvas Polish, Engine Selection & Touch Interactions
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');
guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test('engine selection dropdown options are dynamically populated from /api/version', async ({ page, request }) => {
  const verRes = await (await request.get(ENGINE + '/api/version')).json();
  expect(Array.isArray(verRes.engines)).toBe(true);
  expect(verRes.engines).toContain('go-templating');
  expect(verRes.engines).toContain('kcl');
  expect(verRes.engines).toContain('python');

  await page.goto('/');
  const engineSel = page.locator('#engineSel');
  await expect(engineSel).toBeVisible();

  // Verify option elements match /api/version engines
  const options = engineSel.locator('option');
  await expect(options).toHaveCount(verRes.engines.length);

  for (let i = 0; i < verRes.engines.length; i++) {
    await expect(options.nth(i)).toHaveText(verRes.engines[i]);
    await expect(options.nth(i)).toHaveAttribute('value', verRes.engines[i]);
  }
});

test('pointerdown with pointerType=touch does not trigger double-panning', async ({ page }) => {
  await page.goto('/');
  const cw = page.locator('#cw');
  await expect(cw).toBeVisible();

  const initialTransform = await page.locator('#canvas').evaluate(el => el.style.transform);

  // Dispatch a simulated touch pointerdown event on empty canvas
  await page.evaluate(() => {
    const cwEl = document.getElementById('cw');
    const ev = new PointerEvent('pointerdown', {
      bubbles: true,
      cancelable: true,
      clientX: 200,
      clientY: 200,
      button: 0,
      pointerType: 'touch',
    });
    cwEl.dispatchEvent(ev);
  });

  // Canvas view position should remain unshifted
  const currentTransform = await page.locator('#canvas').evaluate(el => el.style.transform);
  expect(currentTransform).toBe(initialTransform);
});

test('Kinds list collapses cluster-scoped duplicates for Namespaced XRD and expands on click', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  // Namespaced group (.m.) is visible and expanded
  const nsGroup = page.locator('#lrail .grp:has-text("sqs.aws.m.upbound.io")');
  await expect(nsGroup).toBeVisible();
  const nsKind = page.locator('#lrail .kind[data-av*=".m."]').first();
  await expect(nsKind).toBeVisible();

  // Cluster-scoped group is collapsed by default and displays cluster-scoped badge + ▶ arrow
  const clusterGroup = page.locator('#lrail .grp[data-grp-toggle="sqs.aws.upbound.io"]');
  await expect(clusterGroup).toBeVisible();
  await expect(clusterGroup.locator('.pill')).toContainText('cluster-scoped');
  await expect(clusterGroup.locator('.grp-toggle')).toHaveText('▶');

  // Cluster-scoped kind inside is not visible while collapsed
  const clusterKind = page.locator('#lrail .kind[data-av="sqs.aws.upbound.io/v1beta1"]');
  await expect(clusterKind).toHaveCount(0);

  // Clicking cluster group header expands it
  await clusterGroup.click();
  await expect(clusterGroup.locator('.grp-toggle')).toHaveText('▼');
  await expect(page.locator('#lrail .kind[data-av="sqs.aws.upbound.io/v1beta1"]').first()).toBeVisible();

  // Clicking again collapses it
  await clusterGroup.click();
  await expect(clusterGroup.locator('.grp-toggle')).toHaveText('▶');
  await expect(page.locator('#lrail .kind[data-av="sqs.aws.upbound.io/v1beta1"]')).toHaveCount(0);
});
