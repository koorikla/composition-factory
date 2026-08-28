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

// jsdom computes no real layout at all: every element's offsetWidth/
// offsetHeight and getBoundingClientRect() report zero, unconditionally,
// forever. @xyflow/react gates its own node/handle registration on exactly
// that ("has this node been measured with a non-zero size yet?" — see
// @xyflow/system's updateNodeInternals: `dimensions.width && dimensions.height`
// must both be truthy before it records anything), so under unmodified
// jsdom a node's handles are NEVER registered, and any interaction that
// needs them — most importantly, dragging a wire between two handles —
// silently no-ops with no error at all (confirmed empirically: the
// ResizeObserver and DOMMatrixReadOnly polyfills below are both necessary
// but not sufficient on their own; this is the piece that was still
// missing after both). This is the standard, widely-used workaround for
// exercising real-DOM layout-dependent libraries under jsdom (the same
// technique is documented for react-beautiful-dnd, react-window, and
// others): report a fixed, non-zero size for every element. It is not an
// attempt at accurate geometry — jsdom has no layout engine for that to be
// accurate WITH — only at being non-zero and internally consistent, which
// is all any dimension-gated code path actually checks for.
if (typeof HTMLElement !== "undefined" && !("__cfFakeLayout" in HTMLElement.prototype)) {
  const FAKE_SIZE = 120
  Object.defineProperty(HTMLElement.prototype, "__cfFakeLayout", { value: true })
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get: () => FAKE_SIZE,
  })
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get: () => FAKE_SIZE,
  })
  const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect
  HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
    const real = originalGetBoundingClientRect.call(this)
    if (real.width || real.height) return real
    return {
      x: 0,
      y: 0,
      width: FAKE_SIZE,
      height: FAKE_SIZE,
      top: 0,
      left: 0,
      right: FAKE_SIZE,
      bottom: FAKE_SIZE,
      toJSON() {
        return this
      },
    } as DOMRect
  }
}

