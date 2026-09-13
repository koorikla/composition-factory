const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind, guardPageErrors } = require('./helpers');

guardPageErrors();
test.use({ storageState: { cookies: [], origins: [] } });

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

test.describe('CF-471 — manifest editor, view toggle and search', () => {
  test('Manifest view is the default and nests the starter', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await expect(page.locator('#vseg button[data-view="manifest"]')).toHaveAttribute('aria-pressed', 'true');
    const view = page.locator('#insp pre.manifest-view');
    await expect(view).toBeVisible();
    await expect(view).toContainText('containers:');
    await expect(view).toContainText('image: nginx:1.27');
    await expect(page.locator('#fseg')).toBeHidden();
    await expect(page.locator('#insp .fld')).toHaveCount(0);
  });

  test('edit, apply and add a second ports item', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    let text = await ta.inputValue();
    expect(text).toContain('replicas: 2');
    text = text.replace('replicas: 2', 'replicas: 3').replace('- containerPort: 80\n', '- containerPort: 80\n            - containerPort: 9090\n');
    await ta.fill(text);
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeHidden();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      const f = r.fields;
      return { rep: f['spec.replicas'] && f['spec.replicas'].value, p1: f['spec.template.spec.containers[0].ports[1].containerPort'] && f['spec.template.spec.containers[0].ports[1].containerPort'].value };
    }).toEqual({ rep: '3', p1: '9090' });
    await expect(page.locator('#insp pre.manifest-view')).toContainText('containerPort: 9090');
  });

  test('an unknown key keeps the editor open, names the line and selects it', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await ta.fill('spec:\n  replicas: 2\n  replicaz: 4\n');
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeVisible();
    const err = page.locator('#insp .manifest-err');
    await expect(err).toBeVisible();
    await expect(err).toContainText('line 3');
    await expect(err).toContainText('spec.replicaz');
    const sel = await ta.evaluate(el => el.value.slice(el.selectionStart, el.selectionEnd));
    expect(sel).toBe('  replicaz: 4');
  });

  test('a wire survives the round trip and keyboard shortcuts work', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.locator('#insp [data-ess-row="spec.replicas"] button[data-expose]').click();
    await expect(page.locator('#insp pre.manifest-view')).toContainText('replicas: {from: params.replicas}');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    const text = await ta.inputValue();
    await ta.fill(text.replace('image: nginx:1.27', 'image: nginx:1.28'));
    await ta.press(process.platform === 'darwin' ? 'Meta+Enter' : 'Control+Enter');
    await expect(ta).toBeHidden();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return { from: r.fields['spec.replicas'].from, img: r.fields['spec.template.spec.containers[0].image'].value };
    }).toEqual({ from: 'params.replicas', img: 'nginx:1.28' });
    await page.click('#insp button[data-manifest-edit]');
    await expect(ta).toBeVisible();
    await ta.press('Escape');
    await expect(ta).toBeHidden();
  });

  test('the snippet dropdown inserts a wrapper at the cursor without touching the document', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    const before = await ta.inputValue();
    const sel = page.locator('#insp select[data-manifest-snippet]');
    const inserted = await sel.evaluate(s => s.options[1].value);
    expect(inserted).toMatch(/^\{(from: params\.|raw: ')/);
    await sel.selectOption({ index: 1 });
    await expect(ta).toHaveValue(/\{(from: params\.|raw: ')/);
    expect(await ta.inputValue()).toContain(inserted);
    await expect(sel).toHaveValue('');
    // Inserting is an editor-only act: the field map changes on Apply, not before.
    const r = await resourceNamed(request, 'deployment');
    expect(r.fields['null']).toBeUndefined();
    expect(Object.keys(r.fields).some(k => /^(null|undefined)$/.test(k))).toBe(false);
    await page.click('#insp button[data-manifest-cancel]');
    await expect(ta).toBeHidden();
    const view = page.locator('#insp pre.manifest-view');
    await expect(view).toContainText(before.split('\n')[0]);
    await expect(view).not.toContainText(inserted);
  });

  test('an open editor survives a re-render caused by an essentials edit', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    const text = await ta.inputValue();
    await ta.fill(text + '# keep me\n');
    const oldTa = await ta.elementHandle();
    const rep = page.locator('#insp [data-ess-row="spec.replicas"] input[data-v="spec.replicas"]');
    await rep.fill('5');
    await rep.press('Enter');
    // The commit re-renders the pane: wait for the textarea to be a new
    // element, so the assertion reads the re-rendered editor, not the old one.
    await page.waitForFunction(old => {
      const cur = document.querySelector('#insp textarea[data-manifest-editor]');
      return cur && cur !== old;
    }, oldTa);
    await expect(page.locator('#insp [data-ess-row="spec.replicas"] input[data-v="spec.replicas"]')).toHaveValue('5');
    await expect(ta).toBeVisible();
    await expect(ta).toHaveValue(/# keep me/);
  });

  test('Fields toggle restores the old list and search filters it', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#vseg button[data-view="fields"]');
    await expect(page.locator('#fseg')).toBeVisible();
    await page.click('#fseg button[data-f="all"]');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeGreaterThan(100);
    await page.fill('#insp-search', 'imagePullPolicy');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeLessThan(10);
    await expect(page.locator('#insp .fld').first()).toContainText('imagePullPolicy');
    await page.reload();
    await page.click('.node[data-id="deployment"] .node-h');
    await expect(page.locator('#vseg button[data-view="fields"]')).toHaveAttribute('aria-pressed', 'true');
  });

  test('search in Manifest view lists schema hits and a click opens Fields view filtered', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.fill('#insp-search', 'terminationGracePeriod');
    const hit = page.locator('#insp .search-hit').first();
    await expect(hit).toContainText('terminationGracePeriodSeconds');
    const hitPath = await hit.getAttribute('data-search-hit');
    await hit.click();
    await expect(page.locator('#vseg button[data-view="fields"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#insp-search')).toHaveValue(hitPath);
    await expect(page.locator('#insp .fld').first()).toContainText(hitPath);
    await expect(page.locator('#insp .fld').first()).toContainText('terminationGracePeriodSeconds');
  });

  test('a search hit activates from the keyboard', async ({ page }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.fill('#insp-search', 'visibilityTimeout');
    const hit = page.locator('#insp .search-hit').first();
    await expect(hit).toContainText('visibilityTimeoutSeconds');
    await hit.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('#vseg button[data-view="fields"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#insp .fld').first()).toContainText('visibilityTimeoutSeconds');
  });

  test('a search with no schema match says so in both views', async ({ page }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.fill('#insp-search', 'zzqqxx-no-such-field');
    await expect(page.locator('#insp .empty')).toContainText('No schema field matches');
    await expect(page.locator('#insp .search-hit')).toHaveCount(0);
    await page.click('#vseg button[data-view="fields"]');
    // the schema list's rows are #insp's own children; the envelope section
    // below keeps its rows, a search is not about them
    await expect(page.locator('#insp > .fld')).toHaveCount(0);
    await expect(page.locator('#insp .empty')).toContainText('No fields match');
  });

  test('applying an unchanged manifest closes the editor without a PUT or an undo step', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#undoBtn')).toBeDisabled();
    const puts = [];
    page.on('request', r => { if (r.method() === 'PUT' && /\/manifest$/.test(r.url())) puts.push(r.url()); });
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeHidden();
    await expect(page.locator('#insp pre.manifest-view')).toContainText('region:');
    await expect(page.locator('#undoBtn')).toBeDisabled();
    expect(puts).toEqual([]);
  });

  test('a failed manifest load shows the error, offers no edit and refuses an apply', async ({ page, request }) => {
    // The route is in place before the page loads so no successful GET can
    // precede it, whichever selection the drop leaves behind.
    await page.route('**/api/blueprint/resources/*/manifest', r => r.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }));
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const before = (await resourceNamed(request, 'deployment')).fields;
    const sec = page.locator('#insp .manifest-sec');
    await expect(sec.locator('.warnbar')).toContainText('boom');
    await expect(page.locator('#insp [data-manifest-edit]')).toHaveCount(0);
    await expect(page.locator('#insp pre.manifest-view')).toHaveCount(0);
    expect((await resourceNamed(request, 'deployment')).fields).toEqual(before);
    // A draft can only outlive a good load; Apply still refuses when the
    // manifest is marked unloaded, so a stale draft never replaces fields.
    await page.unroute('**/api/blueprint/resources/*/manifest');
    await page.click('.node[data-id="work-queue"] .node-h');
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    await page.evaluate(async () => { const m = await import('./js/regions/inspector/state.js'); m.state.manifestLoaded = false; });
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeVisible();
    await expect(page.locator('#insp .manifest-err')).toContainText('manifest not loaded');
    expect((await resourceNamed(request, 'deployment')).fields).toEqual(before);
  });

  test('a managed resource shows its forProvider body and a wrong-typed value is refused by the manifest schema gate', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    const view = page.locator('#insp pre.manifest-view');
    await expect(view).toContainText('region:');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    const text = await ta.inputValue();
    await ta.fill(text + 'maxMessageSize: {value: notanumber}\n');
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeVisible();
    // upjet types every numeric Queue field as `number`; the schema, not the
    // brief, decides the wording.
    await expect(page.locator('#insp .manifest-err')).toContainText('is not a valid number');
    await page.click('#insp button[data-manifest-cancel]');
    await expect(ta).toBeHidden();
    const r = await resourceNamed(request, 'work-queue');
    expect(r.fields['maxMessageSize']).toBeUndefined();
  });
});
