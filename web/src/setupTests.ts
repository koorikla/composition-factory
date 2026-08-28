import { afterEach, expect } from "vitest"
import { cleanup } from "@testing-library/react"

// @testing-library/react does not register its own afterEach when the host
// test runner's globals are not installed (we run with `globals: false` in
// vite.config.ts, matching the explicit `import { ... } from "vitest"` style
// used across this project's tests). Do it ourselves so component tests never
// leak DOM nodes between cases.
afterEach(() => {
  cleanup()
})

// This project has no @testing-library/jest-dom (not an installed
// dependency, and the canvas task's brief rules out adding new ones), but
// Canvas.test.tsx's given assertions call `.toBeInTheDocument()`. The only
// jest-dom behaviour any test here actually needs is "was this element
// found, and is it attached to the live document" — `Element.isConnected`
// gives exactly that, so a minimal custom matcher covers it without a new
// package.
declare module "vitest" {
  interface Matchers<T = any> {
    toBeInTheDocument(): T
  }
}

expect.extend({
  toBeInTheDocument(received: unknown) {
    const pass = received instanceof Element && received.isConnected
    return {
      pass,
      message: () =>
        pass
          ? "expected element not to be in the document"
          : "expected element to be in the document, but it was not found or is not attached",
    }
  },
})

// jsdom has no ResizeObserver. @xyflow/react (the canvas) needs one to
// observe the pane's and each node's dimensions on mount — without this,
// mounting <Canvas /> throws "ResizeObserver is not defined" before any
// test assertion runs. A no-op polyfill is enough: canvas tests assert on
// DOM structure and store state, never on measured pixel sizes.
if (typeof globalThis.ResizeObserver === "undefined") {
  class ResizeObserverPolyfill {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverPolyfill as unknown as typeof ResizeObserver
}

// A known jsdom/@testing-library/user-event fidelity gap, not a bug in this
// project's code: user-event's synthetic mousedown events never populate
// `event.view` (@testing-library/user-event/dist/esm/event/createEvent.js
// defines it as a getter permanently fixed to `undefined ?? null`, with no
// way for a listener to supply one). @xyflow/react's node-drag handling
// (via d3-drag) dereferences `event.view.document` unconditionally on every
// mousedown — including a plain click, since d3-drag can't know yet whether
// a click will turn into a drag — so it throws on `event.view === null`
// before ever reading position. That happens on a SEPARATE listener from
// the one that runs node selection/click handling, and jsdom invokes each
// registered listener in its own try/catch (confirmed empirically: node
// selection and the Delete key both still work correctly across this
// exception) — so the only actual effect of the exception is jsdom's own
// "reportException" turning it into a same-tick `error` event on `window`,
// which Vitest treats as a failing "unhandled error" for the whole run even
// though every assertion passes.
//
// The web platform gives listeners exactly one lever here: calling
// `event.preventDefault()` on that `error` event marks it "handled" (see
// jsdom's reportAnError), which stops it from being escalated further. This
// listener uses that lever, narrowly, only for this exact known signature —
// it does not swallow anything else.
if (typeof window !== "undefined") {
  window.addEventListener("error", event => {
    const err = event.error
    if (
      err instanceof TypeError &&
      err.message === "Cannot read properties of null (reading 'document')" &&
      typeof err.stack === "string" &&
      err.stack.includes("d3-drag")
    ) {
      event.preventDefault()
    }
  })
}
