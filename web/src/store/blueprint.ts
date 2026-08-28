// The blueprint store: the single state document behind the canvas.
//
// Two rules this file locks in:
//
// 1. A wire is not decoration — it *is* `{ from: "params.<name>" }` on a
//    resource's field. `connect()` writes that key into `doc`; `disconnect()`
//    deletes the key entirely (never leaves `{}`). `wires` is always
//    recomputed from `doc` + `nodes` (see computeWires below), so there is no
//    separate wire model that can drift out of sync with the document.
//
// 2. Node positions are mirrored into the undo history on drag *end*, never
//    on drag *move*. `moveNode()` updates a node's x/y with no history push
//    at all; the canvas (a later task) calls `commitMove()` once on
//    pointer-up to fold the whole drag gesture into a single undo entry.
//    Otherwise every pointer-move event becomes its own undo step and
//    Ctrl+Z rewinds one pixel at a time.
import { create } from "zustand"
import { immer } from "zustand/middleware/immer"
import { api } from "../api/contract"
import type { Blueprint, Kind } from "../api/contract"

export interface Node {
  id: string
  kind: string
  apiVersion: string
  /** The resource's name in `doc` — the identity the canvas uses to find
   * "this node's" resource. Distinct from `id`: `id` is this node's stable
   * client-side handle (never changes), `name` is the document-visible,
   * user-facing resource name (unique, and the source of the
   * `setResourceNameAnnotation` the Go side emits). */
  name: string
  x: number
  y: number
}

export interface Wire {
  id: string
  fromParam: string
  toNode: string
  toPath: string
}

interface Snapshot {
  doc: Blueprint | null
  nodes: Node[]
  wires: Wire[]
}

interface BlueprintStore {
  doc: Blueprint | null
  nodes: Node[]
  wires: Wire[]

  /** Bounded undo stack of pre-mutation snapshots; see HISTORY_CAP. */
  history: Snapshot[]
  /** The state as it stood before the current drag gesture's first
   * moveNode() call, captured lazily and folded into `history` by
   * commitMove(). Null when no drag is in progress. */
  dragBaseline: Snapshot | null

  load(): Promise<void>
  addNode(k: Kind, x: number, y: number): void
  moveNode(id: string, x: number, y: number): void
  removeNode(id: string): void
  connect(fromParam: string, toNode: string, toPath: string): void
  disconnect(wireId: string): void
  undo(): void
  canUndo(): boolean
  /** Folds the pending drag baseline (if any) into a single history entry.
   * The canvas calls this once at drag end (pointer-up); moveNode() itself
   * never touches history. A no-op if no drag is in progress. */
  commitMove(): void
}

const HISTORY_CAP = 50

function slugify(kind: string): string {
  const s = kind
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
  return s || "resource"
}

/** Derives a unique, stable resource name from a Kind's name: lowercased,
 * with a numeric suffix on collision. Resource names become the
 * `setResourceNameAnnotation` the Go side emits, and a duplicate name
 * silently collapses two resources into one in Crossplane — so uniqueness
 * here is not cosmetic. The result always satisfies the engine's
 * `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`. */
function uniqueResourceName(existingNames: string[], kind: string): string {
  const base = slugify(kind)
  if (!existingNames.includes(base)) return base
  let n = 2
  while (existingNames.includes(`${base}-${n}`)) n++
  return `${base}-${n}`
}

