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

// ---------------------------------------------------------------------------
// Fix wave E1/E2: WCAG contrast, computed from the actual token values so a
// future "re-copy from the prototype" cannot silently reintroduce a failing
// pair. 4.5:1 is the AA threshold for small text, which is exactly where
// --faint (10-12px) and --warn (the '*' markers) are used.
// ---------------------------------------------------------------------------

function relativeLuminance(hex: string): number {
  const h = hex.replace("#", "")
  const channel = (i: number) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4)
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** The rule block starting at `marker` (which must include the opening
 * brace context to dodge the header comment's mention of the same
 * selectors), up to its first closing brace. */
function blockAfter(source: string, marker: string): string {
  const start = source.indexOf(marker)
  if (start < 0) throw new Error(`selector not found: ${marker}`)
  return source.slice(start, source.indexOf("}", start))
}

function tokenValue(block: string, token: string): string {
  const m = block.match(new RegExp(String.raw`${token}\s*:\s*([^;]+);`))
  if (!m) throw new Error(`token not found in block: ${token}`)
  return m[1].trim()
}

const lightBlock = bareRootBlock(css)
const darkMediaBlock = blockAfter(css, ':root:not([data-theme="light"]) {')
const darkStampBlock = blockAfter(css, ':root[data-theme="dark"] {')

describe("contrast (fix wave E1/E2)", () => {
  it("E1: dark --faint reaches AA (4.5:1) on every surface it sits on, in BOTH dark blocks", () => {
    for (const block of [darkMediaBlock, darkStampBlock]) {
      const faint = tokenValue(block, "--faint")
      for (const surface of ["--surface", "--surface-2", "--ground"]) {
        const ratio = contrast(faint, tokenValue(block, surface))
        expect(ratio, `dark --faint on ${surface} is ${ratio.toFixed(3)}:1`).toBeGreaterThanOrEqual(4.5)
      }
    }
  })

  it("E1: light --faint stays AA on the surfaces it actually sits on (unaffected by the dark fix)", () => {
    const faint = tokenValue(lightBlock, "--faint")
    for (const surface of ["--surface", "--surface-2"]) {
      const ratio = contrast(faint, tokenValue(lightBlock, surface))
      expect(ratio, `light --faint on ${surface} is ${ratio.toFixed(3)}:1`).toBeGreaterThanOrEqual(4.5)
    }
  })

  it("E2: --warn reaches AA on --surface and --surface-2 in every theme state", () => {
    for (const block of [lightBlock, darkMediaBlock, darkStampBlock]) {
      const warn = tokenValue(block, "--warn")
      for (const surface of ["--surface", "--surface-2"]) {
        const ratio = contrast(warn, tokenValue(block, surface))
        expect(ratio, `--warn on ${surface} is ${ratio.toFixed(3)}:1`).toBeGreaterThanOrEqual(4.5)
      }
    }
  })
})

describe("color-scheme (fix wave E3)", () => {
  it("declares light dark on bare :root so native UI follows the page theme", () => {
    expect(lightBlock).toMatch(/color-scheme\s*:\s*light dark\s*;/)
  })

  it("pins dark under both dark guards", () => {
    expect(darkMediaBlock).toMatch(/color-scheme\s*:\s*dark\s*;/)
    expect(darkStampBlock).toMatch(/color-scheme\s*:\s*dark\s*;/)
  })

  it("pins light under an explicit light stamp", () => {
    const lightStamp = blockAfter(css, ':root[data-theme="light"] {')
    expect(lightStamp).toMatch(/color-scheme\s*:\s*light\s*;/)
  })
})

describe("dead weight (fix wave E7)", () => {
  it("carries no --cond token — nothing uses it", () => {
    expect(css).not.toMatch(/--cond\s*:/)
  })

  it("does not fetch IBM Plex Sans Condensed from Google Fonts", () => {
    const html = readFileSync(resolve(__dirname, "../../index.html"), "utf8")
    expect(html).not.toContain("IBM+Plex+Sans+Condensed")
    // the two faces the tokens actually reference are still requested
    expect(html).toContain("IBM+Plex+Sans")
    expect(html).toContain("IBM+Plex+Mono")
  })
})
