// Wire semantics: what a drawn wire MEANS, and how it is drawn. This is the
// one place that maps a wire's meaning to its paint — the canvas never
// picks a stroke colour itself, it always goes through wireStyle(wireKind(...)).
//
// A `Wire` (see store/blueprint.ts) is `{ fromParam, toNode, toPath }`. Today
// the store's connect() can only ever produce a wire sourced from an XRD
// parameter (`{ from: "params.<fromParam>" }`), but wireKind is written
// against the full four-kind vocabulary the design calls for (xrd / shared /
// status / ref) so that resource-to-resource status references and native
// provider refs — neither of which the store draws yet — slot in without
// this file changing shape later.
import type { Blueprint } from "../api/contract"
import type { Wire } from "../store/blueprint"

export type WireKind = "xrd" | "shared" | "status" | "ref"

/** How many resource fields in `doc` are bound to the XRD parameter `param`
 * via `{ from: "params.<param>" }`. A parameter fed into more than one
 * resource is "shared" — the same value fans out to multiple places, which
 * reads differently on the canvas than an ordinary one-to-one binding. */
function paramFanOut(doc: Blueprint, param: string): number {
  let count = 0
  for (const res of doc.spec.resources) {
    for (const assignment of Object.values(res.fields)) {
      if (assignment?.from === `params.${param}`) count++
    }
  }
  return count
}

/** Classifies a wire by what it connects FROM:
 *  - a key in `doc.spec.xrd.parameters`, bound to exactly one field: "xrd"
 *  - that same kind of parameter, bound to two or more fields: "shared"
 *  - a reference into another resource's status output: "status"
 *  - anything else — a native, provider-side reference that was never
 *    sourced from a composition parameter at all: "ref"
 */
export function wireKind(w: Wire, doc: Blueprint): WireKind {
  if (Object.prototype.hasOwnProperty.call(doc.spec.xrd.parameters, w.fromParam)) {
    return paramFanOut(doc, w.fromParam) > 1 ? "shared" : "xrd"
  }
  if (w.fromParam.startsWith("status.") || w.fromParam.includes(".status.")) {
    return "status"
  }
  return "ref"
}

/** Every wire kind's stroke is a CSS variable, never a literal, so the theme
 * system (see src/styles/tokens.css) stays the single source of colour.
 * Rust (--wire-ref) and gold (--shared) sit only ~35 degrees apart in hue —
 * not enough separation to trust on its own — so the native-ref wire (the
 * one kind that never came from a composition parameter) is also dashed,
 * carried over verbatim from the reviewed prototype's inline
 * `stroke-dasharray="5 3"`. */
export function wireStyle(k: WireKind): { stroke: string; strokeDasharray?: string } {
  switch (k) {
    case "xrd":
      return { stroke: "var(--wire-xrd)" }
    case "shared":
      return { stroke: "var(--shared)" }
    case "status":
      return { stroke: "var(--wire-status)" }
    case "ref":
      return { stroke: "var(--wire-ref)", strokeDasharray: "var(--wire-ref-dash)" }
  }
}