function makeId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID()
  }
  return `id-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

/** Rebuilds `wires` from scratch by scanning every resource's fields for a
 * `{ from: "params.<name>" }` assignment and pairing it with the node whose
 * `name` matches the resource. This is the whole point of rule 1: wires are
 * never stored or patched independently, only ever recomputed from `doc`. */
function computeWires(doc: Blueprint | null, nodes: Node[]): Wire[] {
  if (!doc) return []
  const byName = new Map(nodes.map(n => [n.name, n] as const))
  const wires: Wire[] = []
  for (const res of doc.spec.resources) {
    const node = byName.get(res.name)
    if (!node) continue
    for (const [path, assignment] of Object.entries(res.fields)) {
      if (assignment?.from?.startsWith("params.")) {
        wires.push({
          id: `${node.id}:${path}`,
          fromParam: assignment.from.slice("params.".length),
          toNode: node.id,
          toPath: path,
        })
      }
    }
  }
  return wires
}

function snapshotOf(s: { doc: Blueprint | null; nodes: Node[]; wires: Wire[] }): Snapshot {
  return structuredClone({ doc: s.doc, nodes: s.nodes, wires: s.wires })
}

export const useBlueprint = create<BlueprintStore>()(
  immer((set, get) => {
    // `history`/`dragBaseline` are bookkeeping for OUR OWN actions, not
    // application data — nothing outside this module should read or reset
    // them directly. In the real app this is moot: the only place `doc` is
    // ever replaced wholesale is `load()`, which already clears `history`
    // and `dragBaseline` itself. It matters for *tests*: zustand's
    // `setState(partial)` is a shallow merge, so a test resetting fixture
    // state between cases via `useBlueprint.setState({ doc, nodes, wires })`
    // (bypassing `load()` and our actions entirely) leaves `history` and
    // `dragBaseline` completely untouched — and since this store is a single
    // module-level instance shared across every test in a file, those stale
    // entries would otherwise accumulate across unrelated test cases.
    //
    // `trackedDoc` remembers the `doc` reference as of the end of our own
    // last internal mutation. Anywhere we're about to read or push onto
    // `history`, `reconcile()` compares `get().doc` against it: a mismatch
    // means something replaced `doc` without going through us — in practice,
    // only ever a test's direct `setState` — so the old history is
    // meaningless and gets dropped. Multi-step undo within one continuous
    // session is unaffected; this only fires across the boundary a test
    // reset creates.
    let trackedDoc: Blueprint | null = null

    function reconcile() {
      if (get().doc !== trackedDoc) {
        set(draft => {
          draft.history = []
          draft.dragBaseline = null
        })
        trackedDoc = get().doc
      }
    }

    function noteOwnMutation() {
      trackedDoc = get().doc
    }

    function pushHistory() {
      reconcile()
      const snap = snapshotOf(get())
      set(draft => {
        draft.history.push(snap)
        if (draft.history.length > HISTORY_CAP) draft.history.shift()
      })
    }

    return {
      doc: null,
      nodes: [],
      wires: [],
      history: [],
      dragBaseline: null,

      async load() {
        const doc = await api.blueprint()
        // Position (x/y) has no home in the Blueprint schema (Resource
        // carries no coordinates), so nodes for any resources already in a
        // loaded document cannot be reconstructed here — that reconciliation
        // (auto-layout, or a canvas-side positions cache) belongs to the
        // canvas task that actually renders them.
        set(draft => {
          draft.doc = doc
          draft.nodes = []
          draft.wires = []
          draft.history = []
          draft.dragBaseline = null
        })
        noteOwnMutation()
      },

      addNode(k, x, y) {
        const s = get()
        if (!s.doc) return
        pushHistory()
        set(draft => {
          if (!draft.doc) return
          const name = uniqueResourceName(
            draft.doc.spec.resources.map(r => r.name),
            k.kind,
          )
          // Appended, not prepended: the Go emitter (internal/emit/composition.go)
          // ranges over spec.resources in array order and writes one `---`
          // block per resource, so array position is byte-load-bearing in
          // the generated Composition. Prepending would reorder every
          // previously-added resource's block on each new add, producing
          // needless diff churn in ArgoCD-tracked output.
          draft.doc.spec.resources.push({ name, kind: k.kind, provider: k.provider, fields: {} })
          draft.nodes.push({ id: makeId(), kind: k.kind, apiVersion: k.apiVersion, name, x, y })
          draft.wires = computeWires(draft.doc, draft.nodes)
        })
        noteOwnMutation()
      },

      moveNode(id, x, y) {
        // Reconcile first: if `doc` was replaced externally since our last
        // mutation, any pending `dragBaseline` refers to a document that no
        // longer exists and must not be resurrected by this drag.
        reconcile()
        const s = get()
        // Capture the pre-drag snapshot on the *first* move of a gesture
        // only; never pushed to `history` here (see the file header).
        const baseline = s.dragBaseline === null ? snapshotOf(s) : null
        set(draft => {
          if (baseline) draft.dragBaseline = baseline
          const node = draft.nodes.find(n => n.id === id)
          if (node) {
            node.x = x
            node.y = y
          }
        })
        // moveNode never touches `doc`, so `trackedDoc` stays valid — no
        // noteOwnMutation() needed here.
      },

      removeNode(id) {
        const s = get()
        if (!s.doc) return
        const node = s.nodes.find(n => n.id === id)
        if (!node) return
        pushHistory()
        set(draft => {
          if (!draft.doc) return
          // Removing the resource removes every field on it, so every wire
          // touching this node disappears with it once wires are recomputed
          // — there is no separate wire list to clean up by hand.
          draft.doc.spec.resources = draft.doc.spec.resources.filter(r => r.name !== node.name)
          draft.nodes = draft.nodes.filter(n => n.id !== id)
          draft.wires = computeWires(draft.doc, draft.nodes)
        })
        noteOwnMutation()
      },

      connect(fromParam, toNode, toPath) {
        const s = get()
        if (!s.doc) return
        const node = s.nodes.find(n => n.id === toNode)
        if (!node) return
        const res = s.doc.spec.resources.find(r => r.name === node.name)
        if (!res) return
        pushHistory()
        set(draft => {
          if (!draft.doc) return
          const dnode = draft.nodes.find(n => n.id === toNode)
          if (!dnode) return
          const dres = draft.doc.spec.resources.find(r => r.name === dnode.name)
          if (!dres) return
          // The wire *is* this assignment — nothing else to record.
          dres.fields[toPath] = { from: `params.${fromParam}` }
          draft.wires = computeWires(draft.doc, draft.nodes)
        })
        noteOwnMutation()
      },

      disconnect(wireId) {
        const s = get()
        const wire = s.wires.find(w => w.id === wireId)
        if (!wire || !s.doc) return
        pushHistory()
        set(draft => {
          if (!draft.doc) return
          const node = draft.nodes.find(n => n.id === wire.toNode)
          if (!node) return
          const res = draft.doc.spec.resources.find(r => r.name === node.name)
          if (!res) return
          // Delete the key outright — never leave `{}` behind.
          delete res.fields[wire.toPath]
          draft.wires = computeWires(draft.doc, draft.nodes)
        })
        noteOwnMutation()
      },

      undo() {
        reconcile()
        set(draft => {
          const prev = draft.history.pop()
          if (!prev) return
          draft.doc = prev.doc
          draft.nodes = prev.nodes
          draft.wires = prev.wires
          draft.dragBaseline = null
        })
        noteOwnMutation()
      },

      canUndo() {
        reconcile()
        return get().history.length > 0
      },

      commitMove() {
        reconcile()
        set(draft => {
          if (draft.dragBaseline === null) return
          draft.history.push(draft.dragBaseline)
          if (draft.history.length > HISTORY_CAP) draft.history.shift()
          draft.dragBaseline = null
        })
      },
    }
  }),
)
