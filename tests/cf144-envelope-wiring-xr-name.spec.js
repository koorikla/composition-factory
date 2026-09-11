const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, ENGINE } = require('./helpers');

test.describe('CF-144 — Wiring the connection-secret envelope to a new parameter or XR name', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('envelope wire menu offers XR name ($xr) option and selecting it writes raw: {{ $xr }} and shows it bound', async ({ page, request }) => {
    await page.goto('/');

    // Select work-queue resource card
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Locate writeConnectionSecretToRef.name in Crossplane Envelope section
    const row = page.locator('#insp .fld', { hasText: 'writeConnectionSecretToRef.name' }).first();
    await expect(row).toBeVisible();

    // Switch writeConnectionSecretToRef.name to wire mode
    await row.locator('button[data-m="w"]').click();

    const wireSelect = page.locator('#insp select[data-env-wire="writeConnectionSecretToRef.name"]');
    await expect(wireSelect).toBeVisible();

    // Defect 1 check: wireSelect must offer XR name ($xr) option
    const xrOption = wireSelect.locator('option[value="$xr"]');
    await expect(xrOption).toHaveCount(1);
    await expect(xrOption).toHaveText(/XR name \(\$xr\)/);

    // Select XR name ($xr) option
    await wireSelect.selectOption('$xr');

    // Defect 1 check: Selecting XR name writes raw: "{{ $xr }}" into the envelope entry (with value: "" and from: "")
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const res = doc.spec.resources.find(r => r.name === 'work-queue');
      return res && res.envelope && res.envelope['writeConnectionSecretToRef.name']
        ? res.envelope['writeConnectionSecretToRef.name'].raw
        : null;
    }).toBe('{{ $xr }}');

    // Inspector shows it bound to the XR name
    await expect(row).toHaveClass(/wired/);
    await expect(row.locator('.bound .src')).toHaveText('XR name ($xr)');
    await expect(row.locator('button[data-m="w"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(row.locator('[data-env-unwire="writeConnectionSecretToRef.name"]')).toBeVisible();

    // Verify generated Composition uses $xr without emitting a missing-key guard
    const genRes = await request.post(ENGINE + '/api/generate', { data: { write: false } });
    expect(genRes.ok()).toBe(true);
    const genData = await genRes.json();
    const compFile = genData.outputs.find(o => o.path.includes('compositions/'));
    expect(compFile).toBeDefined();
    expect(compFile.body).toContain('writeConnectionSecretToRef:');
    expect(compFile.body).toContain('name: {{ $xr }}');
    expect(compFile.body).not.toContain('hasKey $spec "writeConnectionSecretToRef');

    // Unwiring clears the envelope entry
    await row.locator('[data-env-unwire="writeConnectionSecretToRef.name"]').click();
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const res = doc.spec.resources.find(r => r.name === 'work-queue');
      return res && res.envelope && res.envelope['writeConnectionSecretToRef.name'];
    }).toBeUndefined();
  });

  test('creating a new parameter from required envelope field creates parameter with required: true', async ({ page, request }) => {
    await page.goto('/');

    // Select work-queue resource card
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Locate writeConnectionSecretToRef.name in Crossplane Envelope section
    const row = page.locator('#insp .fld', { hasText: 'writeConnectionSecretToRef.name' }).first();
    await expect(row).toBeVisible();

    // Switch to wire mode
    await row.locator('button[data-m="w"]').click();

    const wireSelect = page.locator('#insp select[data-env-wire="writeConnectionSecretToRef.name"]');
    await expect(wireSelect).toBeVisible();

    // Select "+ new XRD parameter…"
    await wireSelect.selectOption('__new__');

    const nameInput = page.locator('#insp input[data-npname="env:writeConnectionSecretToRef.name"]');
    await expect(nameInput).toBeVisible();
    await nameInput.fill('connectionSecretName');

    // Click Add
    const okBtn = page.locator('#insp button[data-npok="env:writeConnectionSecretToRef.name"]');
    await expect(okBtn).toBeVisible();
    await okBtn.click();

    // Defect 2 check: parameter created from required envelope field must have required: true
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      return doc.spec.xrd.parameters.connectionSecretName ? doc.spec.xrd.parameters.connectionSecretName.required : null;
    }).toBe(true);

    // Verify envelope field is wired to the new parameter
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const res = doc.spec.resources.find(r => r.name === 'work-queue');
      return res && res.envelope && res.envelope['writeConnectionSecretToRef.name']
        ? res.envelope['writeConnectionSecretToRef.name'].from
        : null;
    }).toBe('params.connectionSecretName');

    // Verify generated Composition uses $spec.connectionSecretName without missing-key guard because required: true
    const genRes2 = await request.post(ENGINE + '/api/generate', { data: { write: false } });
    expect(genRes2.ok()).toBe(true);
    const genData2 = await genRes2.json();
    const compFile2 = genData2.outputs.find(o => o.path.includes('compositions/'));
    expect(compFile2).toBeDefined();
    expect(compFile2.body).toContain('writeConnectionSecretToRef:');
    expect(compFile2.body).toContain('name: {{ $spec.connectionSecretName | quote }}');
    expect(compFile2.body).not.toContain('hasKey $spec "connectionSecretName"');
  });
});
