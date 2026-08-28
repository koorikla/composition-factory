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

  // Fix wave F4: the 'shared', 'status' and 'ref' branches were never
  // exercised — only 'xrd' was — so wireKind could break three of its four
  // classifications without a test noticing.
  it("classifies a parameter fanned out to two or more fields as shared", () => {
    const fannedOut = {
      spec: {
        xrd: { parameters: { location: { type: "string" } } },
        resources: [
          {
            name: "main-queue",
            kind: "Queue",
            fields: {
              region: { from: "params.location" },
              zone: { from: "params.location" },
            },
          },
        ],
      },
    } as any
    expect(wireKind({ id: "w1", fromParam: "location", toNode: "n1", toPath: "region" }, fannedOut)).toBe("shared")
    // Both ends of the fan-out classify identically.
    expect(wireKind({ id: "w2", fromParam: "location", toNode: "n1", toPath: "zone" }, fannedOut)).toBe("shared")
  })

  it("classifies a reference into another resource's status output as status", () => {
    // a top-level status.* path…
    expect(wireKind({ id: "w3", fromParam: "status.queueUrl", toNode: "n1", toPath: "policy" }, doc)).toBe("status")
    // …and a resource-qualified …status… path both take the status branch.
    expect(
      wireKind({ id: "w4", fromParam: "main-queue.status.atProvider.url", toNode: "n1", toPath: "policy" }, doc),
    ).toBe("status")
  })

  it("classifies a source that is neither a declared parameter nor a status path as a native ref", () => {
    expect(wireKind({ id: "w5", fromParam: "queueArnRef", toNode: "n1", toPath: "policy" }, doc)).toBe("ref")
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
