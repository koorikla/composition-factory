// Records all README and documentation demo GIFs and high-resolution PNGs
// by driving the live web application (against an isolated cf serve instance).
const { chromium } = require('@playwright/test')
const { GIFEncoder, quantize, applyPalette } = require('gifenc')
const { PNG } = require('pngjs')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { execSync } = require('child_process')

function getBaseURL() {
  if (process.env.CF_DEMO_PORT) {
    return `http://127.0.0.1:${process.env.CF_DEMO_PORT}`
  }
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  const port = 28000 + (parseInt(hash, 16) % 10000)
  return `http://127.0.0.1:${port}`
}

const BASE = getBaseURL()
const OUT = 'docs/screenshots'
const W = 1000, H = 640, FPS = 5

async function record(page, name, scenario) {
  const frames = []
  let stop = false
  const capture = (async () => {
    while (!stop) {
      frames.push(await page.screenshot({ type: 'png' }))
      await new Promise(r => setTimeout(r, 1000 / FPS))
    }
  })()
  await scenario()
  await new Promise(r => setTimeout(r, 800)) // let the final state settle
  stop = true
  await capture
  const gif = GIFEncoder()
  for (const buf of frames) {
    const png = PNG.sync.read(buf)
    const scale = png.width / W
    let rgba = png.data
    let w = png.width, h = png.height
    if (scale > 1) {
      const outBuf = Buffer.alloc(W * H * 4)
      for (let y = 0; y < H; y++) {
        for (let x = 0; x < W; x++) {
          const si = ((y * scale | 0) * w + (x * scale | 0)) * 4
          const di = (y * W + x) * 4
          outBuf[di] = rgba[si]
          outBuf[di + 1] = rgba[si + 1]
          outBuf[di + 2] = rgba[si + 2]
          outBuf[di + 3] = 255
        }
      }
      rgba = outBuf
      w = W
      h = H
    }
    const palette = quantize(rgba, 256)
    const index = applyPalette(rgba, palette)
    gif.writeFrame(index, w, h, { palette, delay: 1000 / FPS })
  }
  gif.finish()
  fs.mkdirSync(OUT, { recursive: true })
  fs.writeFileSync(`${OUT}/${name}.gif`, Buffer.from(gif.bytes()))
  console.log(`[GIF] ${name}.gif: ${frames.length} frames, ${(fs.statSync(`${OUT}/${name}.gif`).size / 1e6).toFixed(2)} MB`)
}

async function capturePNG(page, name) {
  fs.mkdirSync(OUT, { recursive: true })
  await page.screenshot({ path: `${OUT}/${name}.png` })
  console.log(`[PNG] ${name}.png saved`)
}

