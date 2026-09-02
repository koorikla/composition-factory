const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, clickWire } = require('./helpers')
guardPageErrors()

test.describe('Select and delete wires on canvas', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('clicking a wire selects it and displays a floating delete button', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
    await canvasSettled(page);

    // Click the first wire path
    await clickWire(page, 0);

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
    await canvasSettled(page);

    // Select the first wire
    await clickWire(page, 0);
    await expect(page.locator('svg.wires .wire-del-btn')).toBeVisible();

    // Click the delete button
    await page.locator('svg.wires .wire-del-btn circle').click({ force: true });

    // Wire count decreases to 2
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(2);
    await expect(page.locator('svg.wires .wire-del-btn')).toHaveCount(0);

    // Undo restores the wire
    await page.keyboard.press('ControlOrMeta+z');
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(3);
  });

  // The delete button is positioned by a transform ATTRIBUTE on its <g>. On an
  // SVG element that is the same CSS property as `transform`, so a bare
  // :hover{transform:scale()} on the same element silently drops the translate
  // and throws the button at the SVG origin — away from the cursor hovering it.
  test('the floating delete button grows in place when hovered', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
    await canvasSettled(page);

    await clickWire(page, 0);
    const delBtn = page.locator('svg.wires .wire-del-btn');
    await expect(delBtn).toBeVisible();

    const centre = async () => {
      const r = await delBtn.evaluate((e) => e.getBoundingClientRect().toJSON());
      return { x: r.x + r.width / 2, y: r.y + r.height / 2, w: r.width };
    };
    // read a size the .12s grow transition has finished moving, never a frame
    // partway through it
    const settled = async () => {
      let prev = null;
      const deadline = Date.now() + 5000;
      for (;;) {
        const c = await centre();
        if (prev && Math.abs(c.w - prev.w) < 0.01) return c;
        if (Date.now() > deadline) return c;
        prev = c;
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      }
    };

    // clickWire leaves the pointer on the wire's midpoint, which is where the
    // button just appeared — park it elsewhere so the baseline is the un-hovered
    // size rather than an already-growing one
    await page.mouse.move(4, 4);
    const before = await settled();

    // a raw move, not hover(): hover() scrolls the element into view, and
    // svg.wires{overflow:visible} gives the overflow:hidden canvas something to
    // scroll — which would shift the button for reasons that have nothing to
    // do with the transform under test
    await page.mouse.move(before.x, before.y);
    await expect.poll(async () => (await centre()).w).toBeGreaterThan(before.w + 1);

    const after = await settled();
    expect(Math.abs(after.x - before.x)).toBeLessThan(1);
    expect(Math.abs(after.y - before.y)).toBeLessThan(1);
  });

  test('pressing Delete or Backspace removes the selected wire', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
    await canvasSettled(page);

    // Select the first wire
    await clickWire(page, 0);
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1);

    // Press Backspace
    await page.keyboard.press('Backspace');

    // Wire is deleted
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(2);

    // Select another wire and press Delete key
    await clickWire(page, 0);
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1);
    await page.keyboard.press('Delete');

    // Wire count is now 1
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(1);

    // Undo restores both
    await page.keyboard.press('ControlOrMeta+z');
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(2);
    await page.keyboard.press('ControlOrMeta+z');
    await expect.poll(async () => await page.locator('svg.wires path.wire-path').count()).toBe(3);
  });

  test('right-clicking a wire opens context menu with delete option', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3);
    await canvasSettled(page);

    // Right-click a wire
    await clickWire(page, 0, { button: 'right' });

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
