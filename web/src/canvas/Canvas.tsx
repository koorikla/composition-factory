// The canvas: the XR node, one node per resource, and the wires between
// them, all real DOM (see the brief on why @xyflow/react over a <canvas>-
// drawing library — node bodies need to be selectable, focusable text and
// form controls, which a <canvas> tag cannot give us).
import { useCallback, useEffect, useRef, useState, type CSSProperties, type JSX } from "react"
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  applyNodeChanges,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeChange,
  type Connection,
  type FinalConnectionState,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { useBlueprint } from "../store/blueprint"
import { api } from "../api/contract"
import type { Field } from "../api/contract"
import { ResourceNode } from "./ResourceNode"
import { XRNode } from "./XRNode"
import { wireKind, wireStyle, rejectionMessage, typesCompatible } from "./wires"
import { FieldsCacheContext } from "./fieldsCache"

const XR_ID = "xr"

// Static: xyflow warns (and does extra work) if nodeTypes/edgeTypes change
// identity between renders, so this lives outside the component.
const nodeTypes = { resource: ResourceNode, xr: XRNode }

/** prefers-reduced-motion: none of the canvas's own transitions run for a
 * user who has asked the OS to cut motion down.
 *
 * The `.react-flow__handle` rules are the visible half of "an incompatible
 * drop is refused visibly, not silently ignored" (see the global
 * `isValidConnection` below): while a wire is being dragged, xyflow itself
 * toggles `connectingto` on the handle currently under the pointer, and
 * `valid` only when that handle would accept the drop (see @xyflow/system's
 * Handle component) — colour alone, so `--err` vs. `--wire-xrd` is a hint,
 * never the only signal; the aria-live region rendered below carries the
 * same information as text. */
const canvasStyle = `
  .cf-node { transition: box-shadow 120ms ease, border-color 120ms ease; }
  .react-flow__handle { transition: outline-color 100ms ease, outline-offset 100ms ease; }
  .react-flow__handle.connectingto.valid {
    outline: 2px solid var(--wire-xrd);
    outline-offset: 2px;
  }
  .react-flow__handle.connectingto:not(.valid) {
    outline: 2px solid var(--err);
    outline-offset: 2px;
  }
  @media (prefers-reduced-motion: reduce) {
    .cf-node, .react-flow__handle { transition: none; }
  }
`

const visuallyHiddenStyle: CSSProperties = {
  position: "absolute",
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: "hidden",
  clip: "rect(0, 0, 0, 0)",
  whiteSpace: "nowrap",
  border: 0,
}

/** How long a rejection announcement stays in the aria-live region before
 * clearing itself — "transient", not a permanent status. Not an animation
 * (nothing to guard behind prefers-reduced-motion), just a timeout. */
const REJECTION_MESSAGE_MS = 4000

