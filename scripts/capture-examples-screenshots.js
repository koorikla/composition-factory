const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');
const { spawn } = require('child_process');

async function main() {
  const outDir = '/Users/kaurkallas/.gemini/antigravity/brain/b55608da-c485-47b8-bdc5-3dcb143195c7';
  if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });

  const testrunDir = path.resolve('.testrun');
  if (!fs.existsSync(testrunDir)) fs.mkdirSync(testrunDir, { recursive: true });
  fs.mkdirSync(path.join(testrunDir, 'out'), { recursive: true });
  fs.copyFileSync('tests/fixtures/pristine-doc.yaml', path.join(testrunDir, 'doc.cf.yaml'));

  const srv = spawn('./bin/cf', [
    'serve',
    '--addr', '127.0.0.1:8081',
    '--blueprint', path.join(testrunDir, 'doc.cf.yaml'),
    '--out', path.join(testrunDir, 'out'),
    '--lock', path.join(testrunDir, '.cf.lock')
  ]);

  srv.stdout.on('data', d => process.stdout.write(d));
  srv.stderr.on('data', d => process.stderr.write(d));

  // Wait for healthz
  for (let i = 0; i < 30; i++) {
    try {
      const res = await fetch('http://127.0.0.1:8081/healthz');
      if (res.ok) break;
    } catch (_) {}
    await new Promise(r => setTimeout(r, 200));
  }

  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  await page.goto('http://127.0.0.1:8081');
  await page.waitForSelector('.node[data-id="work-queue"]');

  // 1. Open Examples Modal
  await page.click('#examplesBtn');
  await page.waitForSelector('#examplesOverlay:not([hidden])');
  await page.waitForSelector('.example-card[data-id="irsa"]');
  await page.waitForTimeout(400); // let transition settle

  await page.screenshot({ path: path.join(outDir, 'examples-modal.png') });
  console.log('Saved examples-modal.png');

  // 2. Load IRSA Blueprint
  await page.click('.example-card[data-id="irsa"] button[data-load-id="irsa"]');
  await page.waitForSelector('.node[data-id="role"]');
  await page.waitForTimeout(500);

  await page.screenshot({ path: path.join(outDir, 'example-irsa-loaded.png') });
  console.log('Saved example-irsa-loaded.png');

  // 3. Open Modal and Load RDS Blueprint
  await page.click('#examplesBtn');
  await page.waitForSelector('#examplesOverlay:not([hidden])');
  await page.waitForTimeout(400);
  await page.click('.example-card[data-id="rds-postgres"] button[data-load-id="rds-postgres"]');
  await page.waitForSelector('.node[data-id="db-instance"]');
  await page.waitForTimeout(500);

  await page.screenshot({ path: path.join(outDir, 'example-rds-loaded.png') });
  console.log('Saved example-rds-loaded.png');

  await browser.close();
  srv.kill('SIGTERM');
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
