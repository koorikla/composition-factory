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

  const port = 8130;
  const tempDir = fs.mkdtempSync('/tmp/cf-irsa-demo-');
  const bpPath = path.join(tempDir, 'xirsa.cf.yaml');
  fs.copyFileSync('testdata/xirsa.cf.yaml', bpPath);

  console.log('Starting cf serve with IRSA blueprint...');
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

  console.log('Launching browser in dark theme...');
  const browser = await chromium.launch({ headless: true });
  const width = 1040;
  const height = 650;
  const context = await browser.newContext({
    viewport: { width, height },
    colorScheme: 'dark'
  });

  const page = await context.newPage();

  // Set dark theme in localStorage before page load
  await page.addInitScript(() => {
    localStorage.setItem('cf-theme', 'dark');
    document.documentElement.setAttribute('data-theme', 'dark');
  });

  await page.goto(`http://127.0.0.1:${port}/`);
  await page.waitForSelector('#canvas .node');
  await page.evaluate(() => {
    document.documentElement.setAttribute('data-theme', 'dark');
  });
  await page.waitForTimeout(600);

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

  console.log('Performing demo interactions on IRSA composition...');

  // Step 1: Initial presentation of the IRSA canvas
  await page.waitForTimeout(1000);

  // Step 2: Move mouse smoothly over XRD ports to show port highlighting & fanout
  const xrdNode = page.locator('.node[data-id="xrd"]');
  const xrdBox = await xrdNode.boundingBox();
  if (xrdBox) {
    await page.mouse.move(xrdBox.x + 80, xrdBox.y + 60, { steps: 6 });
    await page.waitForTimeout(300);
    await page.mouse.move(xrdBox.x + xrdBox.width - 15, xrdBox.y + 65, { steps: 8 });
    await page.waitForTimeout(500);
  }

  // Step 3: Drag the iam-role card smoothly
  const roleNode = page.locator('.node[data-id="iam-role"]');
  const roleBox = await roleNode.boundingBox();
  if (roleBox) {
    await page.mouse.move(roleBox.x + 80, roleBox.y + 15, { steps: 8 });
    await page.mouse.down();
    await page.mouse.move(roleBox.x + 110, roleBox.y - 25, { steps: 10 });
    await page.waitForTimeout(200);
    await page.mouse.move(roleBox.x + 90, roleBox.y + 5, { steps: 8 });
    await page.mouse.up();
    await page.waitForTimeout(500);
  }

  // Step 4: Click the iam-role card to focus inspector
  await page.click('.node[data-id="iam-role"] .node-h');
  await page.waitForTimeout(800);

  // Step 5: Filter kinds palette for IAM Policy
  await page.fill('#psearch', 'policy');
  await page.waitForTimeout(800);
  await page.fill('#psearch', '');
  await page.waitForTimeout(400);

  // Step 6: Switch output drawer tabs
  const tabs = ['definition.yaml', 'providerconfigs/aws.yaml', 'composition.yaml'];
  for (const tab of tabs) {
    const tabLoc = page.locator(`.tab:has-text("${tab}")`);
    if (await tabLoc.count() > 0) {
      await tabLoc.first().click();
      await page.waitForTimeout(700);
    }
  }

  // Step 7: Click Validate / Generate
  const genBtn = page.locator('#btn-gen');
  if (await genBtn.isVisible()) {
    await genBtn.click();
    await page.waitForTimeout(1000);
  }

  capturing = false;
  await capturePromise;

  await browser.close();
  server.kill();

  console.log(`Captured ${frames.length} frames. Encoding dark-theme GIF...`);

  const encoder = new GIFEncoder(width, height, 'neuquant', true);
  const outPath = path.join(__dirname, '../docs/screenshots/demo.gif');
  const writeStream = fs.createWriteStream(outPath);
  encoder.createReadStream().pipe(writeStream);
  encoder.start();
  encoder.setRepeat(0); // 0 = loop forever
  encoder.setDelay(captureInterval);
  encoder.setQuality(10);

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

  console.log(`\nDark theme IRSA GIF saved to ${outPath} (${(fs.statSync(outPath).size / 1024 / 1024).toFixed(2)} MB)`);
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
