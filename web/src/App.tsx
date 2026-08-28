// Placeholder shell for Task 1. The canvas, palette and inspector panes are
// built in later M3 tasks on top of the tokens and API contract this task
// establishes; this component only proves the two are wired together.
export default function App() {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        height: 46,
        padding: "0 14px",
        background: "var(--surface)",
        borderBottom: "1px solid var(--rule)",
        color: "var(--ink)",
        fontFamily: "var(--sans)",
      }}
    >
      <span style={{ fontWeight: 600, letterSpacing: "-0.01em" }}>compositionfactory</span>
      <span className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>v0.1.0</span>
    </div>
  )
}
