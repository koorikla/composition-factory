import { describe, it, expect } from "vitest"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"

const css = readFileSync(resolve(__dirname, "tokens.css"), "utf8")

// The bare :root block only — from `:root{` or `:root {` to its first closing
// brace. Slicing to end-of-file (an earlier draft of this test did that) scans
// into the dark-mode blocks too, where every token is redefined, so a token
// missing ONLY from the bare block would never be caught.
function bareRootBlock(source: string): string {
  const start = source.indexOf(":root{") >= 0 ? source.indexOf(":root{") : source.indexOf(":root {")
  const end = source.indexOf("}", start)
  return source.slice(start, end)
}

// Exact custom-property match, not a substring check: "--wire-ref" as a bare
// substring is satisfied by "--wire-ref-soft" or "--wire-ref-dash" even when
// "--wire-ref" itself has been deleted, which would silently defeat this test.
function definesToken(block: string, token: string): boolean {
  return new RegExp(String.raw`${token}\s*:`).test(block)
}

describe("token system", () => {
  it("defines the complete light palette on bare :root", () => {
    const root = bareRootBlock(css)
    for (const t of ["--ground", "--surface", "--ink", "--rule",
                     "--wire-xrd", "--wire-status", "--wire-ref", "--shared"]) {
      expect(definesToken(root, t), `${t} missing from :root`).toBe(true)
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
    const bare = bareRootBlock(css)
    for (const t of ["--wire-xrd", "--wire-status", "--wire-ref", "--shared"]) {
      expect(definesToken(bare, t), `${t} defined only inside a theme block — it would be undefined in the un-stamped state`).toBe(true)
    }
  })

  it("keeps the native-ref wire distinguishable without colour", () => {
    // rust and gold sit ~35 degrees apart in hue; the dash is the real discriminator
    expect(css).toMatch(/--wire-ref-dash\s*:/)
  })
})
