const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');
guardPageErrors();

test.describe('CF-165: Canvas wire rendering rect cache', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('cwEl.getBoundingClientRect() is called at most once per wire draw cycle', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const wireCount = await page.locator('svg.wires path.wire-path').count();
    expect(wireCount).toBeGreaterThanOrEqual(3);

    // Instrument getBoundingClientRect to count invocations on #cw vs port dots (.d)
    await page.evaluate(() => {
      window.__cwRectCalls = 0;
      window.__portRectCalls = 0;
      const orig = Element.prototype.getBoundingClientRect;
      Element.prototype.getBoundingClientRect = function () {
        if (this && this.id === 'cw') {
          window.__cwRectCalls++;
        } else if (this && this.classList && this.classList.contains('d')) {
          window.__portRectCalls++;
        }
        return orig.apply(this, arguments);
      };
    });

    // Reset counters and trigger a wire redraw cycle via selection change
    await page.evaluate(() => {
      window.__cwRectCalls = 0;
      window.__portRectCalls = 0;
      window.store.select('work-queue');
    });

    const counts = await page.evaluate(() => ({
      cwCalls: window.__cwRectCalls,
      portCalls: window.__portRectCalls,
    }));

    // With 3 wires, 6 port endpoints are queried.
    expect(counts.portCalls).toBeGreaterThanOrEqual(6);
    // Without cache, cwEl.getBoundingClientRect() is called for every port resolution (6 times).
    // With cache, cwEl.getBoundingClientRect() is called at most once per drawWires() pass.
    expect(counts.cwCalls).toBeLessThanOrEqual(1);
    expect(counts.cwCalls).toBeGreaterThanOrEqual(1);
  });

  test('wire positions remain pixel-accurate with cached container rect', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Verify all wire endpoints match the exact center coordinates of their target ports
    const accuracy = await page.evaluate(() => {
      const cw = document.getElementById('cw');
      const cwRect = cw.getBoundingClientRect();
      const wirePaths = Array.from(document.querySelectorAll('svg.wires path.wire-path'));

      return wirePaths.map((p) => {
        const d = p.getAttribute('d');
        const parts = d.split(' ');
        const startParts = parts[0].slice(1).split(',');
        const endParts = parts[3].split(',');
        const startX = parseFloat(startParts[0]);
        const startY = parseFloat(startParts[1]);
        const endX = parseFloat(endParts[0]);
        const endY = parseFloat(endParts[1]);

        const idx = parseInt(p.getAttribute('data-wire-idx'), 10);
        return { idx, startX, startY, endX, endY };
      });
    });

    expect(accuracy.length).toBeGreaterThanOrEqual(3);

    // Verify the first wire: XRD retention -> work-queue messageRetentionSeconds
    const firstWireCoords = await page.evaluate(() => {
      const cw = document.getElementById('cw');
      const cwRect = cw.getBoundingClientRect();

      const startDot = document.querySelector('.port[data-owner="xrd"][data-path="retention"] .d');
      const endDot = document.querySelector('.port[data-owner="work-queue"][data-path="messageRetentionSeconds"] .d');

      const sR = startDot.getBoundingClientRect();
      const eR = endDot.getBoundingClientRect();

      return {
        expectedStart: {
          x: sR.left - cwRect.left + sR.width / 2,
          y: sR.top - cwRect.top + sR.height / 2,
        },
        expectedEnd: {
          x: eR.left - cwRect.left + eR.width / 2,
          y: eR.top - cwRect.top + eR.height / 2,
        },
      };
    });

    expect(Math.abs(accuracy[0].startX - firstWireCoords.expectedStart.x)).toBeLessThan(0.01);
    expect(Math.abs(accuracy[0].startY - firstWireCoords.expectedStart.y)).toBeLessThan(0.01);
    expect(Math.abs(accuracy[0].endX - firstWireCoords.expectedEnd.x)).toBeLessThan(0.01);
    expect(Math.abs(accuracy[0].endY - firstWireCoords.expectedEnd.y)).toBeLessThan(0.01);
  });

  test('card dragging redrawing wires queries cwEl.getBoundingClientRect at most once per draw frame', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Instrument getBoundingClientRect and measure calls during a card drag operation
    await page.evaluate(() => {
      window.__cwRectCalls = 0;
      window.__portRectCalls = 0;
      const orig = Element.prototype.getBoundingClientRect;
      Element.prototype.getBoundingClientRect = function () {
        if (this && this.id === 'cw') {
          window.__cwRectCalls++;
        } else if (this && this.classList && this.classList.contains('d')) {
          window.__portRectCalls++;
        }
        return orig.apply(this, arguments);
      };
    });

    const node = page.locator('.node[data-id="work-queue"]');
    const head = node.locator('.node-h');
    const box = await head.boundingBox();

    // Reset counters right before drag
    await page.evaluate(() => {
      window.__cwRectCalls = 0;
      window.__portRectCalls = 0;
    });

    // Perform a small 3-step drag
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 30, box.y + box.height / 2 + 20, { steps: 3 });
    await page.mouse.up();
    await canvasSettled(page);

    const results = await page.evaluate(() => ({
      cwCalls: window.__cwRectCalls,
      portCalls: window.__portRectCalls,
    }));

    // Each wire draw query resolves at least 3 wires (6 endpoints).
    // In unpatched code, cwCalls is equal to portCalls (1 cw call per port call, so 6x per frame).
    // In patched code, cwCalls is at most portCalls / 6 (1 cw call per frame).
    // Therefore, cwCalls should be strictly less than portCalls / 2.
    expect(results.portCalls).toBeGreaterThan(0);
    expect(results.cwCalls).toBeLessThanOrEqual(Math.ceil(results.portCalls / 4));
  });
});
