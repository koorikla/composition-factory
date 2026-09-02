// BDD harness for the canvas. Requires the live API (cf serve on 127.0.0.1:8080).
// serve.py is started here; cf serve is a precondition and asserted in the spec.
const { defineConfig } = require('@playwright/test')
module.exports = defineConfig({
  testDir: 'tests',
  workers: 1,  // one live engine — parallel workers corrupt each other's doc state
  timeout: 15000,
  use: { baseURL: 'http://127.0.0.1:8081' },
  webServer: {
    // the suite's own engine: scratch blueprint + out dir, port 8081 —
    // the human's server on 8080 is never touched by tests.
    command: 'sh -c "rm -rf .testrun && mkdir -p .testrun/out && cp tests/fixtures/pristine-doc.yaml .testrun/doc.cf.yaml && ./bin/cf serve --addr 127.0.0.1:8081 --blueprint .testrun/doc.cf.yaml --out .testrun/out --lock .testrun/.cf.lock"',
    url: 'http://127.0.0.1:8081/healthz',
    reuseExistingServer: false,
  },
})
