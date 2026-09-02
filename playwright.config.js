// BDD harness for the canvas. webServer boots an isolated cf serve instance
// on 127.0.0.1:8081 with a scratch blueprint, leaving any human server on 8080 untouched.
const { defineConfig } = require('@playwright/test')
module.exports = defineConfig({
  testDir: 'tests',
  workers: 1,  // one live engine — parallel workers corrupt each other's doc state
  timeout: 15000,
  use: { baseURL: 'http://127.0.0.1:8081' },
  webServer: {
    // the suite's own engine: scratch blueprint + out dir, port 8081 —
    // the human's server on 8080 is never touched by tests.
    command: 'sh -c "make build && rm -rf .testrun && mkdir -p .testrun/out && cp tests/fixtures/pristine-doc.json .testrun/doc.cf.yaml && ./bin/cf serve --addr 127.0.0.1:8081 --blueprint .testrun/doc.cf.yaml --out .testrun/out --lock .testrun/.cf.lock"',
    url: 'http://127.0.0.1:8081/healthz',
    // a crashed run strands the engine on 8081 and blocks every later run;
    // reusing is safe — the engine is ours by construction (scratch doc).
    reuseExistingServer: true,
  },
})