function CanvasInner() {
  const doc = useBlueprint(s => s.doc)
  const storeNodes = useBlueprint(s => s.nodes)
  const wires = useBlueprint(s => s.wires)
  const loadEpoch = useBlueprint(s => s.loadEpoch)
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
  // Hydrate once per load(), keyed on `loadEpoch` (bumped by load() alone —
  // see the store) rather than on `doc`'s identity or on `nodes.length`:
  // ordinary mutations (addNode, removeNode, connect, ...) also give `doc`
  // a brand-new object identity via immer's structural sharing, and
  // `nodes.length` legitimately passes back through zero whenever the user
  // deletes their last node — keying on either previously caused a real
  // bug where a post-delete mutation was misread as "a fresh, unhydrated
  // load" and silently resurrected the node the user just removed.
  // `lastHydratedEpoch` records the most recently processed epoch: it
  // re-arms exactly when load() runs again (e.g. opening a different
  // blueprint without remounting Canvas), and never on an ordinary edit
  // that leaves the epoch unchanged.
  const lastHydratedEpoch = useRef<number | null>(null)
  useEffect(() => {
    if (!doc || lastHydratedEpoch.current === loadEpoch) return
    lastHydratedEpoch.current = loadEpoch
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
  }, [doc, loadEpoch])

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

  // Every field list a ResourceNode has fetched, keyed by node id — see
  // fieldsCache.ts. A ref, not state: it's read imperatively inside
  // isValidConnection during a pointer drag, and writing to it must not
  // itself trigger a Canvas re-render every time any node's fields load.
  const fieldsByNode = useRef(new Map<string, Field[]>())
  const reportFields = useCallback<(nodeId: string, fields: Field[]) => void>((nodeId, fields) => {
    fieldsByNode.current.set(nodeId, fields)
  }, [])

  // xyflow evaluates isValidConnection against the handle a drag STARTS
  // from (see @xyflow/system's XYHandle.onPointerDown, which threads the
  // STARTING handle's own isValidConnection prop through, falling back to
  // this store-level one only when the starting handle doesn't define its
  // own) — every drag in this app starts from an XR parameter's source
  // handle, and XRNode's handles don't set a per-handle isValidConnection,
  // so this is the one place that check actually runs. Putting it on a
  // target Handle instead (an earlier draft did) is never consulted: the
  // target handle's own isValidConnection is only relevant when a drag
  // STARTS from a target handle, which nothing in this app does.
  const isValidConnection = useCallback(
    (connection: {
      source?: string | null
      sourceHandle?: string | null
      target?: string | null
      targetHandle?: string | null
    }) => {
      if (
        connection.source !== XR_ID ||
        !connection.sourceHandle ||
        !connection.target ||
        !connection.targetHandle
      ) {
        return false
      }
      const param = useBlueprint.getState().doc?.spec.xrd.parameters[connection.sourceHandle]
      if (!param) return false
      const targetFields = fieldsByNode.current.get(connection.target)
      const field = targetFields?.find(f => f.path === connection.targetHandle)
      if (!field) return false
      return typesCompatible(param.type, field.type)
    },
    [],
  )

  // The colour-independent half of "refused visibly": onConnect only ever
  // fires for a connection isValidConnection already accepted (xyflow never
  // calls it otherwise), so a rejection can only be observed at the END of
  // the drag gesture, via onConnectEnd's FinalConnectionState — isValid is
  // `false` (not `null`) precisely when the pointer was released over a
  // real handle that refused the drop, as opposed to empty canvas (which
  // xyflow reports as `null`, not a rejection at all).
  const [rejection, setRejection] = useState<string | null>(null)
  const rejectionTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onConnectEnd = useCallback((_event: MouseEvent | TouchEvent, state: FinalConnectionState) => {
    if (state.isValid !== false) return
    if (!state.fromHandle?.id || !state.toHandle?.id) return
    if (rejectionTimeout.current) clearTimeout(rejectionTimeout.current)
    setRejection(rejectionMessage(state.fromHandle.id, state.toHandle.id))
    rejectionTimeout.current = setTimeout(() => setRejection(null), REJECTION_MESSAGE_MS)
  }, [])

  useEffect(
    () => () => {
      if (rejectionTimeout.current) clearTimeout(rejectionTimeout.current)
    },
    [],
  )

  return (
    <div style={{ width: "100%", height: "100%" }}>
      <style>{canvasStyle}</style>
      {/* Screen-reader-only: the same refusal xyflow expresses visually
          (the --err ring in canvasStyle above) as text, since a hover/
          pointer-state CSS ring has no accessible-name equivalent on its
          own. role="status" + aria-live="polite" means assistive tech
          announces this without moving focus. */}
      <div role="status" aria-live="polite" style={visuallyHiddenStyle}>
        {rejection ?? ""}
      </div>
      <FieldsCacheContext.Provider value={reportFields}>
        <ReactFlow
          nodes={rfNodes}
          edges={rfEdges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          onConnect={onConnect}
          onConnectEnd={onConnectEnd}
          isValidConnection={isValidConnection}
          deleteKeyCode={["Backspace", "Delete"]}
          proOptions={{ hideAttribution: true }}
        >
          <Background />
        </ReactFlow>
      </FieldsCacheContext.Provider>
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
