import { describe, it, expect } from "vitest"
import { wireKind, wireStyle, rejectionMessage, typesCompatible } from "./wires"

const doc = {
  spec: {
    xrd: { parameters: { location: { type: "string" }, tags: { type: "object" } } },
    resources: [{ name: "main-queue", kind: "Queue", fields: {} }],
  },
} as any

describe("wire semantics", () => {
  it("classifies a wire from an XRD parameter as xrd", () => {
    expect(wireKind({ id: "w1", fromParam: "location", toNode: "n1", toPath: "region" }, doc)).toBe("xrd")
  })

  it("gives every wire kind a distinct stroke", () => {
    const kinds = ["xrd", "shared", "status", "ref"] as const
    const strokes = new Set(kinds.map(k => wireStyle(k).stroke))
    expect(strokes.size).toBe(4)
  })

  it("makes the native-ref wire dashed, so the distinction survives colour blindness", () => {
    // rust and gold sit ~35 degrees apart in hue: colour alone is not enough
    expect(wireStyle("ref").strokeDasharray).toBeTruthy()
    expect(wireStyle("xrd").strokeDasharray).toBeFalsy()
  })

  it("reads stroke from a CSS variable, never a literal", () => {
    for (const k of ["xrd", "shared", "status", "ref"] as const) {
      expect(wireStyle(k).stroke).toMatch(/^var\(--/)
    }
  })
})

describe("rejection message (fix round 1, Finding 2)", () => {
  it("names both the source parameter and the target field, colour-independently", () => {
    expect(rejectionMessage("providerName", "region")).toBe("providerName → region: incompatible")
  })
})

describe("type compatibility (fix round 1, Finding 2)", () => {
  it("matches identical types", () => {
    expect(typesCompatible("string", "string")).toBe(true)
  })

  it("treats integer and number as the same family", () => {
    expect(typesCompatible("integer", "number")).toBe(true)
    expect(typesCompatible("number", "integer")).toBe(true)
  })

  it("refuses genuinely different types", () => {
    expect(typesCompatible("string", "number")).toBe(false)
    expect(typesCompatible("boolean", "object")).toBe(false)
  })
})
