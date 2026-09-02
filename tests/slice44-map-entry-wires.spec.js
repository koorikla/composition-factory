const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.describe('First-Class Map-Entry Wires in Fields', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('can add map entries, wire one to a parameter, and see nested yaml output', async ({ page, request }) => {
    await page.goto('/');

    // Select the work-queue resource node
    await page.click('.node[data-id="work-queue"] .node-h');

    // Switch to All filter in inspector
    await page.click('#fseg button[data-f="all"]');

    // Find tags field and click "+ Add key"
    const addKeyBtn = page.locator('button[data-add-map-entry="tags"]');
    await expect(addKeyBtn).toBeVisible();
    await addKeyBtn.click();

    // Fill key name "Team", value "infrastructure" and click Add
    await page.fill('input[data-new-map-key="tags"]', 'Team');
    await page.fill('input[data-new-map-val="tags"]', 'infrastructure');
    await page.click('button[data-new-map-ok="tags"]');

    // Add another key "Environment"
    await addKeyBtn.click();
    await page.fill('input[data-new-map-key="tags"]', 'Environment');
    await page.click('button[data-new-map-ok="tags"]');

    // Switch Environment to Wire mode (W)
    await page.click('button[data-m="w"][data-path="tags[Environment]"]');

    // Select params.region (or available param) for Environment
    await page.selectOption('select[data-wire="tags[Environment]"]', 'params.region');

    // Verify persisted doc has both fields
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const q = doc.spec.resources?.find(r => r.name === 'work-queue');
      return {
        team: q?.fields?.['tags[Team]']?.value,
        env: q?.fields?.['tags[Environment]']?.from,
      };
    }).toEqual({
      team: 'infrastructure',
      env: 'params.region',
    });

    // Check composition.yaml output in output drawer
    await page.click('#tabs button[data-t="comp"]');
    await expect(page.locator('#code')).toContainText('tags:');
    await expect(page.locator('#code')).toContainText("Team: 'infrastructure'");
    await expect(page.locator('#code')).toContainText('Environment: {{ $spec.region }}');

    // Delete tags[Team] entry
    await page.click('button[data-del-map-entry="tags[Team]"]');

    // Verify deletion in doc
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const q = doc.spec.resources?.find(r => r.name === 'work-queue');
      return q?.fields?.['tags[Team]'];
    }).toBeUndefined();
  });
});
