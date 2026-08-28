// The canvas: the XR node, one node per resource, and the wires between
// them, all real DOM (see the brief on why @xyflow/react over a <canvas>-
// drawing library — node bodies need to be selectable, focusable text and
// form controls, which a <canvas> tag cannot give us).
import { useCallback, useEffect, useRef, useState, type JSX } from "react"
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  applyNodeChanges,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeChange,
  type Connection,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { useBlueprint } from "../store/blueprint"
import { api } from "../api/contract"
import { ResourceNode } from "./ResourceNode"
import { XRNode } from "./XRNode"
import { wireKind, wireStyle } from "./wires"

const XR_ID = "xr"

// Static: xyflow warns (and does extra work) if nodeTypes/edgeTypes change
// identity between renders, so this lives outside the component.
const nodeTypes = { resource: ResourceNode, xr: XRNode }

/** prefers-reduced-motion: none of the canvas's own transitions run for a
 * user who has asked the OS to cut motion down. Scoped to this component's
 * own classes only — it must not reach into @xyflow/react's own styling. */
const reducedMotionStyle = `
  .cf-node { transition: box-shadow 120ms ease, border-color 120ms ease; }
  @media (prefers-reduced-motion: reduce) {
    .cf-node { transition: none; }
  }
`

function CanvasInner() {
  const doc = useBlueprint(s => s.doc)
  const storeNodes = useBlueprint(s => s.nodes)
  const wires = useBlueprint(s => s.wires)
  const moveNode = useBlueprint(s => s.moveNode)
  const commitMove = useBlueprint(s => s.commitMove)
  const removeNode = useBlueprint(s => s.removeNode)
  const connect = useBlueprint(s => s.connect)

  // The XR node isn't a store `Node` (it has no resource, no name, nothing
  // to key it into the blueprint document) — its position is UI-local.
  const [xrPosition, setXrPosition] = useState({ x: 20, y: 40 })
  const [rfNodes, setRfNodes] = useState<RFNode[]>([])

  // A freshly loaded document has resources but no nodes yet (positions
  // have no home in the Blueprint schema — see store/blueprint.ts's load()).
  // Hydrate at most once per Canvas mount, guarded by a ref rather than
  // keyed on live store values: every store mutation (addNode, removeNode,
  // connect, ...) touches `doc` and — via immer's structural sharing —
  // gives it a brand-new object identity, and `nodes.length` legitimately
  // passes back through zero whenever the user deletes their last node.
  // An effect re-armed by either would misread "user just deleted
  // everything" as "a fresh, unhydrated load" and silently resurrect a
  // node the user just removed. `hydrationAttempted` fires the check
  // exactly once, as soon as a `doc` first shows up (handling the case
  // where Canvas mounts before load() resolves), and never again.
  const hydrationAttempted = useRef(false)
  useEffect(() => {
    if (!doc || hydrationAttempted.current) return
    hydrationAttempted.current = true
    const state = useBlueprint.getState()
    if (state.nodes.length > 0) return
    if (doc.spec.resources.length === 0) return
    let cancelled = false
    api
      .kinds()
      .then(kinds => {
        if (!cancelled) useBlueprint.getState().hydrateNodes(kinds)
      })
      .catch(() => {
        // Best-effort: an un-hydrated document still shows a usable (if
        // resource-less-looking) canvas via the XR node alone.
      })
    return () => {
      cancelled = true
    }
  }, [doc])

  // Rebuild xyflow's node list from the store whenever it changes,
  // preserving each node's `selected` flag — selection is UI state the
  // store has no concept of, so a store change (e.g. another node's drag)
  // must not silently clear what's currently selected.
  useEffect(() => {
    setRfNodes(prev => {
      const selected = new Map(prev.map(n => [n.id, n.selected ?? false]))
      if (!doc) return []
      const xr: RFNode = {
        id: XR_ID,
        type: "xr",
        position: xrPosition,
        data: {},
        selected: selected.get(XR_ID) ?? false,
        deletable: false,
      }
      const resourceNodes: RFNode[] = storeNodes.map(n => ({
        id: n.id,
        type: "resource",
        position: { x: n.x, y: n.y },
        data: {},
        selected: selected.get(n.id) ?? false,
      }))
      return [xr, ...resourceNodes]
    })
    // xrPosition is intentionally excluded from this effect's dependencies:
    // it is read from state that's already current when this runs, and
    // depending on it would re-trigger this whole rebuild (clobbering fresh
    // selection) on every XR drag frame instead of only on store changes.
  }, [doc, storeNodes])

  const rfEdges: RFEdge[] = doc
    ? wires.map(w => {
        const style = wireStyle(wireKind(w, doc))
        return {
          id: w.id,
          source: XR_ID,
          sourceHandle: w.fromParam,
          target: w.toNode,
          targetHandle: w.toPath,
          style: { stroke: style.stroke, strokeDasharray: style.strokeDasharray },
        }
      })
    : []

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      setRfNodes(nds => applyNodeChanges(changes, nds))
      for (const change of changes) {
        if (change.type === "position" && change.position) {
          if (change.id === XR_ID) {
            setXrPosition(change.position)
            continue
          }
          moveNode(change.id, change.position.x, change.position.y)
          if (change.dragging === false) commitMove()
        } else if (change.type === "remove" && change.id !== XR_ID) {
          removeNode(change.id)
        }
      }
    },
    [moveNode, commitMove, removeNode],
  )

  const onConnect = useCallback(
    (connection: Connection) => {
      if (connection.source !== XR_ID) return
      if (!connection.sourceHandle || !connection.targetHandle) return
      connect(connection.sourceHandle, connection.target, connection.targetHandle)
    },
    [connect],
  )

  return (
    <div style={{ width: "100%", height: "100%" }}>
      <style>{reducedMotionStyle}</style>
      <ReactFlow
        nodes={rfNodes}
        edges={rfEdges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onConnect={onConnect}
        deleteKeyCode={["Backspace", "Delete"]}
        proOptions={{ hideAttribution: true }}
      >
        <Background />
      </ReactFlow>
    </div>
  )
}

export function Canvas(): JSX.Element {
  return (
    <ReactFlowProvider>
      <CanvasInner />
    </ReactFlowProvider>
  )
}
