const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-141 — Inspector badges nested optional leaves correctly', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  async function dropKind(page, kind, av, x, y) {
    await page.evaluate(({ kind, av, x, y }) => {
      const row = document.querySelector('.kind[data-kind="' + kind + '"][data-av="' + av + '"]');
      const cw = document.getElementById('cw');
      const r = cw.getBoundingClientRect();
      const dt = new DataTransfer();
      row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }));
      const ev = { clientX: r.left + x, clientY: r.top + y };
      cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }));
      cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }));
    }, { kind, av, x, y });
  }

  test('nested leaf in an optional object is not badged REQ in All filter or counted in required when parent is unset', async ({ page }) => {
    await page.goto('/');

    // Drop Deployment (native kind with nested required leaves inside optional objects, e.g. containers[0].name / env[0].name)
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    const card = page.locator('.node[data-id="deployment"]');
    await expect(card).toBeVisible();

    await page.click('.node[data-id="deployment"] .node-h');
    const insp = page.locator('#insp');
    await expect(insp).toBeVisible();

    // Inspector header should show the required count matching effective requiredness (selector + template branches = 2)
    // and NOT the ~250 raw required fields!
    const header = insp.locator('.insp-t .g');
    await expect(header).toBeVisible();
    const headerText = await header.textContent();
    expect(headerText).toContain('2 required');

    // Switch to All filter
    await page.click('#fseg button[data-f="all"]');

    // Find spec.template.spec.containers[0].name
    const containerNameRow = insp.locator('.fld:has(.n:text-is("spec.template.spec.containers[0].name"))');
    await expect(containerNameRow).toBeVisible();

    // Because containers is unset and optional, containerNameRow must NOT have .rq badge or required placeholder
    await expect(containerNameRow.locator('.rq')).toHaveCount(0);
    const input = containerNameRow.locator('input.val');
    await expect(input).toHaveAttribute('placeholder', 'unset — omitted from output');

    // Now set an image in containers[0].image
    const containerImageRow = insp.locator('.fld:has(.n:text-is("spec.template.spec.containers[0].image"))');
    await expect(containerImageRow).toBeVisible();
    const imageInput = containerImageRow.locator('input.val');
    await imageInput.fill('nginx:latest');
    await imageInput.blur();

    // Now that containers[0] is set/present, required leaves inside containers[0] (like name) become effectively required!
    await expect(containerNameRow.locator('.rq')).toHaveCount(1);
    await expect(input).toHaveAttribute('placeholder', 'required — set a value or wire it');
  });
});