// jsdom has no ResizeObserver. @xyflow/react (the canvas) needs one for two
// things: (1) just to observe the pane's and each node's dimensions on
// mount — without this, mounting <Canvas /> throws "ResizeObserver is not
// defined" before any test assertion runs; and (2) more than that, the
// *callback actually firing* is what triggers @xyflow/react's own node-
// internals registration (its `useResizeObserver` calls `updateNodeInternals`
// only from inside the ResizeObserver callback — see
// @xyflow/react/dist/esm/index.mjs's `useResizeObserver`), which is what
// populates a node's handle registry. A pure no-op observer (observe() that
// never calls back) leaves that registry permanently empty, so
// XYHandle.onPointerDown's very first lookup (`getHandle(...)` for the
// handle the drag started FROM) always comes back empty and the whole
// connection-drag gesture silently no-ops — confirmed empirically: with a
// no-op polyfill, a real mousedown reaches the handle's native DOM listener
// (verified via a raw addEventListener probe) but xyflow's own pointer-down
// handler never attaches its mousemove/mouseup listeners at all, so nothing
// downstream (onConnectEnd, connecting* classes) ever fires.
//
// So this polyfill actually calls back, once per observe(), on a microtask
// (real ResizeObservers report asynchronously too) — with jsdom's own (zero,
// since jsdom computes no real layout) getBoundingClientRect() as the
// reported rect, which is an honest answer to "what can jsdom measure," not
// a fabricated one. Nothing here asserts on pixel geometry; it only needs
// the registration to exist.
if (typeof globalThis.ResizeObserver === "undefined") {
  class ResizeObserverPolyfill {
    #callback: ResizeObserverCallback
    #targets = new Set<Element>()
    #mutationObservers = new Map<Element, MutationObserver>()

    constructor(callback: ResizeObserverCallback) {
      this.#callback = callback
    }

    #fire(target: Element) {
      if (!this.#targets.has(target)) return
      const entry = { target, contentRect: target.getBoundingClientRect() } as ResizeObserverEntry
      this.#callback([entry], this as unknown as ResizeObserver)
    }

    observe(target: Element) {
      this.#targets.add(target)
      // Fires once immediately (real ResizeObservers report an initial
      // size right after observe() too) -- but a real browser ALSO fires
      // again whenever the observed element's rendered size actually
      // changes, which is what makes @xyflow/react's node-internals
      // registration self-correcting: a node that observes itself before
      // its content has finished loading (e.g. ResourceNode's fields are
      // fetched asynchronously, so its <Handle> elements don't exist in
      // the DOM on the very first paint) gets re-measured once that
      // content actually renders. jsdom computes no real layout, so
      // "size changed" can't be measured directly -- a MutationObserver on
      // the same subtree is the honest proxy: real content changes are
      // what cause real size changes in an actual browser, and without
      // this, a node whose handles render asynchronously would have its
      // handle registry permanently frozen at "no handles yet," which is
      // exactly the state a first, too-early callback would otherwise
      // freeze it in (confirmed empirically: without this, a
      // ResourceNode's handles were never findable by xyflow's own
      // connection-drag hit-testing at all).
      queueMicrotask(() => this.#fire(target))
      const mo = new MutationObserver(() => {
        queueMicrotask(() => this.#fire(target))
      })
      mo.observe(target, { childList: true, subtree: true })
      this.#mutationObservers.set(target, mo)
    }

    unobserve(target: Element) {
      this.#targets.delete(target)
      this.#mutationObservers.get(target)?.disconnect()
      this.#mutationObservers.delete(target)
    }

    disconnect() {
      this.#targets.clear()
      for (const mo of this.#mutationObservers.values()) mo.disconnect()
      this.#mutationObservers.clear()
    }
  }
  globalThis.ResizeObserver = ResizeObserverPolyfill as unknown as typeof ResizeObserver
}

// jsdom also has no DOMMatrixReadOnly. @xyflow/react's updateNodeInternals
// (the function the ResizeObserver callback above ultimately triggers) reads
// the canvas's current zoom back out via
// `new window.DOMMatrixReadOnly(style.transform).m22`, parsing its OWN
// viewport element's inline transform style (which it always sets as
// `translate(${x}px,${y}px) scale(${zoom})` — see toTransformString in
// @xyflow/react). This is not a general CSS <transform-list> parser (jsdom
// has none, and writing one is out of scope) — it only reads `m22`, and
// only ever needs to parse the exact `translate(...) scale(...)` shape
// xyflow itself produces, or a raw `matrix(...)` should that ever appear;
// anything unrecognized (including "none"/empty, jsdom's default) is
// treated as the identity, m22 = 1 — the correct answer at 100% zoom, which
// is what a freshly mounted canvas actually is.
if (typeof globalThis.DOMMatrixReadOnly === "undefined") {
  class DOMMatrixReadOnlyPolyfill {
    m22: number
    constructor(transform?: string) {
      let m22 = 1
      const scaleMatch = transform?.match(/scale\(\s*([-\d.]+)/)
      if (scaleMatch) m22 = Number(scaleMatch[1])
      const matrixMatch = transform?.match(/matrix\(([^)]+)\)/)
      if (matrixMatch) {
        const parts = matrixMatch[1].split(",").map(Number)
        if (parts.length >= 4 && Number.isFinite(parts[3])) m22 = parts[3]
      }
      this.m22 = Number.isFinite(m22) ? m22 : 1
    }
  }
  globalThis.DOMMatrixReadOnly = DOMMatrixReadOnlyPolyfill as unknown as typeof DOMMatrixReadOnly
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

// jsdom does not implement `document.elementFromPoint` at all (not even a
// stub — calling it throws "is not a function"). @xyflow/react's
// connection-drag hit-testing (@xyflow/system's `isValidHandle`) calls it
// unconditionally on every pointermove while dragging a wire, so any test
// that drives a real connection gesture needs it to at least exist. A
// browser's real implementation depends on live layout jsdom doesn't
// compute, so there's no faithful default here — this returns `null`
// (matching "nothing found," the same outcome a zero-layout jsdom would
// approximate anyway), and any test that needs a specific drop target
// overrides it for the duration of that one test.
if (typeof document !== "undefined" && typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null
}

// Another known jsdom fidelity gap, same shape as the d3-drag one above:
// @codemirror/view (the Output pane's read-only YAML view, Task 6)
// schedules an initial layout measurement pass via requestAnimationFrame
// right after an EditorView is constructed. That pass calls
// `Range.getClientRects()` on a text range it built internally — jsdom's
// Range implementation does not carry that method — so the callback throws
// TypeError: "textRange(...).getClientRects is not a function". This fires
// on jsdom's own animation-frame timer, asynchronously, after the
// constructing test's synchronous body (and often after the test itself)
// has already finished — confirmed empirically: every assertion in
// Output.test.tsx passes before this ever fires, and CodeMirror's line
// content is already present in the DOM without this measurement pass
// (jsdom computes no real layout for it to measure anyway — see the
// offsetWidth/getBoundingClientRect polyfill above for the same story with
// @xyflow/react). Same fix as d3-drag: mark the resulting `error` event on
// `window` handled, narrowly, only for this exact known signature.
if (typeof window !== "undefined") {
  window.addEventListener("error", event => {
    const err = event.error
    if (
      err instanceof TypeError &&
      err.message === "textRange(...).getClientRects is not a function" &&
      typeof err.stack === "string" &&
      err.stack.includes("@codemirror/view")
    ) {
      event.preventDefault()
    }
  })
}
