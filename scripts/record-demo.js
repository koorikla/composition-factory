const { chromium } = require('playwright');
const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const { PNG } = require('pngjs');
const GIFEncoder = require('gif-encoder-2');

async function main() {
  console.log('Building cf binary...');
  const build = spawn('go', ['build', '-o', 'bin/cf', './cmd/cf']);
  await new Promise((res, rej) => {
    build.on('close', code => code === 0 ? res() : rej(new Error(`build exited ${code}`)));
  });

  const port = 8123;
  const tempDir = fs.mkdtempSync('/tmp/cf-demo-');
  const bpPath = path.join(tempDir, 'xqueue.cf.yaml');
  fs.copyFileSync('testdata/xqueue.cf.yaml', bpPath);

  console.log('Starting cf serve...');
  const server = spawn('./bin/cf', [
    'serve',
    '--blueprint', bpPath,
    '--addr', `127.0.0.1:${port}`,
    '--cache-dir', path.join(process.env.HOME, 'Library/Caches/compositionfactory')
  ]);

  server.stderr.on('data', d => process.stderr.write(d));

  // Wait for server to be healthy
  for (let i = 0; i < 30; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/healthz`);
      if (res.ok) break;
    } catch {}
    await new Promise(r => setTimeout(r, 200));
  }

  console.log('Launching browser...');
  const browser = await chromium.launch({ headless: true });
  const width = 1000;
  const height = 625;
  const page = await browser.newPage({ viewport: { width, height } });

  await page.goto(`http://127.0.0.1:${port}/`);
  await page.waitForSelector('#canvas .node');
  await page.waitForTimeout(500);

  const frames = [];
  const captureInterval = 100; // ms

  let capturing = true;
  const captureLoop = async () => {
    while (capturing) {
      const buffer = await page.screenshot({ type: 'png' });
      frames.push(buffer);
      await new Promise(r => setTimeout(r, captureInterval));
    }
  };

  const capturePromise = captureLoop();

  console.log('Performing demo interactions...');

  // Step 1: Initial pause
  await page.waitForTimeout(800);

  // Step 2: Move mouse smoothly over XRD ports to show port highlighting & fanout
  const xrdNode = page.locator('.node[data-id="xrd"]');
  const xrdBox = await xrdNode.boundingBox();
  if (xrdBox) {
    await page.mouse.move(xrdBox.x + 50, xrdBox.y + 50, { steps: 5 });
    await page.waitForTimeout(400);
    await page.mouse.move(xrdBox.x + xrdBox.width - 15, xrdBox.y + 65, { steps: 8 });
    await page.waitForTimeout(600);
  }

  // Step 3: Drag the main-queue card smoothly
  const queueNode = page.locator('.node[data-id="main-queue"]');
  const queueBox = await queueNode.boundingBox();
  if (queueBox) {
    await page.mouse.move(queueBox.x + 60, queueBox.y + 15, { steps: 8 });
    await page.mouse.down();
    await page.mouse.move(queueBox.x + 120, queueBox.y - 20, { steps: 12 });
    await page.waitForTimeout(200);
    await page.mouse.move(queueBox.x + 90, queueBox.y + 10, { steps: 8 });
    await page.mouse.up();
    await page.waitForTimeout(500);
  }

  // Step 4: Click the queue node to focus inspector
  await page.click('.node[data-id="main-queue"] .node-h');
  await page.waitForTimeout(700);

  // Step 5: Filter kinds palette
  await page.fill('#psearch', 'policy');
  await page.waitForTimeout(700);
  await page.fill('#psearch', '');
  await page.waitForTimeout(400);

  // Step 6: Switch generated output drawer tabs
  const tabs = ['definition.yaml', 'providerconfigs/aws.yaml', 'composition.yaml'];
  for (const tab of tabs) {
    const tabLoc = page.locator(`.tab:has-text("${tab}")`);
    if (await tabLoc.count() > 0) {
      await tabLoc.first().click();
      await page.waitForTimeout(600);
    }
  }

  // Step 7: Click Validate / Generate
  const genBtn = page.locator('#btn-gen');
  if (await genBtn.isVisible()) {
    await genBtn.click();
    await page.waitForTimeout(800);
  }

  capturing = false;
  await capturePromise;

  await browser.close();
  server.kill();

  console.log(`Captured ${frames.length} frames. Encoding GIF...`);

  const encoder = new GIFEncoder(width, height, 'neuquant', true);
  const outPath = path.join(__dirname, '../docs/screenshots/demo.gif');
  const writeStream = fs.createWriteStream(outPath);
  encoder.createReadStream().pipe(writeStream);
  encoder.start();
  encoder.setRepeat(0); // 0 = loop forever
  encoder.setDelay(captureInterval);
  encoder.setQuality(10); // 10 is balanced quality/speed

  for (let i = 0; i < frames.length; i++) {
    const png = PNG.sync.read(frames[i]);
    encoder.addFrame(png.data);
    if (i % 10 === 0) {
      process.stdout.write(`Encoding frame ${i + 1}/${frames.length}...\r`);
    }
  }

  encoder.finish();

  await new Promise((res, rej) => {
    writeStream.on('finish', res);
    writeStream.on('error', rej);
  });

  console.log(`\nGIF saved to ${outPath} (${(fs.statSync(outPath).size / 1024 / 1024).toFixed(2)} MB)`);
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
