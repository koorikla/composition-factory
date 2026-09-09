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

function relativeLuminance(rgbStr) {
  let r, g, b
  if (rgbStr.startsWith('#')) {
    const hex = rgbStr.slice(1)
    const num = parseInt(hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex, 16)
    r = (num >> 16) & 255
    g = (num >> 8) & 255
    b = num & 255
  } else {
    const match = rgbStr.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
    if (!match) throw new Error(`Invalid color: ${rgbStr}`)
    r = parseInt(match[1], 10)
    g = parseInt(match[2], 10)
    b = parseInt(match[3], 10)
  }
  const sRGB = [r, g, b].map((v) => {
    const s = v / 255
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * sRGB[0] + 0.7152 * sRGB[1] + 0.0722 * sRGB[2]
}

function contrastRatio(color1, color2) {
  const l1 = relativeLuminance(color1)
  const l2 = relativeLuminance(color2)
  const lighter = Math.max(l1, l2)
  const darker = Math.min(l1, l2)
  return (lighter + 0.05) / (darker + 0.05)
}

test('dark theme --faint meets WCAG AA contrast on dark surfaces', async ({ page }) => {
  await bootWithTheme(page, 'dark')

  const tokens = await page.evaluate(() => {
    const cs = getComputedStyle(document.documentElement)
    return {
      faint: cs.getPropertyValue('--faint').trim(),
      surface: cs.getPropertyValue('--surface').trim(),
      surface2: cs.getPropertyValue('--surface-2').trim(),
      ground: cs.getPropertyValue('--ground').trim(),
    }
  })

  const crSurface = contrastRatio(tokens.faint, tokens.surface)
  const crSurface2 = contrastRatio(tokens.faint, tokens.surface2)
  const crGround = contrastRatio(tokens.faint, tokens.ground)

  expect(crSurface, '--faint on --surface').toBeGreaterThanOrEqual(4.5)
  expect(crSurface2, '--faint on --surface-2').toBeGreaterThanOrEqual(4.5)
  expect(crGround, '--faint on --ground').toBeGreaterThanOrEqual(4.5)
})

test('dark and light theme primary button and fan badge meet WCAG AA contrast', async ({ page }) => {
  for (const mode of ['dark', 'light']) {
    await bootWithTheme(page, mode)

    const btnPaint = await page.locator('#generateBtn').evaluate((e) => {
      const cs = getComputedStyle(e)
      return { bg: cs.backgroundColor, color: cs.color }
    })
    const btnRatio = contrastRatio(btnPaint.color, btnPaint.bg)
    expect(btnRatio, `.btn.pri contrast in ${mode} mode`).toBeGreaterThanOrEqual(4.5)

    const fanPaint = await page.evaluate(() => {
      const el = document.createElement('span')
      el.className = 'fan'
      document.body.appendChild(el)
      const cs = getComputedStyle(el)
      const paint = { bg: cs.backgroundColor, color: cs.color }
      el.remove()
      return paint
    })
    const fanRatio = contrastRatio(fanPaint.color, fanPaint.bg)
    expect(fanRatio, `.fan contrast in ${mode} mode`).toBeGreaterThanOrEqual(4.5)
  }
})

test('--shared and --warn are distinct and meet WCAG AA contrast against surfaces in both themes', async ({ page }) => {
  for (const mode of ['light', 'dark']) {
    await bootWithTheme(page, mode)

    const tokens = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement)
      return {
        shared: cs.getPropertyValue('--shared').trim(),
        warn: cs.getPropertyValue('--warn').trim(),
        surface: cs.getPropertyValue('--surface').trim(),
        surface2: cs.getPropertyValue('--surface-2').trim(),
      }
    })

    expect(tokens.shared.toLowerCase(), `--shared and --warn should be distinct in ${mode} mode`).not.toBe(tokens.warn.toLowerCase())

    const crSharedSurface = contrastRatio(tokens.shared, tokens.surface)
    const crSharedSurface2 = contrastRatio(tokens.shared, tokens.surface2)
    const crWarnSurface = contrastRatio(tokens.warn, tokens.surface)
    const crWarnSurface2 = contrastRatio(tokens.warn, tokens.surface2)

    expect(crSharedSurface, `--shared on --surface in ${mode} mode`).toBeGreaterThanOrEqual(4.5)
    expect(crSharedSurface2, `--shared on --surface-2 in ${mode} mode`).toBeGreaterThanOrEqual(4.5)
    expect(crWarnSurface, `--warn on --surface in ${mode} mode`).toBeGreaterThanOrEqual(4.5)
    expect(crWarnSurface2, `--warn on --surface-2 in ${mode} mode`).toBeGreaterThanOrEqual(4.5)
  }
})

test('token definitions in proto.css and canvas-prototype.html remain in sync', async () => {
  const fs = require('fs')
  function extractTokens(content) {
    const vars = []
    const re = /(--[\w-]+)\s*:\s*([^;}\n]+)/g
    let m
    while ((m = re.exec(content)) !== null) {
      vars.push(`${m[1]}:${m[2].trim()}`)
    }
    return vars.slice(0, 75)
  }
  const protoTokens = extractTokens(fs.readFileSync('web-proto/css/proto.css', 'utf8'))
  const canvasTokens = extractTokens(fs.readFileSync('docs/design/canvas-prototype.html', 'utf8'))
  expect(protoTokens).toEqual(canvasTokens)
})

