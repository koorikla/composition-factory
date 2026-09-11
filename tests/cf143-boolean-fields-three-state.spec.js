const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, dropKind } = require('./helpers');

test.describe('CF-143 — Boolean fields render as three-state control (unset / true / false)', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });


  test('boolean fields render as a three-state select instead of a free-text input and cycle unset / true / false cleanly', async ({ page, request }) => {
    await page.goto('/');

    // Drop Deployment (native kind with boolean fields like spec.paused)
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    const card = page.locator('.node[data-id="deployment"]');
    await expect(card).toBeVisible();

    await page.click('.node[data-id="deployment"] .node-h');
    const insp = page.locator('#insp');
    await expect(insp).toBeVisible();

    // Switch to All filter to see all fields including spec.paused
    await page.click('#fseg button[data-f="all"]');

    const pausedRow = insp.locator('.fld:has(.n:text-is("spec.paused"))');
    await expect(pausedRow).toBeVisible();

    // 1. Contract check: must NOT render a free-text input with placeholder "unset — omitted from output"
    await expect(pausedRow.locator('input.val[data-v="spec.paused"]')).toHaveCount(0);

    // 2. Must render a three-state control (select.val) with unset / true / false options
    const select = pausedRow.locator('select.val[data-v="spec.paused"]');
    await expect(select).toBeVisible();
    await expect(select).toHaveValue('');

    const options = select.locator('option');
    await expect(options).toHaveCount(3);
    await expect(options.nth(0)).toHaveText(/unset/);
    await expect(options.nth(0)).toHaveAttribute('value', '');
    await expect(options.nth(1)).toHaveText('true');
    await expect(options.nth(1)).toHaveAttribute('value', 'true');
    await expect(options.nth(2)).toHaveText('false');
    await expect(options.nth(2)).toHaveAttribute('value', 'false');

    // 3. Select "true" -> should persist { value: "true" } in blueprint
    await select.selectOption('true');
    await expect.poll(async () => {
      const doc = await (await request.get('http://127.0.0.1:' + new URL(page.url()).port + '/api/blueprint')).json();
      const dep = doc.spec.resources.find(r => r.name === 'deployment');
      return dep && dep.fields && dep.fields['spec.paused'] ? dep.fields['spec.paused'].value : null;
    }).toBe('true');

    // Inspector re-render preserves selection "true"
    await expect(select).toHaveValue('true');

    // 4. Select "false" -> should persist { value: "false" } in blueprint
    await select.selectOption('false');
    await expect.poll(async () => {
      const doc = await (await request.get('http://127.0.0.1:' + new URL(page.url()).port + '/api/blueprint')).json();
      const dep = doc.spec.resources.find(r => r.name === 'deployment');
      return dep && dep.fields && dep.fields['spec.paused'] ? dep.fields['spec.paused'].value : null;
    }).toBe('false');

    await expect(select).toHaveValue('false');

    // 5. Select "unset" -> should clear the field (delete from fields)
    await select.selectOption('');
    await expect.poll(async () => {
      const doc = await (await request.get('http://127.0.0.1:' + new URL(page.url()).port + '/api/blueprint')).json();
      const dep = doc.spec.resources.find(r => r.name === 'deployment');
      return dep && dep.fields && dep.fields['spec.paused'] ? dep.fields['spec.paused'] : null;
    }).toBeNull();

    await expect(select).toHaveValue('');
  });
});
