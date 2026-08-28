// The XR node: the composite resource itself, represented as its XRD's
// parameters — one output port per parameter, since every parameter is a
// value a resource field can wire from. There is exactly one of these per
// document (it is not a store `Node`, it has no resource behind it), which
// is why it is synthesised directly from `doc.spec.xrd` rather than looked
// up by id the way ResourceNode looks up a store node.
import { Handle, Position, type NodeProps } from "@xyflow/react"
import { useBlueprint } from "../store/blueprint"

export function XRNode({ selected }: NodeProps) {
  const xrd = useBlueprint(s => s.doc?.spec.xrd)
  if (!xrd) return null
  const entries = Object.entries(xrd.parameters)

  return (
    <div
      data-testid="node-xr"
      data-selected={selected || undefined}
      tabIndex={0}
      className="cf-node cf-xr-node"
      style={{
        background: "var(--surface-2)",
        // Same visible-selection treatment as ResourceNode: border and
        // shadow shift together, tokens only.
        border: `1px solid ${selected ? "var(--wire-xrd)" : "var(--rule-2)"}`,
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
        <span style={{ fontWeight: 600 }}>{xrd.kind}</span>
        <span className="mono" style={{ marginLeft: 8, color: "var(--muted)", fontSize: 11 }}>
          XR
        </span>
      </div>
      <div className="cf-node-ports">
        {entries.map(([name, param]) => (
          <div
            key={name}
            className="cf-port-row"
            style={{ position: "relative", padding: "4px 16px 4px 10px" }}
          >
            <span className="mono">{name}</span>
            {/* role="img" for the same reason as ResourceNode's markers:
                aria-label on a bare <span> is inert ARIA. */}
            {param.required && (
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
            <Handle type="source" position={Position.Right} id={name} />
          </div>
        ))}
      </div>
    </div>
  )
}
