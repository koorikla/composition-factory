import { afterEach } from "vitest"
import { cleanup } from "@testing-library/react"

// @testing-library/react does not register its own afterEach when the host
// test runner's globals are not installed (we run with `globals: false` in
// vite.config.ts, matching the explicit `import { ... } from "vitest"` style
// used across this project's tests). Do it ourselves so component tests never
// leak DOM nodes between cases.
afterEach(() => {
  cleanup()
})
