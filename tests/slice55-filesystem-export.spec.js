// Slice 55 — go-templating FileSystem source export: a `templates:` select in
// the output drawer flips spec.emit.templateSource; in FileSystem mode the
// composition points at a mounted folder and the drawer grows one tab per
// template file plus a runtime tab (ConfigMap(s) + DeploymentRuntimeConfig).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
})

test('selecting template files exports one file per object, a runtime doc and a FileSystem step', async ({ page, request }) => {
  const sel = page.locator('#tplSource')
  await expect(sel).toHaveValue('Inline')
  await sel.selectOption('FileSystem')

  // the doc carries the switch (full-doc PUT — the engine is the source of truth)
  await expect.poll(async () => {
    const d = await (await request.get(ENGINE + '/api/blueprint')).json()
    return d.spec.emit && d.spec.emit.templateSource
  }).toBe('FileSystem')

  // composition: FileSystem step pointing at the mount, no inline body
  await page.click('#tabs button[data-t="comp"]')
  const code = page.locator('#code')
  await expect(code).toContainText('source: FileSystem')
  await expect(code).toContainText('dirPath: /templates/xnotifies.platform.sparky.ee')
  await expect(code).not.toContainText('inline:')

  // one tab per template file, blueprint order, context first
  const ctx = page.locator('#tabs button[data-t="tpl:000-context.yaml"]')
  await expect(ctx).toHaveText('templates/000-context.yaml')
  await ctx.click()
  await expect(code).toContainText('{{- define "cf.tags" }}')
  await expect(code).toContainText('$spec := .observed.composite.resource.spec')
  const wq = page.locator('#tabs button[data-t="tpl:001-work-queue.yaml"]')
  await expect(wq).toHaveText('templates/001-work-queue.yaml')
  await wq.click()
  await expect(code).toContainText('kind: Queue')
  await expect(code).toContainText('setResourceNameAnnotation "work-queue"')
  await expect(page.locator('#tabs button[data-t="tpl:002-dead-letter.yaml"]')).toBeVisible()

  // runtime: the ConfigMap carrying the files + the DeploymentRuntimeConfig mount
  const rt = page.locator('#tabs button[data-t="runtime"]')
  await expect(rt).toHaveText('runtime.yaml')
  await rt.click()
  await expect(code).toContainText('kind: ConfigMap')
  await expect(code).toContainText('001-work-queue.yaml: |')
  await expect(code).toContainText('kind: DeploymentRuntimeConfig')
  await expect(code).toContainText('mountPath: /templates/xnotifies.platform.sparky.ee')
})

test('switching back to inline drops the file tabs; the switch is one undo step', async ({ page, request }) => {
  const sel = page.locator('#tplSource')
  await sel.selectOption('FileSystem')
  await expect(page.locator('#tabs button[data-t="runtime"]')).toBeVisible()

  await sel.selectOption('Inline')
  await expect(page.locator('#tabs button[data-t="runtime"]')).toHaveCount(0)
  await expect(page.locator('#tabs button[data-t^="tpl:"]')).toHaveCount(0)
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('source: Inline')
  await expect.poll(async () => {
    const d = await (await request.get(ENGINE + '/api/blueprint')).json()
    return (d.spec.emit && d.spec.emit.templateSource) || 'Inline'
  }).toBe('Inline')

  await page.click('#undoBtn')
  await expect(page.locator('#tabs button[data-t="runtime"]')).toBeVisible()
  await expect(sel).toHaveValue('FileSystem')
})

test('a reload keeps the select in sync with the persisted document', async ({ page }) => {
  await page.locator('#tplSource').selectOption('FileSystem')
  await expect(page.locator('#tabs button[data-t="runtime"]')).toBeVisible()
  await page.reload()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(page.locator('#tplSource')).toHaveValue('FileSystem')
  await expect(page.locator('#tabs button[data-t="runtime"]')).toBeVisible()
})
