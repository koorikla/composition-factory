// BDD harness for the canvas. webServer boots an isolated cf serve instance
// with a scratch blueprint derived per worktree, leaving any human server on 8080 untouched.
const { defineConfig } = require('@playwright/test')
const crypto = require('crypto')
const { execSync } = require('child_process')

function getWorkspace() {
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  const port = process.env.CF_E2E_PORT ? parseInt(process.env.CF_E2E_PORT, 10) : (18000 + (parseInt(hash, 16) % 10000))
  const scratchDir = `.testrun-${hash}`
  return { hash, port, scratchDir }
}

const { port, scratchDir } = getWorkspace()
const baseURL = `http://127.0.0.1:${port}`

module.exports = defineConfig({
  testDir: 'tests',
  workers: 1,  // one live engine — parallel workers corrupt each other's doc state
  timeout: 15000,
  use: {
    baseURL,
    // The inspector defaults to the Manifest view; the 28 specs that drive the
    // Required/Set/All field list get that view seeded here instead of each
    // clicking the toggle. cf471 opts out with an empty storageState.
    storageState: { cookies: [], origins: [{ origin: baseURL, localStorage: [{ name: "cf-insp-view", value: "fields" }] }] },
  },
  webServer: {
    // The e2e engine runs against its own scratch cache (${scratchDir}/cache)
    // seeded from tests/fixtures/cache so specs never skip or pass based on host
    // cache state, boots without live OCI pulls, and leaves the developer's
    // ~/Library/Caches/compositionfactory untouched.
    command: `sh -c "make build && rm -rf ${scratchDir} && mkdir -p ${scratchDir}/out && mkdir -p ${scratchDir}/cache && cp -R tests/fixtures/cache/. ${scratchDir}/cache/ && mkdir -p .testrun && cp tests/fixtures/pristine-doc.json ${scratchDir}/doc.cf.yaml && ./bin/cf serve --addr 127.0.0.1:${port} --blueprint ${scratchDir}/doc.cf.yaml --out ${scratchDir}/out --lock ${scratchDir}/.cf.lock --cache-dir ${scratchDir}/cache"`,
    url: `${baseURL}/healthz`,
    reuseExistingServer: false,
  },
})

