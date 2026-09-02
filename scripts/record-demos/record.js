// Records the README demo GIFs by driving the real app (no mocks) against a
// scratch engine. Frames are captured on a timer while each scenario runs,
// then quantized into a GIF — no ffmpeg needed.
//
//   node scripts/record-demos/record.js       (engine on :8086 must be up —
//   see scripts/record-demos/run.sh which handles the whole lifecycle)
const { chromium } = require('@playwright/test')
const { GIFEncoder, quantize, applyPalette } = require('gifenc')
const { PNG } = require('pngjs')
const fs = require('fs')

const BASE = 'http://127.0.0.1:8086'
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
  await new Promise(r => setTimeout(r, 800)) // let the final state breathe
  stop = true
  await capture
  const gif = GIFEncoder()
  for (const buf of frames) {
    const png = PNG.sync.read(buf)
    const scale = png.width / W // retina screenshots come back 2x
    let rgba = png.data
    let w = png.width, h = png.height
    if (scale > 1) { // nearest-neighbour downscale to declared size
      const outBuf = Buffer.alloc(W * H * 4)
      for (let y = 0; y < H; y++)
        for (let x = 0; x < W; x++) {
          const si = ((y * scale | 0) * w + (x * scale | 0)) * 4
          const di = (y * W + x) * 4
          outBuf[di] = rgba[si]; outBuf[di+1] = rgba[si+1]
          outBuf[di+2] = rgba[si+2]; outBuf[di+3] = 255
        }
      rgba = outBuf; w = W; h = H
    }
    const palette = quantize(rgba, 256)
    const index = applyPalette(rgba, palette)
    gif.writeFrame(index, w, h, { palette, delay: 1000 / FPS })
  }
  gif.finish()
  fs.writeFileSync(`${OUT}/${name}.gif`, Buffer.from(gif.bytes()))
  console.log(name, frames.length, 'frames,', (fs.statSync(`${OUT}/${name}.gif`).size / 1e6).toFixed(1) + 'MB')
}

async function main() {
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: W, height: H }, colorScheme: 'dark' })

  // ---- hero: the IRSA dependency tree + a real render check -------------
  await page.goto(BASE)
  await page.waitForSelector('.node[data-id="sa"]')
  await record(page, 'demo', async () => {
    await page.hover('.node[data-id="role"] .port.status')
    await page.waitForTimeout(700)
    await page.click('.node[data-id="sa"] .node-h')
    await page.waitForTimeout(900)
    await page.click('#validateBtn')
    await page.waitForSelector('#valid:has-text("render ok")', { timeout: 90000 })
    await page.waitForTimeout(400)
  })

  // ---- build from scratch: drop, wire, watch the YAML -------------------
  await record(page, 'compose', async () => {
    await page.click('#rtabs button[data-r="kinds"]')
    await page.waitForTimeout(400)
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

  // ---- providers: catalogue search ----------------------------------------
  await record(page, 'catalogue', async () => {
    await page.click('#rtabs button[data-r="src"]')
    await page.waitForTimeout(500)
    await page.fill('#cat-search', 'rds')
    await page.waitForTimeout(1200)
    await page.fill('#cat-search', 'gcp-storage')
    await page.waitForTimeout(1400)
  })

  await browser.close()
}
main().catch(e => { console.error(e); process.exit(1) })
