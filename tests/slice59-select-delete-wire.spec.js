const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.describe('Select and delete wires on canvas', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('clicking a wire selects it and displays a floating delete button', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);

    // Click the first wire path
    const wire = page.locator('svg.wires path.wire-path').first();
    await wire.click({ force: true });

    // The wire is visually selected with .wire-selected
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1);

    // A delete button appears on the selected wire
    const delBtn = page.locator('svg.wires .wire-del-btn');
    await expect(delBtn).toBeVisible();

    // Clicking empty canvas space deselects the wire
    await page.click('#canvas', { position: { x: 50, y: 50 } });
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(0);
    await expect(page.locator('svg.wires .wire-del-btn')).toHaveCount(0);
  });

  test('clicking the floating delete button removes the wire and is undoable', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);

    // Select the first wire
    await page.locator('svg.wires path.wire-path').first().click({ force: true });
    await expect(page.locator('svg.wires .wire-del-btn')).toBeVisible();

    // Click the delete button
    await page.locator('svg.wires .wire-del-btn').click({ force: true });

    // Wire count decreases to 2
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(2);
    await expect(page.locator('svg.wires .wire-del-btn')).toHaveCount(0);

    // Undo restores the wire
    await page.keyboard.press('Meta+z');
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
  });

  test('pressing Delete or Backspace removes the selected wire', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);

    // Select the first wire
    await page.locator('svg.wires path.wire-path').first().click({ force: true });
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1);

    // Press Backspace
    await page.keyboard.press('Backspace');

    // Wire is deleted
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(2);

    // Select another wire and press Delete key
    await page.locator('svg.wires path.wire-path').first().click({ force: true });
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1);
    await page.keyboard.press('Delete');

    // Wire count is now 1
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(1);

    // Undo restores both
    await page.keyboard.press('Meta+z');
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(2);
    await page.keyboard.press('Meta+z');
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
  });

  test('right-clicking a wire opens context menu with delete option', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);

    // Right-click a wire
    await page.locator('svg.wires path.wire-path').first().click({ button: 'right', force: true });

    // Context menu opens with Delete wire
    const ctxMenu = page.locator('#ctx-menu');
    await expect(ctxMenu).toBeVisible();
    await expect(ctxMenu).toContainText('Delete wire');

    // Click Delete wire in context menu
    await ctxMenu.locator('button:has-text("Delete wire")').click();

    // Wire is deleted
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(2);
    await expect(ctxMenu).not.toBeVisible();
  });
});
