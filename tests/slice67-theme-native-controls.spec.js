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
    return vars
  }
  const protoTokens = extractTokens(fs.readFileSync('web-proto/css/proto.css', 'utf8'))
  const canvasTokens = extractTokens(fs.readFileSync('docs/design/canvas-prototype.html', 'utf8'))
  expect(protoTokens).toEqual(canvasTokens)
})

test('light theme code syntax colors meet WCAG AA contrast against sunk background', async ({ page }) => {
  await bootWithTheme(page, 'light')

  const { sunk, codeBg, syntaxColors } = await page.evaluate(() => {
    const code = document.querySelector('.code') || document.getElementById('code') || document.body
    const sunk = getComputedStyle(document.documentElement).getPropertyValue('--sunk').trim()
    const codeBg = getComputedStyle(code).backgroundColor

    // Five syntax colors: template (.tm), shared (.sh), comment (.co/.cm), key (.k/.kk), string (.st)
    const classes = ['tm', 'sh', 'co', 'cm', 'k', 'kk', 'st']
    const colors = {}
    for (const cls of classes) {
      const span = document.createElement('span')
      span.className = cls
      code.appendChild(span)
      colors[cls] = getComputedStyle(span).color
      span.remove()
    }
    return { sunk, codeBg, syntaxColors: colors }
  })

  // All five syntax colors in light theme must achieve >= 4.5:1 contrast against --sunk (#D8E0EA)
  for (const [cls, color] of Object.entries(syntaxColors)) {
    const crSunk = contrastRatio(color, sunk)
    const crCodeBg = contrastRatio(color, codeBg)
    expect(crSunk, `.code .${cls} contrast against --sunk (${color} on ${sunk})`).toBeGreaterThanOrEqual(4.5)
    expect(crCodeBg, `.code .${cls} contrast against code background (${color} on ${codeBg})`).toBeGreaterThanOrEqual(4.5)
  }
})

test('CRON example card icon does not use rogue violet #7c3aed', async ({ page }) => {
  await page.goto('/')
  const btn = page.locator('#examplesBtn')
  await expect(btn).toBeVisible()
  await btn.click()
  const cronCard = page.locator('.example-card[data-id="k8s-cronjob"]')
  await expect(cronCard).toBeVisible()
  const icon = cronCard.locator('.example-icon')
  await expect(icon).toHaveText('CRON')
  const bg = await icon.evaluate((el) => {
    return {
      inlineBg: el.style.background,
      computedBg: getComputedStyle(el).backgroundColor,
    }
  })
  expect(bg.inlineBg, 'CRON inline background must not be rogue violet #7c3aed').not.toContain('#7c3aed')
  expect(bg.computedBg, 'CRON computed background must not be rogue violet rgb(124, 58, 237)').not.toBe('rgb(124, 58, 237)')
})

test('--dim, --accent, --pri, --panel, and --fg are defined in computed styles in both light and dark modes', async ({ page }) => {
  for (const mode of ['light', 'dark']) {
    await bootWithTheme(page, mode)
    const tokens = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement)
      return {
        dim: cs.getPropertyValue('--dim').trim(),
        accent: cs.getPropertyValue('--accent').trim(),
        pri: cs.getPropertyValue('--pri').trim(),
        panel: cs.getPropertyValue('--panel').trim(),
        fg: cs.getPropertyValue('--fg').trim(),
      }
    })
    expect(tokens.dim, `--dim should be defined in ${mode} mode`).toBeTruthy()
    expect(tokens.accent, `--accent should be defined in ${mode} mode`).toBeTruthy()
    expect(tokens.pri, `--pri should be defined in ${mode} mode`).toBeTruthy()
    expect(tokens.panel, `--panel should be defined in ${mode} mode`).toBeTruthy()
    expect(tokens.fg, `--fg should be defined in ${mode} mode`).toBeTruthy()
  }
})

test('tour card computed background and color follow theme tokens', async ({ page }) => {
  for (const mode of ['light', 'dark']) {
    await bootWithTheme(page, mode)
    const cardPaint = await page.evaluate(() => {
      const card = document.createElement('div')
      card.className = 'tour-card'
      document.body.appendChild(card)
      const cs = getComputedStyle(card)
      const paint = {
        bg: cs.backgroundColor,
        color: cs.color,
      }
      card.remove()
      return paint
    })
    if (mode === 'light') {
      // Must not fall back to dark defaults #16181d (rgb(22, 24, 29)) and #e6e6e6 (rgb(230, 230, 230))
      expect(cardPaint.bg, 'tour card background in light mode must not be dark fallback').not.toBe('rgb(22, 24, 29)')
      expect(cardPaint.color, 'tour card color in light mode must not be light fallback').not.toBe('rgb(230, 230, 230)')
      // Light surface is #FFFFFF (rgb(255, 255, 255)) and ink is #0B0F14 (rgb(11, 15, 20))
      expect(cardPaint.bg, 'tour card background in light mode matches surface').toBe('rgb(255, 255, 255)')
      expect(cardPaint.color, 'tour card color in light mode matches ink').toBe('rgb(11, 15, 20)')
    } else {
      // Dark surface is #161B22 (rgb(22, 27, 34)) and ink is #E8ECF2 (rgb(232, 236, 242))
      expect(cardPaint.bg, 'tour card background in dark mode matches dark surface').toBe('rgb(22, 27, 34)')
      expect(cardPaint.color, 'tour card color in dark mode matches dark ink').toBe('rgb(232, 236, 242)')
    }
  }
})


