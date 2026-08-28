// The canvas: the XR node, one node per resource, and the wires between
// them, all real DOM (see the brief on why @xyflow/react over a <canvas>-
// drawing library — node bodies need to be selectable, focusable text and
// form controls, which a <canvas> tag cannot give us).
import { useCallback, useEffect, useRef, useState, type CSSProperties, type DragEvent, type JSX } from "react"
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  applyNodeChanges,
  useReactFlow,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeChange,
  type Connection,
  type FinalConnectionState,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { useBlueprint } from "../store/blueprint"
import { api } from "../api/contract"
import type { Field, Kind } from "../api/contract"
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
 * same information as text.
 *
 * The `.react-flow` block re-points xyflow's own handle theme variables
 * (--xy-handle-background-color / --xy-handle-border-color) at this app's
 * tokens: the vendor defaults are literal #1a192b/#fff, which ignore
 * tokens.css entirely and leave port dots near-invisible in the dark theme.
 * `.cf-node:focus-visible` restates tokens.css's global focus ring with a
 * wider offset so keyboard focus stays visible over a node's own border. */
const canvasStyle = `
  .cf-node { transition: box-shadow 120ms ease, border-color 120ms ease; }
  .cf-node:focus-visible { outline: 2px solid var(--wire-xrd); outline-offset: 2px; }
  .react-flow {
    --xy-handle-background-color: var(--rule-2);
    --xy-handle-border-color: var(--surface);
  }
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

// The palette (Task 4, Palette.tsx's dragKindData) serializes the WHOLE
// Kind object as JSON onto this MIME type; this is the one place on the
// canvas side that has to agree on that shape. A minimal runtime check
// (rather than trusting the cast) because dataTransfer content is, in
// principle, whatever a browser extension or a stray drag from elsewhere on
// the page put there — not just this app's own Palette.
function parseDraggedKind(dataTransfer: DataTransfer): Kind | null {
  const raw = dataTransfer.getData("application/x-compositionfactory-kind")
  if (!raw) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (
    parsed &&
    typeof parsed === "object" &&
    typeof (parsed as Kind).kind === "string" &&
    typeof (parsed as Kind).apiVersion === "string"
  ) {
    return parsed as Kind
  }
  return null
}

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

export interface CanvasProps {
  /** Reports the currently selected resource node's id (never the XR node's
   * — the XR node has no resource to inspect), or null once nothing is
   * selected. Optional and backward-compatible: every existing test in this
   * file renders `<Canvas />` with no props at all. Added so the app shell
   * (App.tsx, Task 6) can wire the Inspector to whatever the user actually
   * clicked, rather than guessing. */
  onSelectionChange?: (nodeId: string | null) => void
}

function CanvasInner({ onSelectionChange }: CanvasProps) {
  const doc = useBlueprint(s => s.doc)
  const storeNodes = useBlueprint(s => s.nodes)
  const wires = useBlueprint(s => s.wires)
  const loadEpoch = useBlueprint(s => s.loadEpoch)
  const moveNode = useBlueprint(s => s.moveNode)
  const commitMove = useBlueprint(s => s.commitMove)
  const removeNode = useBlueprint(s => s.removeNode)
  const connect = useBlueprint(s => s.connect)
  const addNode = useBlueprint(s => s.addNode)
  const { screenToFlowPosition } = useReactFlow()

  // The XR node isn't a store `Node` (it has no resource, no name, nothing
  // to key it into the blueprint document) — its position is UI-local.
  const [xrPosition, setXrPosition] = useState({ x: 20, y: 40 })
  const [rfNodes, setRfNodes] = useState<RFNode[]>([])

  // A freshly loaded document has resources but no nodes yet (positions
  // have no home in the Blueprint schema — see store/blueprint.ts's load()).
  // Hydrate once per load(), keyed on `loadEpoch` ALONE (bumped by load()
  // alone — see the store), with `doc` read via getState() inside rather
  // than subscribed: ordinary mutations (addNode, removeNode, connect,
  // setField, ...) give `doc` a brand-new object identity via immer's
  // structural sharing, so keying this effect on `doc` too made every edit
  // re-run it — and an edit landing while the kinds() fetch was still in
  // flight ran this effect's cleanup, cancelled the fetch, and then
  // re-entered with the epoch already marked as processed, permanently
  // abandoning hydration for that load. Keyed on the epoch alone, an edit
  // neither cancels nor re-triggers the in-flight hydration; the cleanup's
  // `cancelled` flag now means exactly "a new load started, or Canvas
  // unmounted." (`nodes.length` stays out of the deps for the same reason
  // it always has: it legitimately passes back through zero on an ordinary
  // delete, which previously resurrected the deleted node.)
  useEffect(() => {
    const state = useBlueprint.getState()
    if (!state.doc) return
    if (state.nodes.length > 0) return
    if (state.doc.spec.resources.length === 0) return
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
  }, [loadEpoch])

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
      // Only reported when this batch actually contains a selection change
      // (xyflow emits one 'select' change per node whose selected flag
      // flips — a click on node B ordinarily arrives as [deselect A,
      // select B] in one batch): a plain drag's batch is all 'position'
      // changes and must not be misread as "selection cleared."
      let selectionChanged = false
      let selectedId: string | null = null
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
        } else if (change.type === "select") {
          selectionChanged = true
          // The XR node has no resource, so it is never a reportable
          // selection — a click that selects only the XR node reports as
          // "nothing selected," same as clicking empty canvas.
          if (change.selected && change.id !== XR_ID) selectedId = change.id
        }
      }
      if (selectionChanged) onSelectionChange?.(selectedId)
    },
    [moveNode, commitMove, removeNode, onSelectionChange],
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

  // Drop-to-create: the palette (Palette.tsx's KindRow) sets
  // "application/x-compositionfactory-kind" as the serialized Kind on drag
  // start. onDragOver must call preventDefault() — the browser's default is
  // to refuse the drop entirely, firing no "drop" event at all — and
  // screenToFlowPosition converts the browser's client coordinates into the
  // same flow-space coordinates addNode's x/y already expect (accounting
  // for the canvas's own pan/zoom), exactly like dragging an existing node
  // already does via xyflow's own onNodesChange.
  const onDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.dataTransfer.dropEffect = "copy"
  }, [])

  const onDrop = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      event.preventDefault()
      const kind = parseDraggedKind(event.dataTransfer)
      if (!kind) return
      const position = screenToFlowPosition({ x: event.clientX, y: event.clientY })
      addNode(kind, position.x, position.y)
    },
    [addNode, screenToFlowPosition],
  )

  return (
    <div style={{ width: "100%", height: "100%" }} onDragOver={onDragOver} onDrop={onDrop}>
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
          {/* The dot grid takes its colour from the --grid token (the one
              place tokens.css defines a canvas-grid colour, per theme) —
              without this, Background falls back to xyflow's own vendor
              default, an off-token grey that ignores the dark theme. */}
          <Background color="var(--grid)" />
        </ReactFlow>
      </FieldsCacheContext.Provider>
    </div>
  )
}

export function Canvas({ onSelectionChange }: CanvasProps = {}): JSX.Element {
  return (
    <ReactFlowProvider>
      <CanvasInner onSelectionChange={onSelectionChange} />
    </ReactFlowProvider>
  )
}
