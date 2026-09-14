// tests/cf484-inspector-search-envelope.spec.js
const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

// CF-484 — the inspector's field search filters only the spec.forProvider rows,
// so the one word a user types to find the connection secret answers "no match"
// directly above the writeConnectionSecretToRef.name row it is looking at.
test.describe('CF-484 — inspector field search reaches the envelope rows', () => {
  for (const view of ['fields', 'manifest']) {
    for (const term of ['connection', 'secret']) {
      test(`${view} view: "${term}" finds the envelope row instead of reporting no match`, async ({ page }) => {
        await page.goto('/');
        await page.click('.node[data-id="work-queue"] .node-h');
        await page.click(`#vseg button[data-view="${view}"]`);

        // Premise: the row the user is hunting for is in this panel.
        const envRow = page.locator('#insp .insp-sec .fld').filter({ hasText: 'writeConnectionSecretToRef.name' });
        await expect(envRow).toHaveCount(1);

        await page.fill('#insp-search', term);
        await expect(page.locator('#insp-search')).toHaveValue(term);
        await page.waitForTimeout(600); // let the panel re-render around the term

        const matching = await page.locator('#insp .fld').filter({ hasText: 'writeConnectionSecretToRef' }).count();
        const noMatch = (await page.locator('#insp .empty').allTextContents()).join(' | ');

        // A term that matches a row the inspector can show must still show it.
        expect(matching, `searching "${term}" in ${view} view left no writeConnectionSecretToRef row on screen`).toBeGreaterThan(0);
        // And the panel must not tell the user the field does not exist while
        // it is showing that very field.
        expect(noMatch, `searching "${term}" in ${view} view printed a no-match message with ${matching} matching row(s) visible in the same panel`).toBe('');
      });
    }
  }

  test('a term nothing matches still says so', async ({ page }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#vseg button[data-view="fields"]');
    await page.fill('#insp-search', 'zzqqxx-no-such-field');
    await expect(page.locator('#insp > .fld')).toHaveCount(0);
    await expect(page.locator('#insp .empty')).not.toHaveCount(0);
  });
});
