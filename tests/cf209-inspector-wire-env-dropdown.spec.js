const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-209 — Resource inspector wire dropdown lists declared environment keys', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('inspector wire dropdown renders compatible env keys, selects them, commits wire, and styles env wire badge', async ({ page, request }) => {
    // 1. Seed blueprint with spec.environment declaring string and integer keys,
    // and seed dead-letter.region wired to env.clusterName.
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: {
        type: 'string',
        description: 'Target cluster name'
      },
      maxRetries: {
        type: 'integer',
        default: '3',
        description: 'Maximum retries'
      }
    };
    const dl = docWithEnv.spec.resources.find((r) => r.name === 'dead-letter');
    dl.fields = dl.fields || {};
    dl.fields.region = { from: 'env.clusterName' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Select dead-letter resource card
    await page.click('.node[data-id="dead-letter"] .node-h');
    const insp = page.locator('#insp');
    await expect(insp).toBeVisible();

    // 3. Verify that dead-letter.region displays an env-styled wire badge (--shared / .shared)
    const regionRow = insp.locator('.fld:has(span.n:text-is("region"))');
    await expect(regionRow).toBeVisible();
    const wireBadge = regionRow.locator('.bound');
    await expect(wireBadge).toBeVisible();
    await expect(wireBadge.locator('.src')).toHaveText('env.clusterName');
    // Badge styling check: should have shared class and --shared color
    await expect(wireBadge).toHaveClass(/shared/);
    const srcSpan = wireBadge.locator('.src');
    await expect(srcSpan).toHaveClass(/sh/);

    // 4. Remove wire using the unwire button
    const unwireBtn = wireBadge.locator('[data-unwire="region"]');
    await expect(unwireBtn).toBeVisible();
    await unwireBtn.click();

    // Verify document was updated to remove region wire
    await expect.poll(async () => {
      const doc = await (await request.get(`${ENGINE}/api/blueprint`)).json();
      const r = doc.spec.resources.find((x) => x.name === 'dead-letter');
      return (r.fields && r.fields.region && r.fields.region.from) || null;
    }).toBeNull();

    // 5. Click "W" (wire mode) for region (string field)
    await regionRow.locator('button[data-m="w"]').click();

    // The wire select dropdown should now be rendered
    const regionWireSel = insp.locator('select[data-wire="region"]');
    await expect(regionWireSel).toBeVisible();

    // Environment optgroup should exist and contain compatible env.clusterName (string)
    const envOptgroup = regionWireSel.locator('optgroup[label="Environment"]');
    await expect(envOptgroup).toBeAttached();
    const clusterOpt = envOptgroup.locator('option[value="env.clusterName"]');
    await expect(clusterOpt).toHaveCount(1);
    await expect(clusterOpt).toHaveText('env.clusterName (string)');

    // Incompatible integer key (maxRetries) must NOT be present in region wire dropdown
    const incompatibleOpt = regionWireSel.locator('option[value="env.maxRetries"]');
    await expect(incompatibleOpt).toHaveCount(0);

    // 6. Check integer field compatibility: switch delaySeconds to wire mode
    await page.click('#fseg button[data-f="all"]');
    const delayRow = insp.locator('.fld:has(span.n:text-is("delaySeconds"))');
    await delayRow.locator('button[data-m="w"]').click();
    const delayWireSel = insp.locator('select[data-wire="delaySeconds"]');
    await expect(delayWireSel).toBeVisible();

    const delayEnvOptgroup = delayWireSel.locator('optgroup[label="Environment"]');
    await expect(delayEnvOptgroup).toBeAttached();
    const retriesOpt = delayEnvOptgroup.locator('option[value="env.maxRetries"]');
    await expect(retriesOpt).toHaveCount(1);
    await expect(retriesOpt).toHaveText('env.maxRetries (integer)');

    // String key (clusterName) must NOT be present in delaySeconds (integer) wire dropdown
    await expect(delayWireSel.locator('option[value="env.clusterName"]')).toHaveCount(0);

    // 7. Select env.clusterName in region dropdown and verify it commits to document
    await regionWireSel.selectOption('env.clusterName');

    await expect.poll(async () => {
      const doc = await (await request.get(`${ENGINE}/api/blueprint`)).json();
      const r = doc.spec.resources.find((x) => x.name === 'dead-letter');
      return (r.fields && r.fields.region && r.fields.region.from) || null;
    }).toBe('env.clusterName');

    // 8. Verify the inspector re-renders with the env-styled wire badge
    const newBadge = regionRow.locator('.bound');
    await expect(newBadge).toBeVisible();
    await expect(newBadge).toHaveClass(/shared/);
    await expect(newBadge.locator('.src')).toHaveText('env.clusterName');

    // 9. When already wired to env.clusterName, entering wire mode again has env.clusterName selected
    await regionRow.locator('button[data-m="w"]').click();
    const reWireSel = insp.locator('select[data-wire="region"]');
    await expect(reWireSel).toBeVisible();
    await expect(reWireSel).toHaveValue('env.clusterName');
  });

  test('inspector wire dropdown does not show Environment optgroup if no environment keys are declared', async ({ page, request }) => {
    // Reset to pristine doc (no spec.environment)
    await resetDoc(request);

    await page.goto('/');
    await canvasSettled(page);

    await page.click('.node[data-id="dead-letter"] .node-h');
    const insp = page.locator('#insp');
    await expect(insp).toBeVisible();

    // Click "W" for deduplicationScope (unwired string field)
    await page.click('#fseg button[data-f="all"]');
    const row = insp.locator('.fld:has(span.n:text-is("deduplicationScope"))');
    await row.locator('button[data-m="w"]').click();

    const wireSel = insp.locator('select[data-wire="deduplicationScope"]');
    await expect(wireSel).toBeVisible();

    // No Environment optgroup should exist
    await expect(wireSel.locator('optgroup[label="Environment"]')).toHaveCount(0);
  });
});
