const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, ENGINE } = require('./helpers');

test.describe('CF-153 — Wire select does not commit on keystroke', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('focusing wire select and pressing key does not commit wire without explicit Enter or selection', async ({ page, request }) => {
    await page.goto('/');

    // Select dead-letter resource card
    await page.click('.node[data-id="dead-letter"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Switch deduplicationScope to wire mode
    const row = page.locator('#insp .fld', { hasText: 'deduplicationScope' }).first();
    await row.locator('button[data-m="w"]').click();

    const sel = page.locator('#insp select[data-wire="deduplicationScope"]');
    await expect(sel).toBeVisible();

    // Focus the select box
    await sel.focus();

    // Press 'r' - on a native select, typing 'r' jumps option selection and fires change event in browsers
    await page.keyboard.press('r');

    // Wait a brief moment to ensure no unintended async save/commit happened
    await page.waitForTimeout(300);

    // Verify persisted document still has NOT wired deduplicationScope
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    const r = doc.spec.resources.find(x => x.name === 'dead-letter');
    const f = r.fields && r.fields.deduplicationScope;
    expect(f && f.from ? f.from : 'unset').toBe('unset');

    // Now explicitly select an option via selectOption
    // When user chooses an option, it commits
    await sel.selectOption({ label: 'params.region' });

    // Verify it commits when explicitly chosen
    await expect.poll(async () => {
      const d = await (await request.get(ENGINE + '/api/blueprint')).json();
      const res = d.spec.resources.find(x => x.name === 'dead-letter');
      return res.fields && res.fields.deduplicationScope && res.fields.deduplicationScope.from || 'unset';
    }).toBe('params.region');
  });

  test('focusing wire select, navigating with keyboard and pressing Enter commits the wire', async ({ page, request }) => {
    await page.goto('/');

    // Select dead-letter resource card
    await page.click('.node[data-id="dead-letter"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Switch deduplicationScope to wire mode
    const row = page.locator('#insp .fld', { hasText: 'deduplicationScope' }).first();
    await row.locator('button[data-m="w"]').click();

    const sel = page.locator('#insp select[data-wire="deduplicationScope"]');
    await expect(sel).toBeVisible();

    // Focus the select box and press 'r' (selecting a matching item)
    await sel.focus();
    await page.keyboard.press('r');

    // Verify still unset before Enter
    await page.waitForTimeout(300);
    const docBefore = await (await request.get(ENGINE + '/api/blueprint')).json();
    const rBefore = docBefore.spec.resources.find(x => x.name === 'dead-letter');
    expect(rBefore.fields && rBefore.fields.deduplicationScope ? rBefore.fields.deduplicationScope.from : 'unset').toBe('unset');

    // Now press Enter
    await page.keyboard.press('Enter');

    // Verify it committed the selected value
    await expect.poll(async () => {
      const d = await (await request.get(ENGINE + '/api/blueprint')).json();
      const res = d.spec.resources.find(x => x.name === 'dead-letter');
      return res.fields && res.fields.deduplicationScope && res.fields.deduplicationScope.from || 'unset';
    }).not.toBe('unset');
  });
});

