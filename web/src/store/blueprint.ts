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
import dagre from "@dagrejs/dagre"
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

  /** Bumped by exactly one place: load(). Ordinary mutations (addNode,
   * removeNode, connect, disconnect, hydrateNodes) give `doc` a brand-new
   * object identity too — immer's structural sharing does that on every
   * change — so identity alone can't tell "a new document was loaded"
   * apart from "the existing document was edited." loadEpoch is the
   * unambiguous signal for the former: the canvas keys its one-time-per-
   * load hydration check on this, not on doc's identity or on nodes.length
   * (both of which legitimately change on an ordinary delete too — keying
   * on either previously caused a real bug where deleting a node raced
   * with a stray re-hydration and silently resurrected it). */
  loadEpoch: number

  /** Bounded undo stack of pre-mutation snapshots; see HISTORY_CAP. */
  history: Snapshot[]
  /** The state as it stood before the current drag gesture's first
   * moveNode() call, captured lazily and folded into `history` by
   * commitMove(). Null when no drag is in progress. */
  dragBaseline: Snapshot | null
  /** setField's analogue of dragBaseline: the state as it stood before the
   * current field-edit gesture's first setField() call, captured lazily and
   * folded into `history` by commitField(). Null when no edit is pending. */
  editBaseline: Snapshot | null
  /** Which (nodeId, path) `editBaseline` belongs to. commitField() only
   * folds the baseline when it's called for THIS field — a stray or
   * mis-ordered commit for a different field must not silently swallow (or
   * mis-attribute) another field's still-pending edit. Null exactly when
   * editBaseline is null. */
  editingField: { nodeId: string; path: string } | null

  load(): Promise<void>
  addNode(k: Kind, x: number, y: number): void
  /** Gives every resource in `doc` that does not already have a node one,
   * laid out with dagre. The canvas calls this once after a fresh load, so
   * a document read from disk shows up instead of an empty pane. See the
   * doc comment on the implementation below for the matching rule. */
  hydrateNodes(kinds: Kind[]): void
  moveNode(id: string, x: number, y: number): void
  removeNode(id: string): void
  connect(fromParam: string, toNode: string, toPath: string): void
  disconnect(wireId: string): void
  /** Writes a literal `{ value }` assignment onto one field of the resource
   * behind `nodeId`, replacing whatever was there before (a previous
   * literal or a raw escape hatch) — mirrors `connect`'s "the assignment IS
   * the field" rule, just for a typed-in value instead of a parameter
   * reference. Called on every keystroke from the Inspector's field editor,
   * so — like `moveNode` and unlike `connect`/`disconnect` — it
   * deliberately does NOT pushHistory() itself; see `editBaseline` and
   * `commitField()` below for the drag-protocol equivalent that makes a
   * whole typing gesture into exactly one undo step (fix round 1, Finding
   * 2: without this, Ctrl+Z after an edit popped the PREVIOUS history
   * entry instead — e.g. the addNode() that created the node being edited,
   * making the node itself vanish).
   *
   * Two more rules, both from fix round 1:
   * - Finding 1: an empty `value` DELETES the field key entirely, never
   *   leaves `{ value: "" }` behind — mirrors `disconnect`'s "never leave
   *   an empty mapping" rule. `{ value: "" }` is indistinguishable on the
   *   wire from "the user cleared this field back out," and leaving it set
   *   trips `blueprint.Validate()`'s "set exactly one of from, value or
   *   raw" check the moment it's satisfied by zero of the three instead —
   *   backspacing a field you just typed into should return it to "unset,"
   *   not hand you a confusing generate-time error.
   * - Finding 3: refuses (no mutation, no history touched) when the field
   *   currently holds a wire (`from` is set) unless the caller passes
   *   `{ overwriteWire: true }`. The Inspector's own rendering already
   *   never shows an editable box for a wired field, but the STORE is the
   *   single source of truth per this file's header — a UI-only guard
   *   would leave every other caller free to silently clobber a wire.
   *   Returns `false` on refusal (never throws: a rejected edit is
   *   expected, recoverable UI feedback, not exceptional control flow) and
   *   `true` once the field is actually written. */
  setField(
    nodeId: string,
    path: string,
    value: string,
    options?: { overwriteWire?: boolean },
  ): boolean
  undo(): void
  canUndo(): boolean
  /** Folds the pending drag baseline (if any) into a single history entry.
   * The canvas calls this once at drag end (pointer-up); moveNode() itself
   * never touches history. A no-op if no drag is in progress. */
  commitMove(): void
  /** setField's counterpart to commitMove(): folds `editBaseline` (if it
   * belongs to this exact `(nodeId, path)`) into one history entry and
   * clears both `editBaseline` and `editingField`. The Inspector calls this
   * on the field's blur — which, by ordinary DOM focus semantics, fires
   * before a different element (including a different field's textarea)
   * ever gains focus, so "call it on blur" already satisfies "commit
   * before a different field gains focus." A no-op if no edit is pending,
   * or if it's called for a field other than the one `editBaseline` was
   * captured for (a stray/late commit must not fold — or silently drop —
   * a DIFFERENT field's still-pending edit). */
  commitField(nodeId: string, path: string): void
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

