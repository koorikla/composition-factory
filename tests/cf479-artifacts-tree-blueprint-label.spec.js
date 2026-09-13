const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
const pristine = require('./fixtures/pristine-doc.json')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('artifacts tree row displays the served blueprint filename rather than metadata.name', async ({ page }) => {
  await page.goto('/')

  // In e2e harness, cf serve runs with --blueprint <scratchDir>/doc.cf.yaml,
  // while the document metadata.name is "xnotify".
  // The artifacts tree row for the blueprint must display the served file's name ("doc.cf.yaml"),
  // not <doc.metadata.name>.cf.yaml ("xnotify.cf.yaml").
  const bpTreeItem = page.locator('#tree-root .tree-item[data-t="bp"]')
  await expect(bpTreeItem).toBeVisible()

  const bpName = bpTreeItem.locator('.tree-item-name')
  await expect(bpName).toHaveText('doc.cf.yaml')
  await expect(bpName).not.toHaveText('xnotify.cf.yaml')
})

test('artifacts tree row retains served filename when metadata.name differs from filename', async ({ request, page }) => {
  // Reset with metadata.name: "custom-blueprint"
  const customDoc = { ...pristine, metadata: { name: 'custom-blueprint' } }
  await resetDoc(request, customDoc)

  await page.goto('/')
  const bpTreeItem = page.locator('#tree-root .tree-item[data-t="bp"]')
  await expect(bpTreeItem).toBeVisible()

  const bpName = bpTreeItem.locator('.tree-item-name')
  await expect(bpName).toHaveText('doc.cf.yaml')
  await expect(bpName).not.toHaveText('custom-blueprint.cf.yaml')
})
