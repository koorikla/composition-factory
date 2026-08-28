// The output pane: renders exactly what POST /api/generate reports, and
// nothing else — it never generates locally.
//
// The canvas document lives client-side (in the store), but /api/generate
// re-reads the document from DISK. Without pushing the store's current doc
// first, the preview would silently ignore everything the user just did on
// the canvas — so a refresh is always two calls, in order: PUT /api/blueprint
// (the full-document replace this task adds — see api/contract.ts's
// putBlueprint doc comment), then POST /api/generate {"write":false}. Either
// leg can fail (PUT on a document the engine's own validation rejects,
// generate on the same underlying document); either failure surfaces as an
// alert and the previous successful output is hidden, never left on screen
// looking current — showing yesterday's YAML next to today's error is
// exactly the silent-wrongness class `cf gen --check` exists to catch, and
// this pane must not reproduce it. Regeneration is triggered by the store's
// `doc` changing (see the effect below) and debounced: the Inspector calls
// `setField` on every keystroke, and firing a PUT+generate round trip per
// keystroke would be both wasteful and visibly janky.
//
// GenerateOutput now carries `body` — the engine's actual generated YAML,
// byte-for-byte (see api/contract.ts's doc comment; the server extension
// landed on main as 5817041). So the view renders each output's real
// content verbatim: a heading naming its `path`, then its `body` in a
// plain, read-only <pre>. A <pre> is read-only by construction (no
// contenteditable, nothing to wire up to reject edits) — same guarantee
// the previous CodeMirror mount enforced explicitly, for free. Deliberately
// no syntax-highlighting library: this is a straight, unmodified rendering
// of exactly the bytes the server sent, not a reinterpretation of them.
import { useEffect, useRef, useState } from "react"
import { api, ApiError } from "../api/contract"
import type { GenerateResult } from "../api/contract"
import { useBlueprint } from "../store/blueprint"

// Long enough that a burst of store edits collapses into one round trip
// instead of one per keystroke (mirrors Palette.tsx's SEARCH_DEBOUNCE_MS);
// short enough that the preview still feels live.
const REGENERATE_DEBOUNCE_MS = 200

interface YamlViewProps {
  result: GenerateResult
}

/** Each output's `path` heading, then its `body` rendered verbatim in a
 * <pre> — no trimming, no reformatting, no syntax highlighting. `body` is
 * placed as the <pre>'s sole child so its textContent is exactly `body`,
 * byte for byte. */
function YamlView({ result }: YamlViewProps) {
  return (
    // role="region" + aria-label, NOT aria-readonly: aria-readonly is only
    // meaningful on roles that support it (textbox, grid, ...) — on a plain
    // div it is inert ARIA that no assistive tech announces. A labelled
    // region is what this actually is: a named landmark of read-only
    // content a screen-reader user can jump to.
    <div
      data-testid="yaml-view"
      role="region"
      aria-label="Generated YAML (read-only)"
      style={{ display: "flex", flexDirection: "column", gap: 12, overflow: "auto" }}
    >
      {result.outputs.map(o => (
        <div key={o.path}>
          <div className="mono" style={{ fontSize: 11, color: "var(--faint)", marginBottom: 4 }}>
            {o.path}
          </div>
          <pre
            data-testid="yaml-body"
            className="mono"
            style={{
              margin: 0,
              fontSize: 12,
              padding: 8,
              border: "1px solid var(--rule)",
              borderRadius: 4,
              background: "var(--surface)",
              color: "var(--ink)",
              overflow: "auto",
              maxHeight: 220,
            }}
          >
            {o.body}
          </pre>
        </div>
      ))}
    </div>
  )
}

function messageOf(e: unknown, fallback: string): string {
  if (e instanceof ApiError) return e.message
  return fallback
}

export function Output() {
  const doc = useBlueprint(s => s.doc)
  const [result, setResult] = useState<GenerateResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Refresh-cycle versioning (fix wave F9): every refresh claims the next
  // sequence number the moment it actually starts (after the debounce), and
  // every response — success or failure, PUT or generate — checks that it
  // is still the NEWEST refresh before touching state. A stale generate
  // response landing after a newer one has painted must never overwrite it:
  // that is yesterday's YAML presented as current, the exact silent-
  // wrongness class this pane exists to avoid. A ref, not effect-local
  // state, so the guard holds across effect instances — including any
  // future refresh trigger that never passes through this effect's
  // teardown at all.
  const refreshSeq = useRef(0)

  // Debounced on `doc` — the store's current document is the trigger for
  // every regenerate, exactly as an addNode/connect/setField edit makes it.
  useEffect(() => {
    const timer = setTimeout(() => {
      const seq = ++refreshSeq.current
      const stale = () => refreshSeq.current !== seq
      async function refresh() {
        if (doc) {
          try {
            await api.putBlueprint(doc)
          } catch (e) {
            if (stale()) return
            // A failed PUT never reaches /api/generate at all — surfaced
            // exactly like a generate failure: an alert, and the previous
            // successful output (if any) is hidden, never left showing.
            setError(messageOf(e, "failed to save the document"))
            setResult(null)
            return
          }
        }
        try {
          const generated = await api.generate(false)
          if (stale()) return
          setResult(generated)
          setError(null)
        } catch (e) {
          if (stale()) return
          setError(messageOf(e, "generation failed"))
          setResult(null)
        }
      }
      refresh()
    }, REGENERATE_DEBOUNCE_MS)
    return () => {
      // Invalidate any refresh this effect instance started: a doc change
      // must not let the old cycle's response land during the new cycle's
      // debounce gap, and an unmounted pane must not set state at all.
      refreshSeq.current++
      clearTimeout(timer)
    }
  }, [doc])

  return (
    <div
      data-testid="output"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        padding: 10,
        height: "100%",
        overflow: "auto",
        background: "var(--surface-2)",
        color: "var(--ink)",
        fontFamily: "var(--sans)",
      }}
    >
      <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span style={{ fontWeight: 600 }}>Output</span>
        <span className="mono" style={{ fontSize: 10, color: "var(--faint)" }}>
          write: false
        </span>
      </div>
      {error ? (
        <div role="alert" className="mono" style={{ fontSize: 12, color: "var(--err)" }}>
          {error}
        </div>
      ) : (
        result && <YamlView result={result} />
      )}
    </div>
  )
}
