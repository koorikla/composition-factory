// Native widgets — scrollbars, <select> dropdowns, focus rings — are painted by
// the UA from `color-scheme`, not from this app's CSS custom properties. The
// canvas themed itself entirely through tokens and never declared one, so under
// the dark theme the drawer's engine and templates selects rendered as white
// boxes with near-white text on them, and the tab strip grew a white scrollbar.
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

/** Set the theme the way the topbar button does, before the app boots. */
async function bootWithTheme(page, mode) {
  await page.addInitScript((m) => {
    try { localStorage.setItem('cf-theme', m) } catch (_) { /* private mode */ }
  }, mode)
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)
  await expect(page.locator('html')).toHaveAttribute('data-theme', mode)
}

test('the dark theme tells the UA it is dark, so native widgets follow', async ({ page }) => {
  await bootWithTheme(page, 'dark')
  const scheme = await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)
  expect(scheme).toBe('dark')
})

test('the light theme declares a light scheme even on a dark-preferring OS', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await bootWithTheme(page, 'light')
  const scheme = await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)
  expect(scheme).toBe('light')
})

test('the engine and templates selects are painted by the theme, not left UA-white', async ({ page }) => {
  await bootWithTheme(page, 'dark')

  const surface = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--surface').trim())
  expect(surface.toLowerCase()).not.toBe('#ffffff')

  for (const id of ['engineSel', 'tplSource']) {
    const el = page.locator('#' + id)
    await expect(el).toBeVisible()
    const paint = await el.evaluate((e) => {
      const cs = getComputedStyle(e)
      return { bg: cs.backgroundColor, color: cs.color }
    })
    // not the UA default white box the bug produced
    expect(paint.bg, id + ' background').not.toBe('rgb(255, 255, 255)')
    // and the text is not near-white on it either
    expect(paint.color, id + ' text').not.toBe(paint.bg)
  }
})