async function resetToIRSA(page) {
  const res = await page.request.put(BASE + '/api/blueprint', {
    headers: { 'Content-Type': 'application/yaml' },
    data: fs.readFileSync('testdata/irsa.cf.yaml', 'utf8'),
  })
  await page.goto(BASE, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.node[data-id="sa"]', { timeout: 15000 })
}

async function main() {
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: W, height: H }, colorScheme: 'dark' })

  console.log('--- Starting Demo GIF & Screenshot Captures ---')

  // 1. HERO DEMO (demo.gif & canvas.png)
  await resetToIRSA(page)
  await capturePNG(page, 'canvas')

  await record(page, 'demo', async () => {
    await page.hover('.node[data-id="role"] .port.status')
    await page.waitForTimeout(800)
    await page.click('.node[data-id="sa"] .node-h')
    await page.waitForTimeout(900)
    await page.hover('.node[data-id="sa"] .port')
    await page.waitForTimeout(800)
    await page.click('#tabs button[data-t="comp"]')
    await page.waitForTimeout(1000)
  })

  // 2. INSPECTOR CLOSE-UP (inspector.png)
  await page.click('.node[data-id="role"] .node-h')
  await page.waitForTimeout(400)
  await capturePNG(page, 'inspector')

  // 3. COMPOSE (compose.gif)
  await record(page, 'compose', async () => {
    await page.click('#rtabs button[data-r="kinds"]')
    await page.waitForTimeout(500)
    await page.evaluate(() => {
      const row = document.querySelector('.kind[data-kind="RolePolicy"]') ||
        document.querySelector('.kind[data-kind="Role"]')
      const cw = document.getElementById('cw')
      const r = cw.getBoundingClientRect()
      const dt = new DataTransfer()
      row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
      const at = { clientX: r.left + 420, clientY: r.top + 420 }
      cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
      cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
    })
    await page.waitForTimeout(1400)
    await page.click('#fseg button[data-f="all"]').catch(() => {})
    await page.waitForTimeout(1200)
  })

  // 4. CATALOGUE (catalogue.gif & catalogue.png)
  await record(page, 'catalogue', async () => {
    await page.click('#rtabs button[data-r="src"]')
    await page.waitForTimeout(600)
    await page.fill('#cat-search', 'rds')
    await page.waitForTimeout(1200)
    await page.fill('#cat-search', 'gcp-storage')
    await page.waitForTimeout(1400)
  })
  await capturePNG(page, 'catalogue')

  // 5. DRAG-TO-WIRE (wire.gif)
  await resetToIRSA(page)
  await page.click('#tabs button[data-t="comp"]').catch(() => {})
  await record(page, 'wire', async () => {
    await page.waitForTimeout(600)
    const dot = await page.locator('.port[data-owner="xrd"][data-path="team"] .d').boundingBox()
    const hdr = await page.locator('.node[data-id="role"] .node-h').boundingBox()
    if (dot && hdr) {
      const from = { x: dot.x + dot.width / 2, y: dot.y + dot.height / 2 }
      const to = { x: hdr.x + hdr.width / 2, y: hdr.y + hdr.height / 2 }
      await page.mouse.move(from.x, from.y)
      await page.mouse.down()
      for (let i = 1; i <= 10; i++) {
        await page.mouse.move(from.x + (to.x - from.x) * i / 10, from.y + (to.y - from.y) * i / 10)
        await page.waitForTimeout(90)
      }
      await page.mouse.up()
      await page.waitForSelector('#wire-picker')
      await page.waitForTimeout(500)
      await page.type('#wire-picker-search', 'path', { delay: 140 })
      await page.waitForTimeout(600)
      await page.click('#wire-picker .wire-picker-item')
      await page.waitForTimeout(1200)
    }
  })

  // 6. STARTUP EXAMPLES CHOOSER (examples.gif & examples-modal.png)
  await resetToIRSA(page)
  await record(page, 'examples', async () => {
    await page.click('#examplesBtn')
    await page.waitForSelector('#examplesOverlay:not([hidden])')
    await page.waitForSelector('.example-card')
    await capturePNG(page, 'examples-modal')
    await page.waitForTimeout(800)
    await page.hover('.example-card[data-id="rds-postgres"]')
    await page.waitForTimeout(800)
    await page.click('button[data-load-id="rds-postgres"]')
    await page.waitForSelector('.node[data-id="db-instance"]')
    await page.waitForTimeout(1200)
  })

  // 7. ARTIFACT & FILE TREE EXPLORER (tree.gif & tree-explorer.png)
  await record(page, 'tree', async () => {
    await page.click('#tree-root .tree-item[data-t="xrd"]')
    await page.waitForTimeout(900)
    await page.click('#tree-root .tree-item[data-t="bp"]')
    await page.waitForTimeout(900)
    await page.click('#tree-root .tree-item[data-t="comp"]')
    await page.waitForTimeout(900)
    await page.click('#code-copy-btn')
    await page.waitForTimeout(1200)
  })
  await capturePNG(page, 'tree-explorer')

  // 8. ALTERNATIVE EMITTERS - KCL ENGINE (kcl.gif & kcl-engine.png)
  await record(page, 'kcl', async () => {
    await page.click('#tabs button[data-t="comp"]')
    await page.waitForTimeout(700)
    await page.locator('#engineSel').selectOption('kcl')
    await page.waitForTimeout(1500)
    await page.click('#tabs button[data-t="fns"]')
    await page.waitForTimeout(1000)
    await page.click('#tabs button[data-t="comp"]')
    await page.waitForTimeout(1200)
  })
  await capturePNG(page, 'kcl-engine')

  // 9. FILESYSTEM EXPORT (fs-export.png)
  await page.locator('#engineSel').selectOption('go-templating')
  await page.locator('#tplSource').selectOption('FileSystem')
  await page.waitForTimeout(1000)
  await page.click('#tree-root .tree-item[data-t="tpl:000-context.yaml"]').catch(() => {})
  await page.waitForTimeout(600)
  await capturePNG(page, 'fs-export')

  // 10. FLOATING PANELS & DOCKING (floating.gif)
  await page.locator('#tplSource').selectOption('Inline')
  await page.waitForTimeout(500)
  await record(page, 'floating', async () => {
    await page.click('#drawer-float-btn')
    await page.waitForTimeout(700)
    // drag the floated drawer
    const box = await page.locator('#region-output').boundingBox()
    if (box) {
      await page.mouse.move(box.x + 100, box.y + 12)
      await page.mouse.down()
      await page.mouse.move(box.x + 150, box.y - 80)
      await page.mouse.up()
      await page.waitForTimeout(800)
    }
    // float inspector too
    const inspFloat = page.locator('#pane-float-r')
    if (await inspFloat.isVisible()) {
      await inspFloat.click({ force: true })
      await page.waitForTimeout(900)
    }
    // dock drawer back
    await page.click('#drawer-float-btn', { force: true })
    if (await inspFloat.isVisible()) {
      await inspFloat.click({ force: true })
    }
    await page.waitForTimeout(1000)
  })

  await browser.close()
  console.log('--- Finished Demo GIF & Screenshot Captures Successfully ---')
}

main().catch(e => { console.error(e); process.exit(1) })
