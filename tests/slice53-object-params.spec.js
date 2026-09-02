const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

async function param(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.xrd.parameters || {})[name];
}

test.describe('OpenAPI-grade object parameters', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('the inspector edits an object parameter as a member tree, nested included', async ({ page, request }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    // empty selection → the XRD parameter editor
    await page.click('#addParamBtn');
    await expect(page.locator('[data-pt="newParam"]')).toBeVisible();
    await page.selectOption('[data-pt="newParam"]', 'object');

    // add a first member
    await page.click('[data-madd="newParam|"]');
    await expect.poll(async () => {
      const p = await param(request, 'newParam');
      return p && p.properties && Object.keys(p.properties).length;
    }).toBe(1);

    // rename it
    const first = Object.keys((await param(request, 'newParam')).properties)[0];
    await page.fill('[data-mname="newParam|' + first + '"]', 'cidr');
    await page.locator('[data-mname="newParam|' + first + '"]').blur();
    await expect.poll(async () => {
      const p = await param(request, 'newParam');
      return p.properties.cidr ? 'cidr' : Object.keys(p.properties)[0];
    }).toBe('cidr');

    // make it an object and nest a member inside it
    await page.selectOption('[data-mtype="newParam|cidr"]', 'object');
    await page.click('[data-madd="newParam|cidr"]');
    await expect.poll(async () => {
      const p = await param(request, 'newParam');
      const c = p.properties.cidr;
      return c && c.properties && Object.keys(c.properties).length;
    }).toBe(1);

    // required + delete on the nested member
    const nested = Object.keys((await param(request, 'newParam')).properties.cidr.properties)[0];
    await page.check('[data-mreq="newParam|cidr.' + nested + '"]');
    await expect.poll(async () => {
      const p = await param(request, 'newParam');
      return p.properties.cidr.properties[nested].required;
    }).toBe(true);

    await page.click('[data-mdel="newParam|cidr.' + nested + '"]');
    await expect.poll(async () => {
      const p = await param(request, 'newParam');
      return Object.keys(p.properties.cidr.properties || {}).length;
    }).toBe(0);
  });

  test('updating an object parameter no longer drops its members', async ({ page, request }) => {
    // seed an object param with members straight through the API
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.xrd.parameters.net = {
      type: 'object', required: false,
      properties: { cidr: { type: 'string', required: true } },
    };
    const put = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(put.ok()).toBeTruthy();

    await page.goto('/');
    await expect(page.locator('[data-pr="net"]')).toBeVisible();
    await page.check('[data-pr="net"]'); // the paramFrom bug used to eat properties here
    await expect.poll(async () => (await param(request, 'net')).required).toBe(true);
    const p = await param(request, 'net');
    expect(p.properties && p.properties.cidr && p.properties.cidr.type).toBe('string');
  });
});
