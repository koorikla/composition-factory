// CF-490 — StatefulSet card shows four required ports, two with identical truncated labels
// Contract: Two ports on one card must not render with the same visible label.
// Enough of each path to tell them apart must be legible without hovering.
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, dropKind } = require('./helpers');

guardPageErrors();

const EMPTY_BLUEPRINT = {
  apiVersion: 'factory.crossplane.io/v1alpha1',
  kind: 'Blueprint',
  metadata: { name: 'blank' },
  spec: {
    xrd: {
      group: 'platform.sparky.ee',
      kind: 'XBlank',
      plural: 'xblanks',
      version: 'v1alpha1',
      scope: 'Namespaced',
      parameters: {},
    },
    resources: [],
  },
};

test.describe('CF-490 — StatefulSet card renders unique port labels', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request, EMPTY_BLUEPRINT);
  });

  test('dropping a StatefulSet renders unique visible labels for all ports on its card', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);

    const card = page.locator('.node[data-id="stateful-set"]');
    await expect(card).toBeVisible();

    // Verify both dataSourceRef ports are present
    const p1 = card.locator('.port[data-path="spec.template.spec.volumes[0].ephemeral.volumeClaimTemplate.spec.dataSourceRef.name"] .nm');
    const p2 = card.locator('.port[data-path="spec.volumeClaimTemplates[0].spec.dataSourceRef.name"] .nm');

    await expect(p1).toBeVisible();
    await expect(p2).toBeVisible();

    const label1 = (await p1.textContent()).trim();
    const label2 = (await p2.textContent()).trim();

    // The two ports must not have identical labels
    expect(label1).not.toBe(label2);

    // All ports on this card must have distinct visible labels
    const allLabels = (await card.locator('.port .nm').allTextContents()).map(s => s.trim());
    const uniqueLabels = new Set(allLabels);
    expect(uniqueLabels.size).toBe(allLabels.length);

    // Ports must be fully visible and not clipped
    const clipped = await page.evaluate(() => {
      const bad = [];
      document.querySelectorAll('.node[data-id="stateful-set"] .port .nm')
        .forEach(el => {
          if (el.scrollWidth > el.clientWidth + 1) bad.push(el.textContent.trim() + ' (' + el.scrollWidth + '>' + el.clientWidth + ')');
        });
      return bad;
    });
    expect(clipped).toEqual([]);
  });
});
