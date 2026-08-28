// The palette: search over the kinds index (server-side — never client-side
// filtering, since the index can hold far more than one page) and drag-to-
// create onto the canvas. Every row is a drag source AND keyboard-
// activatable (Enter adds at a default position): upjet ships every managed
// resource twice (a namespaced v2 and a cluster-scoped v1 variant, see
// src/api/fixtures/kinds.json and store/blueprint.ts's hydrateNodes note),
// so the scope distinction is rendered as its own element, never folded
// into the kind name.
import { useEffect, useRef, useState } from "react"
import { api } from "../api/contract"
import type { Kind } from "../api/contract"
import { useBlueprint } from "../store/blueprint"

// Debounce delay for search-as-you-type: long enough that a fast typist
// doesn't fire a request per keystroke, short enough that the result list
// still feels live.
const SEARCH_DEBOUNCE_MS = 250

// Where a keyboard-added node lands: the canvas lays out real positions via
// dagre on load/hydrate (see store/blueprint.ts), so this only needs to be
// a sane, visible starting point a user can drag from afterward.
const DEFAULT_ADD_X = 480
const DEFAULT_ADD_Y = 40

function dragKindData(k: Kind): string {
  return JSON.stringify(k)
}

interface KindRowProps {
  kind: Kind
}

function KindRow({ kind }: KindRowProps) {
  const addNode = useBlueprint(s => s.addNode)
  const testId = `kind-${kind.apiVersion}-${kind.kind}`

  return (
    <div
      data-testid={testId}
      tabIndex={0}
      role="button"
      aria-label={`Add ${kind.kind} (${kind.apiVersion}, ${kind.scope})`}
      draggable
      onDragStart={event => {
        event.dataTransfer.setData("application/x-compositionfactory-kind", dragKindData(kind))
        event.dataTransfer.setData("text/plain", kind.kind)
        event.dataTransfer.effectAllowed = "copy"
      }}
      onKeyDown={event => {
        if (event.key !== "Enter" && event.key !== " ") return
        event.preventDefault()
        addNode(kind, DEFAULT_ADD_X, DEFAULT_ADD_Y)
      }}
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: "6px 10px",
        border: "1px solid var(--rule)",
        borderRadius: 4,
        background: "var(--surface)",
        cursor: "grab",
      }}
    >
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
          <span style={{ fontWeight: 600 }}>{kind.kind}</span>
          <span
            data-testid={`scope-${kind.scope}`}
            className="mono"
            style={{
              fontSize: 10,
              padding: "1px 5px",
              borderRadius: 3,
              color: kind.namespaced ? "var(--wire-xrd)" : "var(--wire-ref)",
              background: kind.namespaced ? "var(--wire-xrd-soft)" : "var(--wire-ref-soft)",
            }}
          >
            {kind.scope}
          </span>
        </div>
      </div>
      <span
        className="mono"
        title={`${kind.required} required of ${kind.fields} fields`}
        style={{ fontSize: 10, color: "var(--faint)", whiteSpace: "nowrap" }}
      >
        {kind.required} req
      </span>
    </div>
  )
}

export function Palette() {
  const [query, setQuery] = useState("")
  const [kinds, setKinds] = useState<Kind[]>([])
  const [loaded, setLoaded] = useState(false)
  const debounceTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current)
    let cancelled = false
    debounceTimer.current = setTimeout(() => {
      api
        .kinds(query)
        .then(result => {
          if (!cancelled) {
            setKinds(result)
            setLoaded(true)
          }
        })
        .catch(() => {
          if (!cancelled) {
            setKinds([])
            setLoaded(true)
          }
        })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      cancelled = true
      if (debounceTimer.current) clearTimeout(debounceTimer.current)
    }
  }, [query])

  // Grouped by apiVersion: upjet's namespaced and cluster-scoped variants of
  // the same kind share a name but never a group, so each apiVersion section
  // heads its own kind list even when two sections both contain "Queue".
  const groups = new Map<string, Kind[]>()
  for (const k of kinds) {
    const list = groups.get(k.apiVersion)
    if (list) list.push(k)
    else groups.set(k.apiVersion, [k])
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        padding: 10,
        background: "var(--surface-2)",
        height: "100%",
        overflow: "auto",
      }}
    >
      <input
        type="search"
        role="searchbox"
        aria-label="Search kinds"
        placeholder="Search kinds…"
        value={query}
        onChange={event => setQuery(event.target.value)}
        style={{
          padding: "6px 8px",
          border: "1px solid var(--rule)",
          borderRadius: 4,
          background: "var(--surface)",
          color: "var(--ink)",
          fontFamily: "var(--sans)",
        }}
      />
      {loaded && kinds.length === 0 && (
        <div className="mono" style={{ color: "var(--faint)", fontSize: 12, padding: "4px 2px" }}>
          no kinds match “{query}”
        </div>
      )}
      {[...groups.entries()].map(([apiVersion, group]) => (
        <div key={apiVersion} style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <div
            className="mono"
            style={{ fontSize: 10, color: "var(--faint)", letterSpacing: "0.02em" }}
          >
            {apiVersion}
          </div>
          {group.map(k => (
            <KindRow key={`${k.apiVersion}/${k.kind}`} kind={k} />
          ))}
        </div>
      ))}
    </div>
  )
}
