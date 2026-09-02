// Slice 56 — interactive file & artifact tree explorer in the output drawer:
// hierarchical tree view on the left of the text editor, clickable files with
// icons and line counts, breadcrumbs bar, copy button, and sidebar collapse toggle.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
})

test('tree explorer displays categorized artifacts and switches editor on click', async ({ page }) => {
  // Tree header count badge
  const badge = page.locator('#tree-files-count')
  await expect(badge).toBeVisible()
  await expect(badge).toContainText('file')

  // Tree items exist
  const bpItem = page.locator('#tree-root .tree-item[data-t="bp"]')
  await expect(bpItem).toBeVisible()
  await expect(bpItem).toContainText('xnotify.cf.yaml')

  const compItem = page.locator('#tree-root .tree-item[data-t="comp"]')
  await expect(compItem).toBeVisible()
  await expect(compItem).toContainText('composition.yaml')

  const xrdItem = page.locator('#tree-root .tree-item[data-t="xrd"]')
  await expect(xrdItem).toBeVisible()
  await expect(xrdItem).toContainText('definition.yaml')

  // Click on definition in tree
  await xrdItem.click()
  await expect(xrdItem).toHaveClass(/active/)
  await expect(page.locator('#tabs button[data-t="xrd"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('#code')).toContainText('kind: CompositeResourceDefinition')
  await expect(page.locator('#eb-path')).toContainText('xrds/')

  // Click on blueprint in tree
  await bpItem.click()
  await expect(bpItem).toHaveClass(/active/)
  await expect(page.locator('#tabs button[data-t="bp"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('#code')).toContainText('kind: Blueprint')
  await expect(page.locator('#eb-path')).toContainText('xnotify.cf.yaml')
  await expect(page.locator('#code-edit')).toBeVisible()
})

test('tree groups expand and collapse on click', async ({ page }) => {
  const group = page.locator('.tree-group[data-gid="engine"]')
  const title = group.locator('.tree-group-title')
  await expect(group).not.toHaveAttribute('data-collapsed')
  await expect(group.locator('.tree-item[data-t="comp"]')).toBeVisible()

  // Click title to collapse
  await title.click()
  await expect(group).toHaveAttribute('data-collapsed', '')
  await expect(group.locator('.tree-item[data-t="comp"]')).toBeHidden()

  // Click title again to expand
  await title.click()
  await expect(group).not.toHaveAttribute('data-collapsed')
  await expect(group.locator('.tree-item[data-t="comp"]')).toBeVisible()
})

test('tree toggle button collapses and expands the explorer sidebar', async ({ page }) => {
  const drawer = page.locator('#region-output')
  const treeBtn = page.locator('#treeToggleBtn')
  const tree = page.locator('#drawer-tree')

  await expect(tree).toBeVisible()
  await treeBtn.click()
  await expect(drawer).toHaveAttribute('data-tree-collapsed', '')
  await expect(tree).toBeHidden()

  await treeBtn.click()
  await expect(drawer).not.toHaveAttribute('data-tree-collapsed')
  await expect(tree).toBeVisible()
})

test('FileSystem template export mode populates Templates and Runtime tree groups', async ({ page }) => {
  await page.locator('#tplSource').selectOption('FileSystem')
  await expect(page.locator('#tree-root .tree-group[data-gid="templates"]')).toBeVisible()
  await expect(page.locator('#tree-root .tree-group[data-gid="runtime"]')).toBeVisible()

  const tplItem = page.locator('#tree-root .tree-item[data-t="tpl:001-work-queue.yaml"]')
  await expect(tplItem).toBeVisible()
  await tplItem.click()
  await expect(page.locator('#code')).toContainText('kind: Queue')
  await expect(page.locator('#eb-path')).toContainText('001-work-queue.yaml')

  const rtItem = page.locator('#tree-root .tree-item[data-t="runtime"]')
  await expect(rtItem).toBeVisible()
  await rtItem.click()
  await expect(page.locator('#code')).toContainText('kind: DeploymentRuntimeConfig')
  await expect(page.locator('#eb-path')).toContainText('runtime.yaml')
})
