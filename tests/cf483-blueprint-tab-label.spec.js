const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
const pristine = require('./fixtures/pristine-doc.json')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('blueprint tab in output region displays served filename rather than metadata.name', async ({ page }) => {
  await page.goto('/')

  // In e2e harness, cf serve runs with --blueprint <scratchDir>/doc.cf.yaml,
  // while the document metadata.name is "xnotify".
  // The tab button in the output region must display the served file's name ("doc.cf.yaml"),
  // not <doc.metadata.name>.cf.yaml ("xnotify.cf.yaml").
  const bpTab = page.locator('#tabs button[data-t="bp"]')
  await expect(bpTab).toBeVisible()
  await expect(bpTab).toHaveText('doc.cf.yaml')
  await expect(bpTab).not.toHaveText('xnotify.cf.yaml')
})

test('blueprint tab retains served filename when metadata.name differs from filename', async ({ request, page }) => {
  // Reset with metadata.name: "custom-blueprint"
  const customDoc = { ...pristine, metadata: { name: 'custom-blueprint' } }
  await resetDoc(request, customDoc)

  await page.goto('/')
  const bpTab = page.locator('#tabs button[data-t="bp"]')
  await expect(bpTab).toBeVisible()
  await expect(bpTab).toHaveText('doc.cf.yaml')
  await expect(bpTab).not.toHaveText('custom-blueprint.cf.yaml')
})

test('topbar crumb, tree row, and output tab all consistently display served filename', async ({ page }) => {
  await page.goto('/')

  const crumb = page.locator('#crumb b')
  await expect(crumb).toBeVisible()
  await expect(crumb).toHaveText('doc.cf.yaml')

  const bpTreeItem = page.locator('#tree-root .tree-item[data-t="bp"] .tree-item-name')
  await expect(bpTreeItem).toBeVisible()
  await expect(bpTreeItem).toHaveText('doc.cf.yaml')

  const bpTab = page.locator('#tabs button[data-t="bp"]')
  await expect(bpTab).toBeVisible()
  await expect(bpTab).toHaveText('doc.cf.yaml')
})
