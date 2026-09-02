// Every test starts from the pristine xnotify doc — and the suite runs
// against ITS OWN engine started by playwright.config's
// webServer with a scratch blueprint, never the one a human is using on
// 8080. Isolation is structural, not manners.
const { test, expect } = require('@playwright/test')
const pristine = require('./fixtures/pristine-doc.json')
const crypto = require('crypto')
const fs = require('fs')
const { execSync } = require('child_process')

if (!fs.existsSync('.testrun')) {
  fs.mkdirSync('.testrun', { recursive: true })
}

function getEngineURL() {
  if (process.env.CF_E2E_PORT) {
    return `http://127.0.0.1:${process.env.CF_E2E_PORT}`
  }
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  const port = 18000 + (parseInt(hash, 16) % 10000)
  return `http://127.0.0.1:${port}`
}

const ENGINE = getEngineURL()

async function resetDoc(request) {
  let api
  try { api = await request.get(ENGINE + '/api/kinds') } catch (e) { api = null }
  test.skip(!api || !api.ok(), `cf serve is not running on ${ENGINE}`)

  const r = await request.put(ENGINE + '/api/blueprint', { data: pristine })
  if (!r.ok()) throw new Error('resetDoc failed: ' + (await r.text()))
}

module.exports = { resetDoc, ENGINE }

// Any uncaught error in the page fails the test that produced it. Without
// this, a broken module import or a ReferenceError inside an event handler
// leaves the suite green while the canvas is dead — which is exactly how a
// duplicate `import { esc }` and a missing `startDrag` import shipped.
function guardPageErrors() {
  const errors = []
  test.beforeEach(async ({ page }) => {
    errors.length = 0
    page.on('pageerror', (e) => errors.push(e))
  })
  test.afterEach(async () => {
    if (errors.length) {
      throw new Error('uncaught page error(s):\n' + errors.map((e) => '  ' + (e.message || String(e))).join('\n'))
    }
  })
}

module.exports.guardPageErrors = guardPageErrors

/* ---------- canvas geometry: wait on state, never on the clock ----------

   The canvas settles in several passes: cards render, get measured and laid
   out from their own offsetWidth/offsetHeight, get re-laid when the webfonts
   land, and drawWires() redraws on the next animation frame. Anything read
   before that lands is wrong in one of two ways, and both have failed in CI
   while passing on macOS:

     - locator.boundingBox() only waits for the element to be ATTACHED. It
       then asks the browser for a box model, which is null until layout has
       actually run — so an attached-but-unlaid port yields null, and the
       arithmetic on it throws "Cannot read properties of null".
     - A wire whose endpoints happen to land on the same y is a perfectly
       horizontal path, so its client rect is zero-HEIGHT. Playwright calls
       that invisible and refuses to click it, force:true included. Whether
       wire 0 or wire 1 lines up is a function of font metrics, which is why
       this reproduces on headless Linux and not on a Mac.
*/

/** Resolve once the canvas has stopped moving: fonts loaded, node boxes and
 *  wire paths identical across consecutive animation frames. */
async function canvasSettled(page) {
  await page.evaluate(async () => {
    delete window.__cfSettle
    if (document.fonts) await document.fonts.ready
  })
  await page.waitForFunction(() => {
    const sig =
      [...document.querySelectorAll('svg.wires path.wire-path')]
        .map((p) => p.getAttribute('d')).join('|') + '#' +
      [...document.querySelectorAll('.node')]
        .map((n) => n.style.left + ',' + n.style.top + ':' + n.offsetWidth + 'x' + n.offsetHeight).join('|')
    const s = window.__cfSettle || (window.__cfSettle = { sig: null, n: 0 })
    if (sig !== s.sig) { s.sig = sig; s.n = 0; return false }
    return ++s.n >= 3
  }, null, { polling: 'raf' })
}

/** boundingBox() that cannot return null: waits for the element to be visible
 *  (which requires a non-empty client rect) and for its box to stop moving. */
async function settledBox(locator, options) {
  const timeout = (options && options.timeout) || 10000
  await expect(locator).toBeVisible({ timeout })
  const deadline = Date.now() + timeout
  let prev = null
  for (;;) {
    const box = await locator.boundingBox()
    if (box && prev && box.x === prev.x && box.y === prev.y &&
        box.width === prev.width && box.height === prev.height) return box
    if (box && Date.now() > deadline) return box
    if (!box && Date.now() > deadline) throw new Error('element is visible but has no bounding box')
    prev = box
    await locator.page().evaluate(
      () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))))
  }
}

/** Click the nth wire the way a cursor would: on a point that actually lies on
 *  its stroke and hit-tests to it. No bounding box is involved, so a wire that
 *  happens to come out perfectly horizontal is as clickable as any other.
 *
 *  Two things make the point search fussy. Cards paint above the wire layer,
 *  so the geometric midpoint can be covered — walk along the path until a
 *  point hit-tests back to this path. And the stroke is only 2.25px wide while
 *  Chromium's context-menu hit test truncates the cursor to whole pixels: a
 *  fractional point that elementFromPoint accepts can still miss by half a
 *  stroke once truncated. So only integer points count, and we click exactly
 *  those. */
async function clickWire(page, nth, options) {
  const handle = await page.waitForFunction((i) => {
    const p = document.querySelectorAll('svg.wires path.wire-path')[i]
    if (!p) return null
    const ctm = p.getScreenCTM()
    const len = p.getTotalLength()
    if (!ctm || !len) return null
    const hits = (x, y) => document.elementFromPoint(x, y) === p
    for (let step = 0; step <= 20; step++) {
      // walk outwards from the midpoint: 0.5, 0.45, 0.55, 0.4, 0.6, ...
      const frac = 0.5 + (step % 2 ? -1 : 1) * Math.ceil(step / 2) * 0.05
      if (frac <= 0 || frac >= 1) continue
      const q = p.getPointAtLength(len * frac).matrixTransform(ctm)
      const x = Math.round(q.x), y = Math.round(q.y)
      // the click lands on whole pixels, so the whole-pixel point is the one
      // that has to hit — and its neighbours, so a 1px scroll cannot unseat it
      if (hits(x, y) && hits(x, y - 1) && hits(x, y + 1)) return { x: x, y: y }
    }
    return null
  }, nth, { polling: 'raf' })
  const { x, y } = await handle.jsonValue()
  await page.mouse.click(x, y, options)
}

module.exports.canvasSettled = canvasSettled
module.exports.settledBox = settledBox
module.exports.clickWire = clickWire
