// A managed-resource node: kind, resource name, apiVersion, and one port
// per field that is either already bound (wired from an XRD parameter) or
// required by the provider — never all of a resource's fields (a Queue
// alone carries 18; showing every one on the canvas at once would drown the
// two or three that actually matter for a given composition).
import { useEffect, useState } from "react"
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { useBlueprint } from "../store/blueprint"
import { api } from "../api/contract"
import type { Field, Parameter } from "../api/contract"

// Deliberately small: the whole point of "bound-or-required, not all 18" is
// that a resource's port list stays scannable. A node that genuinely has
// more than this shows a `+N more` affordance (see below) instead of either
// silently truncating or growing without bound.
const MAX_VISIBLE_PORTS = 6

/** A drop is refused (see the Handle's isValidConnection below) when the
 * XRD parameter's type and the field's type are not in the same family.
 * "integer" and "number" are treated as the same family — the API
 * contract's Parameter.type and Field.type vocabularies don't always agree
 * on which of the two spells "a number", and refusing that pairing would be
 * a false-positive rejection, not a real type error. */
function typesCompatible(paramType: string, fieldType: string): boolean {
  if (paramType === fieldType) return true
  const numeric = new Set(["integer", "number"])
  return numeric.has(paramType) && numeric.has(fieldType)
}

export function ResourceNode({ id }: NodeProps) {
  const node = useBlueprint(s => s.nodes.find(n => n.id === id))
  const resource = useBlueprint(s =>
    node ? s.doc?.spec.resources.find(r => r.name === node.name) : undefined,
  )
  const parameters = useBlueprint(s => s.doc?.spec.xrd.parameters ?? {})

  const boundPaths = resource ? Object.keys(resource.fields) : []
  const boundKey = boundPaths.slice().sort().join(",")

  const [fields, setFields] = useState<Field[]>([])

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
      if (!cancelled) setFields(all)
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
  }, [node?.apiVersion, node?.kind, boundKey])

  if (!node || !resource) return null

  const visible = fields.slice(0, MAX_VISIBLE_PORTS)
  const overflow = fields.length - visible.length

  return (
    <div
      data-testid={`resource-${id}`}
      tabIndex={0}
      className="cf-node cf-resource-node"
      style={{
        background: "var(--surface)",
        border: "1px solid var(--rule)",
        borderRadius: 4,
        minWidth: 200,
        fontFamily: "var(--sans)",
        color: "var(--ink)",
        boxShadow: "var(--shadow)",
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
              <Handle
                type="target"
                position={Position.Left}
                id={f.path}
                isValidConnection={connection => {
                  if (connection.source !== "xr" || !connection.sourceHandle) return false
                  const p: Parameter | undefined = parameters[connection.sourceHandle]
                  if (!p) return false
                  return typesCompatible(p.type, f.type)
                }}
              />
              <span className="mono">{f.path}</span>
              {f.required && (
                <span
                  data-testid="required-marker"
                  aria-label="required"
                  title="required"
                  style={{ marginLeft: 4, color: "var(--warn)" }}
                >
                  *
                </span>
              )}
              {bound && (
                <span
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
