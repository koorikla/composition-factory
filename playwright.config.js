// BDD harness for the canvas. Requires the live API (cf serve on 127.0.0.1:8080).
// serve.py is started here; cf serve is a precondition and asserted in the spec.
const { defineConfig } = require('@playwright/test')
module.exports = defineConfig({
  testDir: 'tests',
  workers: 1,  // one live engine — parallel workers corrupt each other's doc state
  timeout: 15000,
  use: { baseURL: 'http://127.0.0.1:5180' },
  webServer: {
    command: 'python3 web-proto/serve.py',
    url: 'http://127.0.0.1:5180',
    reuseExistingServer: true,
  },
})
