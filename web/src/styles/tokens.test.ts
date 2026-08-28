import { describe, it, expect } from "vitest"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"

const css = readFileSync(resolve(__dirname, "tokens.css"), "utf8")

describe("token system", () => {
  it("defines the complete light palette on bare :root", () => {
    const root = css.slice(css.indexOf(":root{") >= 0 ? css.indexOf(":root{") : css.indexOf(":root {"))
    for (const t of ["--ground", "--surface", "--ink", "--rule",
                     "--wire-xrd", "--wire-status", "--wire-ref", "--shared"]) {
      expect(root.includes(t), `${t} missing from :root`).toBe(true)
    }
  })

  it("guards the dark media query so an explicit light choice wins", () => {
    expect(css).toContain('prefers-color-scheme: dark')
    expect(css).toContain(':root:not([data-theme="light"])')
  })

  it("redefines tokens under an explicit dark stamp so the toggle wins both ways", () => {
    expect(css).toContain(':root[data-theme="dark"]')
  })

  it("never defines a colour ONLY inside a theme block", () => {
    // every --wire-* / --shared token must appear in the bare :root block too
    const bare = css.slice(0, css.indexOf("@media"))
    for (const t of ["--wire-xrd", "--wire-status", "--wire-ref", "--shared"]) {
      expect(bare.includes(t), `${t} defined only inside a theme block — it would be undefined in the un-stamped state`).toBe(true)
    }
  })

  it("keeps the native-ref wire distinguishable without colour", () => {
    // rust and gold sit ~35 degrees apart in hue; the dash is the real discriminator
    expect(css).toMatch(/--wire-ref-dash\s*:/)
  })
})
