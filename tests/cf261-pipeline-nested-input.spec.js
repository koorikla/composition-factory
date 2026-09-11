const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');
guardPageErrors();

test.describe('CF-261 Pipeline Step Nested Input Preservation', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('parseInputYAML and serializeInputYAML preserve nested spec fields across edits', async ({ page }) => {
    await page.goto('/');

    const result = await page.evaluate(async () => {
      const { parseInputYAML, serializeInputYAML, getPathVal, setPathVal } = await import('/js/regions/inspector/xrd.js');

      const rawYaml = [
        'apiVersion: cel.fn.crossplane.io/v1alpha1',
        'kind: Filter',
        'spec:',
        '  filter: "true"',
        '  extraField: "preserved"'
      ].join('\n');

      const parsed = parseInputYAML(rawYaml);
      const initialFilterVal = getPathVal(parsed, 'spec.filter');
      const initialExtraVal = getPathVal(parsed, 'spec.extraField');

      // Edit spec.filter
      setPathVal(parsed, 'spec.filter', 'false');
      const serialized = serializeInputYAML(parsed);

      // Re-parse
      const reParsed = parseInputYAML(serialized);
      const updatedFilterVal = getPathVal(reParsed, 'spec.filter');
      const updatedExtraVal = getPathVal(reParsed, 'spec.extraField');

      return {
        initialFilterVal,
        initialExtraVal,
        serialized,
        updatedFilterVal,
        updatedExtraVal,
      };
    });

    expect(result.initialFilterVal).toBe('true');
    expect(result.initialExtraVal).toBe('preserved');
    expect(result.updatedFilterVal).toBe('false');
    expect(result.updatedExtraVal).toBe('preserved');
  });

  test('UI inspector populates nested input field and editing does not wipe sibling nested fields', async ({ page, request }) => {
    const rawInput = [
      'apiVersion: cel.fn.crossplane.io/v1alpha1',
      'kind: Filter',
      'spec:',
      '  filter: "true"',
      '  extraField: "preserved"'
    ].join('\n');

    // Fetch existing doc and update its pipeline
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.pipeline = [
      {
        name: 'cel-filter',
        functionRef: 'function-cel-filter',
        package: 'xpkg.crossplane.io/crossplane-contrib/function-cel-filter:v0.2.0',
        input: rawInput
      }
    ];
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBeTruthy();

    // Mock kinds fields endpoint so the inspector renders form mode with spec.filter
    await page.route(/\/api\/kinds\/.*\/fields/, async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          fields: [
            { path: 'spec.filter', type: 'string', required: true }
          ]
        })
      });
    });

    await page.goto('/');

    // Open XRD inspector
    await page.click('.node[data-id="xrd"] .node-h');

    // Expect the input field for spec.filter to be populated with "true"
    const filterInput = page.locator('input[data-pipe-fld="0"][data-fld-path="spec.filter"]');
    await expect(filterInput).toBeVisible();
    await expect(filterInput).toHaveValue('true');

    // Edit the input field
    await filterInput.fill('false');
    await filterInput.blur();

    // Verify backend doc retains both updated filter and preserved extraField
    await expect.poll(async () => {
      const bDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return bDoc.spec.pipeline?.[0]?.input || '';
    }).toContain('filter: false');

    const finalDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    const finalInput = finalDoc.spec.pipeline[0].input;
    expect(finalInput).toContain('filter: false');
    expect(finalInput).toContain('extraField: preserved');
  });
});
