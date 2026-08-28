// A managed-resource node: kind, resource name, apiVersion, and one port
// per field that is either already bound (wired from an XRD parameter) or
// required by the provider — never all of a resource's fields (a Queue
// alone carries 18; showing every one on the canvas at once would drown the
// two or three that actually matter for a given composition).
import { useContext, useEffect, useState } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { useBlueprint } from "../store/blueprint"
import { api } from "../api/contract"
import type { Field } from "../api/contract"
import { FieldsCacheContext } from "./fieldsCache"

// Deliberately small: the whole point of "bound-or-required, not all 18" is
// that a resource's port list stays scannable. A node that genuinely has
// more than this shows a `+N more` affordance (see below) instead of either
// silently truncating or growing without bound.
const MAX_VISIBLE_PORTS = 6

export function ResourceNode({ id, selected }: NodeProps) {
  const node = useBlueprint(s => s.nodes.find(n => n.id === id))
  const resource = useBlueprint(s =>
    node ? s.doc?.spec.resources.find(r => r.name === node.name) : undefined,
  )

  const boundPaths = resource ? Object.keys(resource.fields) : []
  const boundKey = boundPaths.slice().sort().join(",")

  const [fields, setFields] = useState<Field[]>([])
  // Reports this node's fetched fields to Canvas's shared cache, keyed by
  // node id — Canvas's global `isValidConnection` (see Canvas.tsx) needs a
  // target field's `type` to judge compatibility, but that field list is
  // fetched here, lazily, per node; the cache is how that reaches Canvas
  // without lifting the fetch itself up (every node still owns its own
  // fetch — the cache is read-only from Canvas's side, write-only from
  // here).
  const reportFields = useContext(FieldsCacheContext)

  useEffect(() => {
    if (!node) return
    let cancelled = false
    const apiVersion = node.apiVersion
    const kind = node.kind
    async function run() {
      // Lazy, targeted fetches — never "give me everything": start from the
      // required-only set, then top up with any already-bound field this
      // resource has that isn't required (e.g. a field a user (or a loaded
      // document) bound to a parameter without it being provider-required).
      const required = await api.fields(apiVersion, kind, { requiredOnly: true })
      let all = required.fields
      const have = new Set(all.map(f => f.path))
      const missing = boundPaths.filter(p => !have.has(p))
      if (missing.length > 0) {
        const extras = await Promise.all(
          missing.map(p => api.fields(apiVersion, kind, { prefix: p })),
        )
        all = [...all, ...extras.flatMap(e => e.fields)]
      }
      if (!cancelled) {
        setFields(all)
        reportFields(id, all)
      }
    }
    run().catch(() => {
      // A field-fetch failure leaves the node showing zero ports rather
      // than crashing the canvas — the resource itself is still real and
      // still deletable.
    })
    return () => {
      cancelled = true
    }
    // boundKey (not boundPaths, a fresh array every render) is the real
    // dependency: re-fetch only when which paths are bound actually changes.
  }, [node?.apiVersion, node?.kind, boundKey, id, reportFields])

  if (!node || !resource) return null

  // A wired field ({from: ...}) ALWAYS ranks into the visible set — its
  // edge terminates on this port's <Handle>, and a hidden handle means the
  // wire silently doesn't render even though the document still carries the
  // assignment. Wired fields therefore claim their slots first (all of
  // them, even past MAX_VISIBLE_PORTS); unwired fields fill whatever budget
  // remains, in the fetched order, so the cap still bounds the UNWIRED tail
  // rather than truncating blindly.
  const isWired = (path: string) => Boolean(resource.fields[path]?.from)
  const wiredCount = fields.reduce((n, f) => n + (isWired(f.path) ? 1 : 0), 0)
  let unwiredBudget = Math.max(MAX_VISIBLE_PORTS - wiredCount, 0)
  const visible = fields.filter(f => {
    if (isWired(f.path)) return true
    if (unwiredBudget > 0) {
      unwiredBudget--
      return true
    }
    return false
  })
  const overflow = fields.length - visible.length

  return (
    <div
      data-testid={`resource-${id}`}
      data-selected={selected || undefined}
      tabIndex={0}
      className="cf-node cf-resource-node"
      style={{
        background: "var(--surface)",
        // Selection is a real state, not a hover nicety: border and shadow
        // both shift (tokens only) so the selected node reads at a glance.
        border: `1px solid ${selected ? "var(--wire-xrd)" : "var(--rule)"}`,
        borderRadius: 4,
        minWidth: 200,
        fontFamily: "var(--sans)",
        color: "var(--ink)",
        boxShadow: selected ? "var(--shadow-lg)" : "var(--shadow)",
      }}
    >
      <div
        className="cf-node-header"
        style={{ borderBottom: "1px solid var(--rule)", padding: "6px 10px" }}
      >
        <div>
          <span style={{ fontWeight: 600 }}>{node.kind}</span>
          <span className="mono" style={{ marginLeft: 8, color: "var(--muted)", fontSize: 11 }}>
            {resource.name}
          </span>
        </div>
        <div className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>
          {node.apiVersion}
        </div>
      </div>
      <div className="cf-node-ports">
        {visible.map(f => {
          const assignment = resource.fields[f.path]
          const bound = Boolean(assignment)
          return (
            <div
              key={f.path}
              className="cf-port-row"
              style={{ position: "relative", padding: "4px 10px 4px 16px" }}
            >
              {/* No isValidConnection here: xyflow evaluates that check
                  against the handle a drag STARTS from, not the one under
                  the pointer — every drag in this app starts from an XR
                  parameter handle, so the compatibility check lives on
                  Canvas's global isValidConnection instead (see
                  Canvas.tsx and wires.ts's typesCompatible). */}
              <Handle type="target" position={Position.Left} id={f.path} />
              <span className="mono">{f.path}</span>
              {/* role="img" makes the aria-label real: on a plain <span>,
                  aria-label is ignored (no role supports naming there) and
                  a screen reader would announce the bare glyph or nothing. */}
              {f.required && (
                <span
                  data-testid="required-marker"
                  role="img"
                  aria-label="required"
                  title="required"
                  style={{ marginLeft: 4, color: "var(--warn)" }}
                >
                  *
                </span>
              )}
              {bound && (
                <span
                  role="img"
                  aria-label="bound"
                  title="bound"
                  style={{ marginLeft: 4, color: "var(--wire-xrd)" }}
                >
                  ●
                </span>
              )}
            </div>
          )
        })}
        {overflow > 0 && (
          <div className="cf-port-overflow mono" style={{ padding: "4px 10px", color: "var(--faint)" }}>
            +{overflow} more
          </div>
        )}
      </div>
    </div>
  )
}
