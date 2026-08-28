// The application shell: palette left, canvas centre, inspector right,
// output below — the layout language of the reviewed prototype
// (docs/design/canvas-prototype.html's .app / .cols / .pane / .drawer
// rules: a 46px header bar over a three-column body over a fixed-height
// bottom drawer), reproduced with this project's own tokens (tokens.css),
// not the prototype's M4-scope machinery (tabs, shared vars, warnbar) that
// lives inside that same drawer there.
//
// This is the one place that bootstraps the document: every panel below
// reads `doc` from the store (useBlueprint), never fetches its own copy, so
// nothing shows real data until this load() resolves.
import { useEffect, useState } from "react"
import { useBlueprint } from "./store/blueprint"
import { Palette } from "./panels/Palette"
import { Canvas } from "./canvas/Canvas"
import { Inspector } from "./panels/Inspector"
import { Output } from "./panels/Output"

export default function App() {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)

  useEffect(() => {
    // Best-effort, same as every other panel's own fetch (Palette, Canvas's
    // hydration effect): a failed initial load leaves the shell showing an
    // empty canvas rather than crashing on an unhandled rejection.
    useBlueprint
      .getState()
      .load()
      .catch(() => {})
  }, [])

  // Cmd/Ctrl+Z → store undo. Window-level so it works wherever focus sits
  // on the canvas — EXCEPT inside a text control (input/textarea/
  // contenteditable), where the browser's own text-level undo owns the
  // shortcut and hijacking it would make typing mistakes unrecoverable.
  // No redo binding: the store has no redo to wire (undo() is one-way).
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey)) return
      if (event.shiftKey || event.altKey) return
      if (event.key !== "z" && event.key !== "Z") return
      const target = event.target
      if (
        target instanceof HTMLElement &&
        (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)
      ) {
        return
      }
      event.preventDefault()
      useBlueprint.getState().undo()
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])

  // A selected node can disappear out from under the Inspector (Delete on
  // the canvas, or an undo that removes it) without the canvas ever sending
  // a matching deselection — guard against showing an inspector for a node
  // that no longer exists rather than trusting Canvas to always announce it.
  const selectedNodeExists = useBlueprint(
    s => selectedNodeId !== null && s.nodes.some(n => n.id === selectedNodeId),
  )
  useEffect(() => {
    if (selectedNodeId !== null && !selectedNodeExists) setSelectedNodeId(null)
  }, [selectedNodeId, selectedNodeExists])

  return (
    <div
      style={{
        display: "grid",
        gridTemplateRows: "auto 1fr auto",
        height: "100vh",
        background: "var(--ground)",
        color: "var(--ink)",
        fontFamily: "var(--sans)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          height: 46,
          padding: "0 14px",
          background: "var(--surface)",
          borderBottom: "1px solid var(--rule)",
        }}
      >
        <span style={{ fontWeight: 600, letterSpacing: "-0.01em" }}>compositionfactory</span>
        <span className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>
          v0.1.0
        </span>
      </div>

      <div
        style={{
          display: "grid",
          gridTemplateColumns: "216px 1fr 330px",
          minHeight: 0,
          overflow: "hidden",
        }}
      >
        <div style={{ minHeight: 0, overflow: "hidden", borderRight: "1px solid var(--rule)" }}>
          <Palette />
        </div>

        <div style={{ minHeight: 0, overflow: "hidden", background: "var(--ground)" }}>
          <Canvas onSelectionChange={setSelectedNodeId} />
        </div>

        <div
          style={{
            minHeight: 0,
            overflow: "hidden",
            borderLeft: "1px solid var(--rule)",
            background: "var(--surface-2)",
          }}
        >
          {selectedNodeId ? (
            <Inspector nodeId={selectedNodeId} />
          ) : (
            <div
              data-testid="inspector-empty"
              className="mono"
              style={{ padding: 10, fontSize: 12, color: "var(--faint)" }}
            >
              select a node to inspect its fields
            </div>
          )}
        </div>
      </div>

      <div style={{ minHeight: 0, height: 220, overflow: "hidden", borderTop: "1px solid var(--rule)" }}>
        <Output />
      </div>
    </div>
  )
}
