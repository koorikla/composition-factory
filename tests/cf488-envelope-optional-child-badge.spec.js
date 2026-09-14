// tests/cf488-envelope-optional-child-badge.spec.js
const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

// CF-488 — writeConnectionSecretToRef is an optional object whose own schema
// marks `name` required. The envelope renderer flattens it to a leaf row and
// shows that flag unconditionally, so a field required only *if you choose to
// write a connection secret* is presented as required of the resource — while
// the panel's own header count, its missing-required banner, Scaffold Required
// Fields and the emitter all treat the resource as complete without it.
test.describe('CF-488 — an unset optional envelope object reads as optional', () => {
  test('writeConnectionSecretToRef.name is not badged required while the object is unset', async ({ page }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#vseg button[data-view="fields"]');

    const envRow = page.locator('#insp .insp-sec .fld').filter({ hasText: 'writeConnectionSecretToRef.name' });
    await expect(envRow).toHaveCount(1);

    // Control: a field the CRD really does require of this resource keeps its
    // badge and its wording. The fix is about one optional object's children,
    // not about required fields in general.
    const requiredRow = page.locator('#insp > .fld').filter({ hasText: 'region' }).first();
    await expect(requiredRow.locator('.rq')).toHaveCount(1);
    await expect(page.locator('#insp')).toContainText('1 required');

    // The claim under test.
    const badge = await envRow.locator('.rq').count();
    const placeholder = await envRow.locator('input,textarea').first().getAttribute('placeholder').catch(() => null);

    expect(badge, 'writeConnectionSecretToRef.name carries a required badge while the resource generates complete without it').toBe(0);
    expect(
      String(placeholder || '').toLowerCase(),
      'writeConnectionSecretToRef.name reads as required while the resource generates complete without it'
    ).not.toContain('required');
  });

  test('generation agrees: nothing requires it', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    const res = await request.post(new URL('/api/generate', page.url()).toString(), { data: { write: false } });
    expect(res.ok()).toBeTruthy();
    const body = await res.text();
    expect(
      (body.match(/writeConnectionSecretToRef/g) || []).length,
      'the emitter writes a writeConnectionSecretToRef the inspector calls required'
    ).toBe(0);
  });
});