// Rough footprint of a rendered ResourceNode, used only to keep dagre's
// initial layout from overlapping — not a real measured DOM size (this
// module has no DOM dependency; the canvas task owns actual rendering).
const LAYOUT_NODE_WIDTH = 220
const LAYOUT_NODE_HEIGHT = 120
// Leaves room at the left edge for the canvas's fixed XR node, which this
// store has no concept of (it is not a resource and carries no position
// here — the canvas task owns where it sits).
const LAYOUT_ORIGIN_X = 420

/** Assigns every name in `names` a non-overlapping (x, y) via dagre. There
 * are no resource-to-resource edges to feed it yet — every wire today runs
 * from an XRD parameter to a resource field, never between two resources
 * (see canvas/wires.ts) — so this is a single-rank layout: dagre still owns
 * the non-overlap math, the graph just has no edges to route around. */
function dagreLayout(names: string[]): Map<string, { x: number; y: number }> {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: "TB", nodesep: 40, ranksep: 80 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const name of names) {
    g.setNode(name, { width: LAYOUT_NODE_WIDTH, height: LAYOUT_NODE_HEIGHT })
  }
  dagre.layout(g)
  const positions = new Map<string, { x: number; y: number }>()
  for (const name of names) {
    const n = g.node(name)
    positions.set(name, {
      x: LAYOUT_ORIGIN_X + n.x - LAYOUT_NODE_WIDTH / 2,
      y: n.y - LAYOUT_NODE_HEIGHT / 2,
    })
  }
  return positions
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
          draft.editBaseline = null
          draft.editingField = null
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
      loadEpoch: 0,
      history: [],
      dragBaseline: null,
      editBaseline: null,
      editingField: null,

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
          draft.loadEpoch += 1
          draft.history = []
          draft.dragBaseline = null
          draft.editBaseline = null
          draft.editingField = null
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

      /** Matches each un-hydrated resource to its Kind by (kind, provider),
       * NEVER kind alone: the kinds index can hold two entries with the
       * same `kind` (a namespaced and a cluster-scoped variant of the same
       * managed resource, differing in provider/scope/apiVersion) — see
       * src/api/fixtures/kinds.json's two "Queue" entries. Matching on kind
       * alone would silently hand a hydrated node the wrong apiVersion. A
       * resource with no matching Kind is left un-hydrated rather than
       * guessed at.
       *
       * Not undoable (no pushHistory(), doc itself is untouched): hydrating
       * a loaded document into nodes is not a user edit, so it must not be
       * possible to Ctrl+Z a document back to "no nodes visible". Positions
       * come from dagreLayout above; a resource that already has a node is
       * left alone — dragged or not, hydration never overwrites it. */
      hydrateNodes(kinds) {
        const s = get()
        if (!s.doc) return
        const already = new Set(s.nodes.map(n => n.name))
        const toHydrate = s.doc.spec.resources.filter(r => !already.has(r.name))
        if (toHydrate.length === 0) return
        const positions = dagreLayout(toHydrate.map(r => r.name))
        set(draft => {
          if (!draft.doc) return
          for (const r of toHydrate) {
            const k = kinds.find(candidate => candidate.kind === r.kind && candidate.provider === r.provider)
            if (!k) continue
            const pos = positions.get(r.name) ?? { x: 0, y: 0 }
            draft.nodes.push({ id: makeId(), kind: r.kind, apiVersion: k.apiVersion, name: r.name, x: pos.x, y: pos.y })
          }
          draft.wires = computeWires(draft.doc, draft.nodes)
        })
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

      setField(nodeId, path, value, options) {
        // reconcile() first, same reason moveNode() calls it first: a test
        // (or any caller) that replaced `doc` out from under us since our
        // last own mutation must not let a stale editBaseline/history
        // survive the swap.
        reconcile()
        const s = get()
        if (!s.doc) return false
        const node = s.nodes.find(n => n.id === nodeId)
        if (!node) return false
        const res = s.doc.spec.resources.find(r => r.name === node.name)
        if (!res) return false
        // Finding 3: never silently clobber a wire. The Inspector's own
        // rendering already keeps a wired field out of reach of this call,
        // but the store is the single source of truth, so it enforces the
        // rule itself rather than trusting every future caller to.
        if (res.fields[path]?.from && !options?.overwriteWire) return false
        // Finding 2 / the undo trap: capture the pre-edit snapshot on the
        // FIRST setField of a gesture only (exactly moveNode's dragBaseline
        // pattern) — never pushed straight to `history` here, so a run of
        // keystrokes still costs zero history entries until commitField()
        // folds them into one.
        const startingNewEdit = s.editBaseline === null
        const baseline = startingNewEdit ? snapshotOf(s) : null
        set(draft => {
          if (startingNewEdit) {
            draft.editBaseline = baseline
            draft.editingField = { nodeId, path }
          }
          if (!draft.doc) return
          const dnode = draft.nodes.find(n => n.id === nodeId)
          if (!dnode) return
          const dres = draft.doc.spec.resources.find(r => r.name === dnode.name)
          if (!dres) return
          if (value === "") {
            // Finding 1: an empty value is "unset," not `{ value: "" }` —
            // mirrors disconnect()'s "delete the key outright" rule.
            delete dres.fields[path]
          } else {
            // A fresh object, not a merge: a literal value replaces
            // whatever assignment previously lived at this path, exactly
            // like connect() does for a wire.
            dres.fields[path] = { value }
          }
        })
        // setField DOES touch `doc`, unlike moveNode, so trackedDoc must
        // stay current or the next history-touching action's reconcile()
        // would wrongly see an "external" doc replacement and wipe history.
        noteOwnMutation()
        return true
      },

      commitField(nodeId, path) {
        reconcile()
        set(draft => {
          if (draft.editBaseline === null) return
          const editing = draft.editingField
          // Only fold when this commit is for the field the pending
          // baseline actually belongs to — a stray or late commit for a
          // DIFFERENT field must not fold (or drop) that field's own still-
          // pending edit. In the normal case (Inspector calls this from
          // the field's own onBlur) this always matches.
          if (!editing || editing.nodeId !== nodeId || editing.path !== path) return
          draft.history.push(draft.editBaseline)
          if (draft.history.length > HISTORY_CAP) draft.history.shift()
          draft.editBaseline = null
          draft.editingField = null
        })
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
          // Same reasoning as dragBaseline: an in-progress field edit's
          // baseline was captured against the pre-undo doc. Once undo()
          // has replaced doc/nodes/wires wholesale, that baseline (and
          // whatever it was mid-typing into) no longer corresponds to
          // anything live — carrying it forward would let a later
          // commitField() fold a snapshot of a document state that no
          // longer exists on this timeline.
          draft.editBaseline = null
          draft.editingField = null
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
