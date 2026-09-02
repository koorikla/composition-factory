const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

async function docName(request) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return doc.metadata && doc.metadata.name;
}

test.describe('Editable blueprint tab', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('editing the blueprint yaml applies back through the gate and is undoable', async ({ page, request }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    // the edit affordance only exists on the blueprint tab
    await expect(page.locator('#code-edit')).toBeHidden();
    await page.click('#tabs button[data-t="bp"]');
    await expect(page.locator('#code-edit')).toBeVisible();

    await page.click('#code-edit');
    const editor = page.locator('#code-editor');
    await expect(editor).toBeVisible();
    const text = await editor.inputValue();
    expect(text).toContain('name: xnotify');

    await editor.fill(text.replace('name: xnotify', 'name: xedited'));
    await page.click('#code-apply');
    await expect(editor).toBeHidden();
    await expect.poll(() => docName(request)).toBe('xedited');

    await page.click('#undoBtn');
    await expect.poll(() => docName(request)).toBe('xnotify');
  });

  test('an invalid edit surfaces the server error verbatim and leaves the doc alone', async ({ page, request }) => {
    await page.goto('/');
    await page.click('#tabs button[data-t="bp"]');
    await page.click('#code-edit');
    const editor = page.locator('#code-editor');
    await expect(editor).toBeVisible();

    await editor.fill('{not yaml: [');
    await page.click('#code-apply');
    await expect(page.locator('#import-warn')).toContainText(/yaml|parse|unmarshal/i);
    await expect(editor).toBeVisible(); // the edit is kept so it can be fixed
    expect(await docName(request)).toBe('xnotify');

    await page.click('#code-cancel');
    await expect(editor).toBeHidden();
  });
});
